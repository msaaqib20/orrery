// Command orreryd runs the orrery daemon.
//
// It binds to loopback by default and answers on a small JSON API. With no
// configuration file and no network access it still works end to end, using
// the built-in skills and the offline echo provider.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/msaaqib20/orrery/internal/config"
	"github.com/msaaqib20/orrery/internal/httpapi"
	"github.com/msaaqib20/orrery/internal/journal"
	"github.com/msaaqib20/orrery/internal/logging"
	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/provider"
	"github.com/msaaqib20/orrery/internal/router"
	"github.com/msaaqib20/orrery/internal/runtime"
	"github.com/msaaqib20/orrery/internal/session"
	"github.com/msaaqib20/orrery/internal/skill"
	"github.com/msaaqib20/orrery/internal/version"
)

const pruneInterval = 5 * time.Minute

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "orreryd: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		configPath  = flag.String("config", "", "path to a JSON config file")
		addr        = flag.String("addr", "", "override the listen address")
		showVersion = flag.Bool("version", false, "print version information and exit")
	)
	flag.Parse()

	if *showVersion {
		fmt.Println(version.Get().String())
		return nil
	}

	cfg, err := config.Resolve(*configPath, os.LookupEnv)
	if err != nil {
		return fmt.Errorf("configuration: %w", err)
	}
	if *addr != "" {
		cfg.Addr = *addr
		if err := cfg.Validate(); err != nil {
			return fmt.Errorf("configuration: %w", err)
		}
	}

	log, err := logging.New(os.Stderr, cfg.Log.Level, cfg.Log.Format)
	if err != nil {
		return fmt.Errorf("logging: %w", err)
	}

	rt, closeRuntime, err := buildRuntime(cfg, log)
	if err != nil {
		return err
	}
	defer func() {
		if err := closeRuntime(); err != nil {
			log.Error("shutdown cleanup failed", slog.String("error", err.Error()))
		}
	}()

	srv := &http.Server{
		Addr:              cfg.Addr,
		Handler:           httpapi.New(rt, httpapi.Options{Logger: log, MaxBodyBytes: cfg.Limits.MaxBodyBytes}).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       cfg.Limits.RequestTimeout.D(),
		WriteTimeout:      cfg.Limits.RequestTimeout.D() + 5*time.Second,
		IdleTimeout:       2 * time.Minute,
		ErrorLog:          slog.NewLogLogger(log.Handler(), slog.LevelError),
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	go prune(ctx, rt, log)

	errCh := make(chan error, 1)
	go func() {
		log.Info("listening",
			slog.String("addr", cfg.Addr),
			slog.String("provider", cfg.Provider.Name),
			slog.Int("skills", len(rt.Skills())),
			slog.String("version", version.Version),
		)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return fmt.Errorf("listen: %w", err)
	case <-ctx.Done():
		log.Info("shutting down")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.Limits.ShutdownGrace.D())
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	log.Info("stopped")
	return nil
}

// buildRuntime assembles every component. It returns a close function rather
// than relying on the caller to remember the order things must be torn down in.
func buildRuntime(cfg config.Config, log *slog.Logger) (*runtime.Runtime, func() error, error) {
	skills := skill.NewRegistry()
	if err := skill.RegisterBuiltins(skills); err != nil {
		return nil, nil, fmt.Errorf("register skills: %w", err)
	}

	policy, err := permission.FromGrants(cfg.Permissions.DefaultAllow, cfg.Permissions.Grants)
	if err != nil {
		return nil, nil, fmt.Errorf("permissions: %w", err)
	}
	warnUngranted(skills, policy, log)

	providers := provider.NewRegistry()
	if err := providers.Register(provider.Echo{}); err != nil {
		return nil, nil, fmt.Errorf("register provider: %w", err)
	}

	jnl, err := journal.OpenFile(cfg.JournalPath())
	if err != nil {
		return nil, nil, fmt.Errorf("journal: %w", err)
	}

	rt, err := runtime.New(runtime.Options{
		Logger:          log,
		Sessions:        session.NewStore(cfg.Session.MaxTurns, cfg.Session.TTL.D()),
		Skills:          skills,
		Router:          router.New(cfg.Router.MinScore),
		Providers:       providers,
		Policy:          policy,
		Journal:         jnl,
		ProviderName:    cfg.Provider.Name,
		ProviderTimeout: cfg.Provider.Timeout.D(),
		MaxTokens:       cfg.Provider.MaxTokens,
	})
	if err != nil {
		jnl.Close()
		return nil, nil, fmt.Errorf("runtime: %w", err)
	}

	return rt, rt.Close, nil
}

// warnUngranted surfaces skills that are registered but cannot run under the
// current policy. Finding this out at start-up beats finding it out from a
// user's 403.
func warnUngranted(skills *skill.Registry, policy *permission.Policy, log *slog.Logger) {
	for _, d := range skills.Descriptors() {
		if err := policy.CheckAll(d.Name, d.Capabilities); err != nil {
			log.Warn("skill is registered but not permitted",
				slog.String("skill", d.Name),
				slog.String("reason", err.Error()))
		}
	}
}

// prune evicts idle sessions on a timer until ctx is cancelled.
func prune(ctx context.Context, rt *runtime.Runtime, log *slog.Logger) {
	ticker := time.NewTicker(pruneInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rt.Prune()
		}
	}
}
