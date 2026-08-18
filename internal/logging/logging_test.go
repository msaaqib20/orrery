package logging

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestParseLevel(t *testing.T) {
	cases := map[string]slog.Level{
		"debug":   slog.LevelDebug,
		"INFO":    slog.LevelInfo,
		"":        slog.LevelInfo,
		" warn ":  slog.LevelWarn,
		"warning": slog.LevelWarn,
		"error":   slog.LevelError,
	}
	for in, want := range cases {
		got, err := ParseLevel(in)
		if err != nil {
			t.Errorf("ParseLevel(%q) returned %v", in, err)
			continue
		}
		if got != want {
			t.Errorf("ParseLevel(%q) = %v, want %v", in, got, want)
		}
	}
	if _, err := ParseLevel("loud"); err == nil {
		t.Error("expected an unknown level to be rejected")
	}
}

func TestNewJSONHandlerEmitsStructuredOutput(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "info", "json")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("started", slog.String("component", "test"))

	var rec map[string]any
	if err := json.Unmarshal(buf.Bytes(), &rec); err != nil {
		t.Fatalf("output was not valid JSON: %v (%s)", err, buf.String())
	}
	if rec["msg"] != "started" {
		t.Errorf("msg = %v", rec["msg"])
	}
	if rec["component"] != "test" {
		t.Errorf("component = %v", rec["component"])
	}
}

func TestNewRespectsLevel(t *testing.T) {
	var buf bytes.Buffer
	log, err := New(&buf, "warn", "text")
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	log.Info("suppressed")
	if buf.Len() != 0 {
		t.Errorf("info record leaked through a warn-level logger: %s", buf.String())
	}
	log.Warn("kept")
	if !strings.Contains(buf.String(), "kept") {
		t.Error("warn record was dropped")
	}
}

func TestNewRejectsUnknownFormat(t *testing.T) {
	if _, err := New(&bytes.Buffer{}, "info", "yaml"); err == nil {
		t.Error("expected an unknown format to be rejected")
	}
}

func TestDiscardIsSilent(t *testing.T) {
	log := Discard()
	log.Error("this should not panic")
}
