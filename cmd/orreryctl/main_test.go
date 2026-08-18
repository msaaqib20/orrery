package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func newClient(t *testing.T, h http.Handler) (*client, func()) {
	t.Helper()
	srv := httptest.NewServer(h)
	c := &client{base: srv.URL, http: &http.Client{Timeout: 5 * time.Second}}
	return c, srv.Close
}

func TestErrorMessageParsesEnvelope(t *testing.T) {
	raw := []byte(`{"error":{"code":"permission_denied","message":"clock may not use clock.read"}}`)
	got := errorMessage(raw)
	if !strings.Contains(got, "permission_denied") || !strings.Contains(got, "clock.read") {
		t.Errorf("errorMessage = %q", got)
	}
}

func TestErrorMessageFallsBackToRawBody(t *testing.T) {
	if got := errorMessage([]byte("  plain text  ")); got != "plain text" {
		t.Errorf("errorMessage = %q", got)
	}
}

func TestErrorMessageHandlesEmptyEnvelope(t *testing.T) {
	if got := errorMessage([]byte(`{"error":{}}`)); got == "" {
		t.Error("errorMessage returned nothing for an empty envelope")
	}
}

func TestEnvOr(t *testing.T) {
	if got := envOr("ORRERY_TEST_UNSET_VAR", "fallback"); got != "fallback" {
		t.Errorf("envOr = %q", got)
	}
	t.Setenv("ORRERY_TEST_SET_VAR", "value")
	if got := envOr("ORRERY_TEST_SET_VAR", "fallback"); got != "value" {
		t.Errorf("envOr = %q", got)
	}
	t.Setenv("ORRERY_TEST_EMPTY_VAR", "")
	if got := envOr("ORRERY_TEST_EMPTY_VAR", "fallback"); got != "fallback" {
		t.Errorf("envOr on an empty value = %q", got)
	}
}

func TestDoSendsJSONBody(t *testing.T) {
	var seen map[string]string
	c, closeSrv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q", ct)
		}
		if err := json.NewDecoder(r.Body).Decode(&seen); err != nil {
			t.Errorf("decode: %v", err)
		}
		w.Write([]byte(`{"text":"ok"}`))
	}))
	defer closeSrv()

	var out struct {
		Text string `json:"text"`
	}
	if _, err := c.do(context.Background(), http.MethodPost, "/v1/message", map[string]string{"text": "hi"}, &out); err != nil {
		t.Fatalf("do: %v", err)
	}
	if seen["text"] != "hi" {
		t.Errorf("server saw %v", seen)
	}
	if out.Text != "ok" {
		t.Errorf("Text = %q", out.Text)
	}
}

func TestDoSurfacesServerErrors(t *testing.T) {
	c, closeSrv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		w.Write([]byte(`{"error":{"code":"permission_denied","message":"nope"}}`))
	}))
	defer closeSrv()

	_, err := c.do(context.Background(), http.MethodGet, "/v1/skills", nil, nil)
	if err == nil {
		t.Fatal("expected an error for a 403 response")
	}
	if !strings.Contains(err.Error(), "permission_denied") {
		t.Errorf("error = %v", err)
	}
}

func TestDoRejectsUndecodableResponse(t *testing.T) {
	c, closeSrv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`not json`))
	}))
	defer closeSrv()

	var out struct {
		Text string `json:"text"`
	}
	if _, err := c.do(context.Background(), http.MethodGet, "/v1/skills", nil, &out); err == nil {
		t.Fatal("expected a decode error")
	}
}

func TestDoHonoursContextCancellation(t *testing.T) {
	c, closeSrv := newClient(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer closeSrv()

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	if _, err := c.do(ctx, http.MethodGet, "/readyz", nil, nil); err == nil {
		t.Fatal("expected a cancellation error")
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("do waited %v instead of honouring the context", elapsed)
	}
}
