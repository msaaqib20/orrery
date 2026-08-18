// Package config defines orrery's configuration surface and the rules for
// loading it from disk and the environment.
//
// Precedence, lowest to highest: built-in defaults, config file, environment
// variables. Validate is always the last step, so an invalid override is
// rejected no matter which layer produced it.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// Env var names honoured by ApplyEnv.
const (
	EnvAddr     = "ORRERY_ADDR"
	EnvDataDir  = "ORRERY_DATA_DIR"
	EnvLogLevel = "ORRERY_LOG_LEVEL"
	EnvLogFmt   = "ORRERY_LOG_FORMAT"
	EnvProvider = "ORRERY_PROVIDER"
	EnvMinScore = "ORRERY_ROUTER_MIN_SCORE"
)

// Config is the fully resolved runtime configuration.
type Config struct {
	Addr        string      `json:"addr"`
	DataDir     string      `json:"data_dir"`
	Log         Log         `json:"log"`
	Provider    Provider    `json:"provider"`
	Session     Session     `json:"session"`
	Router      Router      `json:"router"`
	Permissions Permissions `json:"permissions"`
	Limits      Limits      `json:"limits"`
}

// Log controls logger construction.
type Log struct {
	Level  string `json:"level"`  // debug | info | warn | error
	Format string `json:"format"` // text | json
}

// Provider selects and bounds the fallback completion backend.
type Provider struct {
	Name      string   `json:"name"`
	Timeout   Duration `json:"timeout"`
	MaxTokens int      `json:"max_tokens"`
}

// Session bounds conversation retention.
type Session struct {
	MaxTurns int      `json:"max_turns"`
	TTL      Duration `json:"ttl"`
}

// Router tunes intent matching.
type Router struct {
	MinScore float64 `json:"min_score"`
}

// Permissions describes the capability policy. Grants maps a skill name (or
// "*") to the capabilities it may exercise.
type Permissions struct {
	DefaultAllow bool                `json:"default_allow"`
	Grants       map[string][]string `json:"grants"`
}

// Limits bounds inbound requests.
type Limits struct {
	MaxBodyBytes   int64    `json:"max_body_bytes"`
	RequestTimeout Duration `json:"request_timeout"`
	ShutdownGrace  Duration `json:"shutdown_grace"`
}

// Default returns a configuration that is safe to run as-is: it binds to
// loopback only, uses the offline echo provider, and denies every capability
// that has not been explicitly granted.
func Default() Config {
	return Config{
		Addr:    "127.0.0.1:7717",
		DataDir: "./data",
		Log: Log{
			Level:  "info",
			Format: "text",
		},
		Provider: Provider{
			Name:      "echo",
			Timeout:   Duration(30 * time.Second),
			MaxTokens: 1024,
		},
		Session: Session{
			MaxTurns: 40,
			TTL:      Duration(2 * time.Hour),
		},
		Router: Router{
			MinScore: 0.6,
		},
		Permissions: Permissions{
			DefaultAllow: false,
			Grants: map[string][]string{
				"clock":  {"clock.read"},
				"recall": {"session.read"},
				"help":   {},
				"math":   {},
			},
		},
		Limits: Limits{
			MaxBodyBytes:   1 << 20, // 1 MiB
			RequestTimeout: Duration(45 * time.Second),
			ShutdownGrace:  Duration(10 * time.Second),
		},
	}
}

// LoadFile decodes a JSON config file over the top of base. Unknown fields are
// rejected so typos fail loudly instead of being silently ignored.
func LoadFile(path string, base Config) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return base, fmt.Errorf("open config: %w", err)
	}
	defer f.Close()

	dec := json.NewDecoder(f)
	dec.DisallowUnknownFields()
	cfg := base
	if err := dec.Decode(&cfg); err != nil {
		return base, fmt.Errorf("decode config %s: %w", path, err)
	}
	return cfg, nil
}

// ApplyEnv overlays environment variables onto cfg.
func ApplyEnv(cfg Config, lookup func(string) (string, bool)) (Config, error) {
	if lookup == nil {
		lookup = os.LookupEnv
	}
	if v, ok := lookup(EnvAddr); ok {
		cfg.Addr = v
	}
	if v, ok := lookup(EnvDataDir); ok {
		cfg.DataDir = v
	}
	if v, ok := lookup(EnvLogLevel); ok {
		cfg.Log.Level = v
	}
	if v, ok := lookup(EnvLogFmt); ok {
		cfg.Log.Format = v
	}
	if v, ok := lookup(EnvProvider); ok {
		cfg.Provider.Name = v
	}
	if v, ok := lookup(EnvMinScore); ok {
		f, err := strconv.ParseFloat(v, 64)
		if err != nil {
			return cfg, fmt.Errorf("%s: %w", EnvMinScore, err)
		}
		cfg.Router.MinScore = f
	}
	return cfg, nil
}

// Resolve runs the full precedence chain. An empty path skips the file layer.
func Resolve(path string, lookup func(string) (string, bool)) (Config, error) {
	cfg := Default()
	if path != "" {
		var err error
		cfg, err = LoadFile(path, cfg)
		if err != nil {
			return cfg, err
		}
	}
	cfg, err := ApplyEnv(cfg, lookup)
	if err != nil {
		return cfg, err
	}
	if err := cfg.Validate(); err != nil {
		return cfg, err
	}
	return cfg, nil
}

var validLevels = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
var validFormats = map[string]bool{"text": true, "json": true}

// Validate reports every problem it finds at once rather than stopping at the
// first, so a misconfigured deployment can be fixed in a single pass.
func (c Config) Validate() error {
	var errs []error

	if strings.TrimSpace(c.Addr) == "" {
		errs = append(errs, errors.New("addr must not be empty"))
	}
	if strings.TrimSpace(c.DataDir) == "" {
		errs = append(errs, errors.New("data_dir must not be empty"))
	}
	if !validLevels[c.Log.Level] {
		errs = append(errs, fmt.Errorf("log.level %q must be one of debug, info, warn, error", c.Log.Level))
	}
	if !validFormats[c.Log.Format] {
		errs = append(errs, fmt.Errorf("log.format %q must be text or json", c.Log.Format))
	}
	if strings.TrimSpace(c.Provider.Name) == "" {
		errs = append(errs, errors.New("provider.name must not be empty"))
	}
	if c.Provider.Timeout <= 0 {
		errs = append(errs, errors.New("provider.timeout must be positive"))
	}
	if c.Provider.MaxTokens <= 0 {
		errs = append(errs, errors.New("provider.max_tokens must be positive"))
	}
	if c.Session.MaxTurns <= 0 {
		errs = append(errs, errors.New("session.max_turns must be positive"))
	}
	if c.Session.TTL <= 0 {
		errs = append(errs, errors.New("session.ttl must be positive"))
	}
	if c.Router.MinScore < 0 || c.Router.MinScore > 1 {
		errs = append(errs, fmt.Errorf("router.min_score %v must be between 0 and 1", c.Router.MinScore))
	}
	if c.Limits.MaxBodyBytes <= 0 {
		errs = append(errs, errors.New("limits.max_body_bytes must be positive"))
	}
	if c.Limits.RequestTimeout <= 0 {
		errs = append(errs, errors.New("limits.request_timeout must be positive"))
	}
	if c.Limits.ShutdownGrace <= 0 {
		errs = append(errs, errors.New("limits.shutdown_grace must be positive"))
	}

	return errors.Join(errs...)
}

// JournalPath returns the location of the append-only event log.
func (c Config) JournalPath() string {
	return strings.TrimRight(c.DataDir, "/\\") + "/journal.jsonl"
}
