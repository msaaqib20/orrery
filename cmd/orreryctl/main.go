// Command orreryctl is a thin client for a running orreryd.
//
// It speaks the same JSON API as any other client and holds no logic of its
// own, so anything it can do is reproducible with curl.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/msaaqib20/orrery/internal/version"
)

const (
	defaultBase    = "http://127.0.0.1:7717"
	defaultTimeout = 60 * time.Second
	usage          = `orreryctl - a client for the orrery daemon

usage:
  orreryctl [flags] <command> [arguments]

commands:
  ask <text>       send a message and print the reply
  skills           list the skills the daemon can handle directly
  session <id>     print a session transcript
  ping             check that the daemon is answering
  version          print client and daemon versions

flags:
`
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "orreryctl: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	var (
		base      = flag.String("addr", envOr("ORRERY_ADDR_URL", defaultBase), "base URL of the daemon")
		sessionID = flag.String("session", "", "session id to continue")
		timeout   = flag.Duration("timeout", defaultTimeout, "request timeout")
		asJSON    = flag.Bool("json", false, "print raw JSON responses")
	)
	flag.Usage = func() {
		fmt.Fprint(flag.CommandLine.Output(), usage)
		flag.PrintDefaults()
	}
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		flag.Usage()
		return errors.New("no command given")
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	c := &client{
		base:   strings.TrimRight(*base, "/"),
		http:   &http.Client{Timeout: *timeout},
		asJSON: *asJSON,
	}

	switch args[0] {
	case "ask":
		if len(args) < 2 {
			return errors.New("ask needs some text")
		}
		return c.ask(ctx, *sessionID, strings.Join(args[1:], " "))
	case "skills":
		return c.skills(ctx)
	case "session":
		if len(args) < 2 {
			return errors.New("session needs an id")
		}
		return c.session(ctx, args[1])
	case "ping":
		return c.ping(ctx)
	case "version":
		return c.version(ctx)
	default:
		flag.Usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

type client struct {
	base   string
	http   *http.Client
	asJSON bool
}

func (c *client) ask(ctx context.Context, sessionID, text string) error {
	payload := map[string]string{"text": text}
	if sessionID != "" {
		payload["session_id"] = sessionID
	}

	var out struct {
		SessionID string `json:"session_id"`
		Text      string `json:"text"`
		Source    string `json:"source"`
		Skill     string `json:"skill"`
		Provider  string `json:"provider"`
		ElapsedMS int64  `json:"elapsed_ms"`
	}
	raw, err := c.do(ctx, http.MethodPost, "/v1/message", payload, &out)
	if err != nil {
		return err
	}
	if c.asJSON {
		fmt.Println(string(raw))
		return nil
	}

	fmt.Println(out.Text)
	via := out.Provider
	if out.Source == "skill" {
		via = out.Skill
	}
	fmt.Fprintf(os.Stderr, "\n[%s via %s, %dms, session %s]\n", out.Source, via, out.ElapsedMS, out.SessionID)
	return nil
}

func (c *client) skills(ctx context.Context) error {
	var out struct {
		Skills []struct {
			Name         string   `json:"name"`
			Summary      string   `json:"summary"`
			Capabilities []string `json:"capabilities"`
		} `json:"skills"`
	}
	raw, err := c.do(ctx, http.MethodGet, "/v1/skills", nil, &out)
	if err != nil {
		return err
	}
	if c.asJSON {
		fmt.Println(string(raw))
		return nil
	}

	for _, s := range out.Skills {
		caps := "-"
		if len(s.Capabilities) > 0 {
			caps = strings.Join(s.Capabilities, ", ")
		}
		fmt.Printf("%-8s %-50s %s\n", s.Name, s.Summary, caps)
	}
	return nil
}

func (c *client) session(ctx context.Context, id string) error {
	var out struct {
		ID    string `json:"id"`
		Turns []struct {
			Role string `json:"role"`
			Text string `json:"text"`
			At   string `json:"at"`
		} `json:"turns"`
	}
	raw, err := c.do(ctx, http.MethodGet, "/v1/sessions/"+id, nil, &out)
	if err != nil {
		return err
	}
	if c.asJSON {
		fmt.Println(string(raw))
		return nil
	}

	fmt.Printf("session %s (%d turns)\n", out.ID, len(out.Turns))
	for _, t := range out.Turns {
		fmt.Printf("  %-9s %s\n", t.Role, t.Text)
	}
	return nil
}

func (c *client) ping(ctx context.Context) error {
	var out struct {
		Status string `json:"status"`
		Skills int    `json:"skills"`
		Active string `json:"active_provider"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/readyz", nil, &out); err != nil {
		return err
	}
	fmt.Printf("%s: %d skills, provider %s\n", out.Status, out.Skills, out.Active)
	return nil
}

func (c *client) version(ctx context.Context) error {
	fmt.Printf("client: %s\n", version.Get().String())

	var out struct {
		Version   string `json:"version"`
		GoVersion string `json:"go_version"`
		Platform  string `json:"platform"`
	}
	if _, err := c.do(ctx, http.MethodGet, "/v1/version", nil, &out); err != nil {
		return fmt.Errorf("daemon unreachable: %w", err)
	}
	fmt.Printf("daemon: orrery %s %s %s\n", out.Version, out.GoVersion, out.Platform)
	return nil
}

// do performs one request and decodes the response into v.
func (c *client) do(ctx context.Context, method, path string, payload any, v any) ([]byte, error) {
	var body io.Reader
	if payload != nil {
		encoded, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("encode request: %w", err)
		}
		body = bytes.NewReader(encoded)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.base+path, body)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("call %s: %w", path, err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode >= 400 {
		return raw, fmt.Errorf("%s: %s", resp.Status, errorMessage(raw))
	}
	if v != nil {
		if err := json.Unmarshal(raw, v); err != nil {
			return raw, fmt.Errorf("decode response: %w", err)
		}
	}
	return raw, nil
}

// errorMessage pulls the message out of the daemon's error envelope, falling
// back to the raw body when the response is not in that shape.
func errorMessage(raw []byte) string {
	var envelope struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Error.Message != "" {
		if envelope.Error.Code != "" {
			return envelope.Error.Code + ": " + envelope.Error.Message
		}
		return envelope.Error.Message
	}
	return strings.TrimSpace(string(raw))
}

func envOr(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return fallback
}
