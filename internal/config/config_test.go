package config

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDefaultIsValid(t *testing.T) {
	if err := Default().Validate(); err != nil {
		t.Fatalf("Default() failed validation: %v", err)
	}
}

func TestDefaultBindsLoopback(t *testing.T) {
	cfg := Default()
	if !strings.HasPrefix(cfg.Addr, "127.0.0.1:") {
		t.Errorf("default Addr = %q, expected a loopback bind", cfg.Addr)
	}
	if cfg.Permissions.DefaultAllow {
		t.Error("default policy must deny capabilities that were not granted")
	}
}

func TestDurationRoundTrip(t *testing.T) {
	type holder struct {
		D Duration `json:"d"`
	}
	in := holder{D: Duration(90 * time.Second)}
	b, err := json.Marshal(in)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(b), `"1m30s"`) {
		t.Fatalf("marshal produced %s, expected a duration string", b)
	}
	var out holder
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if out.D.D() != 90*time.Second {
		t.Errorf("round trip = %v, want 1m30s", out.D.D())
	}
}

func TestDurationAcceptsNanoseconds(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte("1500000000"), &d); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if d.D() != 1500*time.Millisecond {
		t.Errorf("got %v, want 1.5s", d.D())
	}
}

func TestDurationRejectsGarbage(t *testing.T) {
	var d Duration
	if err := json.Unmarshal([]byte(`"not-a-duration"`), &d); err == nil {
		t.Error("expected an error for an unparsable duration")
	}
	if err := json.Unmarshal([]byte(`true`), &d); err == nil {
		t.Error("expected an error for a boolean duration")
	}
}

func writeConfig(t *testing.T, body string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	return path
}

func TestLoadFileOverlaysOntoBase(t *testing.T) {
	path := writeConfig(t, `{"addr":"127.0.0.1:9999","provider":{"timeout":"5s"}}`)

	cfg, err := LoadFile(path, Default())
	if err != nil {
		t.Fatalf("LoadFile: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" {
		t.Errorf("Addr = %q, want the file value", cfg.Addr)
	}
	if cfg.Provider.Timeout.D() != 5*time.Second {
		t.Errorf("provider timeout = %v, want 5s", cfg.Provider.Timeout.D())
	}
	if cfg.Provider.Name != "echo" {
		t.Errorf("provider name = %q, want the default to survive a partial override", cfg.Provider.Name)
	}
}

func TestLoadFileRejectsUnknownFields(t *testing.T) {
	path := writeConfig(t, `{"addres":"127.0.0.1:9999"}`)
	if _, err := LoadFile(path, Default()); err == nil {
		t.Fatal("expected a typo in a field name to be rejected")
	}
}

func TestLoadFileMissingFile(t *testing.T) {
	if _, err := LoadFile(filepath.Join(t.TempDir(), "absent.json"), Default()); err == nil {
		t.Fatal("expected an error for a missing config file")
	}
}

func TestApplyEnvOverridesFile(t *testing.T) {
	env := map[string]string{
		EnvAddr:     "0.0.0.0:8080",
		EnvLogLevel: "debug",
		EnvProvider: "static",
		EnvMinScore: "0.25",
	}
	lookup := func(k string) (string, bool) {
		v, ok := env[k]
		return v, ok
	}

	cfg, err := ApplyEnv(Default(), lookup)
	if err != nil {
		t.Fatalf("ApplyEnv: %v", err)
	}
	if cfg.Addr != "0.0.0.0:8080" {
		t.Errorf("Addr = %q", cfg.Addr)
	}
	if cfg.Log.Level != "debug" {
		t.Errorf("Log.Level = %q", cfg.Log.Level)
	}
	if cfg.Provider.Name != "static" {
		t.Errorf("Provider.Name = %q", cfg.Provider.Name)
	}
	if cfg.Router.MinScore != 0.25 {
		t.Errorf("Router.MinScore = %v", cfg.Router.MinScore)
	}
}

func TestApplyEnvRejectsBadFloat(t *testing.T) {
	lookup := func(k string) (string, bool) {
		if k == EnvMinScore {
			return "high", true
		}
		return "", false
	}
	if _, err := ApplyEnv(Default(), lookup); err == nil {
		t.Fatal("expected an unparsable min score to be rejected")
	}
}

func TestResolveAppliesFullPrecedence(t *testing.T) {
	path := writeConfig(t, `{"addr":"127.0.0.1:1111","log":{"level":"warn","format":"json"}}`)
	lookup := func(k string) (string, bool) {
		if k == EnvLogLevel {
			return "error", true
		}
		return "", false
	}

	cfg, err := Resolve(path, lookup)
	if err != nil {
		t.Fatalf("Resolve: %v", err)
	}
	if cfg.Addr != "127.0.0.1:1111" {
		t.Errorf("Addr = %q, want the file value to win over the default", cfg.Addr)
	}
	if cfg.Log.Format != "json" {
		t.Errorf("Log.Format = %q, want the file value", cfg.Log.Format)
	}
	if cfg.Log.Level != "error" {
		t.Errorf("Log.Level = %q, want the env value to win over the file", cfg.Log.Level)
	}
}

func TestValidateReportsEveryProblem(t *testing.T) {
	cfg := Default()
	cfg.Addr = ""
	cfg.Log.Level = "chatty"
	cfg.Router.MinScore = 4

	err := cfg.Validate()
	if err == nil {
		t.Fatal("expected validation to fail")
	}
	msg := err.Error()
	for _, want := range []string{"addr", "log.level", "router.min_score"} {
		if !strings.Contains(msg, want) {
			t.Errorf("error %q does not mention %q", msg, want)
		}
	}
}

func TestJournalPathTrimsTrailingSeparator(t *testing.T) {
	cfg := Default()
	cfg.DataDir = "/var/lib/orrery/"
	if got := cfg.JournalPath(); got != "/var/lib/orrery/journal.jsonl" {
		t.Errorf("JournalPath() = %q", got)
	}
}
