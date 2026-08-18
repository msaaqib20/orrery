package httpapi

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/msaaqib20/orrery/internal/journal"
	"github.com/msaaqib20/orrery/internal/logging"
	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/provider"
	"github.com/msaaqib20/orrery/internal/router"
	"github.com/msaaqib20/orrery/internal/runtime"
	"github.com/msaaqib20/orrery/internal/session"
	"github.com/msaaqib20/orrery/internal/skill"
)

func newTestServer(t *testing.T, grants map[string][]string) http.Handler {
	t.Helper()

	skills := skill.NewRegistry()
	if err := skill.RegisterBuiltins(skills); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}
	if grants == nil {
		grants = map[string][]string{
			"clock":  {"clock.read"},
			"recall": {"session.read"},
		}
	}
	policy, err := permission.FromGrants(false, grants)
	if err != nil {
		t.Fatalf("FromGrants: %v", err)
	}

	providers := provider.NewRegistry()
	if err := providers.Register(provider.Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	rt, err := runtime.New(runtime.Options{
		Logger:          logging.Discard(),
		Sessions:        session.NewStore(20, time.Hour),
		Skills:          skills,
		Router:          router.New(0.6),
		Providers:       providers,
		Policy:          policy,
		Journal:         journal.NewMemory(),
		ProviderName:    "echo",
		ProviderTimeout: time.Second,
		MaxTokens:       128,
	})
	if err != nil {
		t.Fatalf("runtime.New: %v", err)
	}

	return New(rt, Options{Logger: logging.Discard()}).Handler()
}

func do(t *testing.T, h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	var r *http.Request
	if body == "" {
		r = httptest.NewRequest(method, path, nil)
	} else {
		r = httptest.NewRequest(method, path, strings.NewReader(body))
		r.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder) T {
	t.Helper()
	var out T
	if err := json.Unmarshal(w.Body.Bytes(), &out); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, w.Body.String())
	}
	return out
}

func TestHealthz(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodGet, "/healthz", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	got := decode[HealthResponse](t, w)
	if got.Status != "ok" {
		t.Errorf("Status = %q", got.Status)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
		t.Errorf("Content-Type = %q", ct)
	}
}

func TestReadyz(t *testing.T) {
	h := newTestServer(t, nil)
	got := decode[ReadyResponse](t, do(t, h, http.MethodGet, "/readyz", ""))

	if got.Status != "ready" {
		t.Errorf("Status = %q", got.Status)
	}
	if got.Skills != 5 {
		t.Errorf("Skills = %d, want 5", got.Skills)
	}
	if got.Active != "echo" {
		t.Errorf("Active = %q", got.Active)
	}
}

func TestVersionEndpoint(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodGet, "/v1/version", "")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	var payload map[string]any
	if err := json.Unmarshal(w.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if payload["version"] == "" {
		t.Error("version is empty")
	}
}

func TestSkillsEndpoint(t *testing.T) {
	h := newTestServer(t, nil)
	got := decode[SkillsResponse](t, do(t, h, http.MethodGet, "/v1/skills", ""))

	if len(got.Skills) != 5 {
		t.Fatalf("returned %d skills, want 5", len(got.Skills))
	}
	if got.Skills[0].Name != "clock" {
		t.Errorf("first skill = %q, want clock (sorted)", got.Skills[0].Name)
	}
	var clock SkillView
	for _, s := range got.Skills {
		if s.Name == "clock" {
			clock = s
		}
	}
	if len(clock.Capabilities) != 1 || clock.Capabilities[0] != "clock.read" {
		t.Errorf("clock capabilities = %v", clock.Capabilities)
	}
}

func TestMessageRoutesToSkill(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodPost, "/v1/message", `{"text":"ping"}`)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	got := decode[MessageResponse](t, w)
	if got.Text != "pong" {
		t.Errorf("Text = %q", got.Text)
	}
	if got.Source != "skill" || got.Skill != "ping" {
		t.Errorf("Source = %q, Skill = %q", got.Source, got.Skill)
	}
	if got.SessionID == "" {
		t.Error("no session id was returned")
	}
}

func TestMessageFallsThroughToProvider(t *testing.T) {
	h := newTestServer(t, nil)
	got := decode[MessageResponse](t, do(t, h, http.MethodPost, "/v1/message", `{"text":"describe a heron"}`))

	if got.Source != "provider" {
		t.Errorf("Source = %q, want provider", got.Source)
	}
	if !strings.Contains(got.Text, "describe a heron") {
		t.Errorf("Text = %q", got.Text)
	}
}

func TestMessageCarriesSessionAcrossCalls(t *testing.T) {
	h := newTestServer(t, nil)

	first := decode[MessageResponse](t, do(t, h, http.MethodPost, "/v1/message", `{"text":"ping"}`))
	body := `{"session_id":"` + first.SessionID + `","text":"ping"}`
	second := decode[MessageResponse](t, do(t, h, http.MethodPost, "/v1/message", body))

	if second.SessionID != first.SessionID {
		t.Errorf("session changed: %q then %q", first.SessionID, second.SessionID)
	}

	w := do(t, h, http.MethodGet, "/v1/sessions/"+first.SessionID, "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}
	sess := decode[SessionResponse](t, w)
	if len(sess.Turns) != 4 {
		t.Errorf("session holds %d turns, want 4", len(sess.Turns))
	}
}

func TestMessageRejectsEmptyText(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodPost, "/v1/message", `{"text":"   "}`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	got := decode[ErrorBody](t, w)
	if got.Error.Code != "empty_text" {
		t.Errorf("code = %q", got.Error.Code)
	}
}

func TestMessageRejectsMalformedJSON(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodPost, "/v1/message", `{"text":`)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := decode[ErrorBody](t, w); got.Error.Code != "bad_json" {
		t.Errorf("code = %q", got.Error.Code)
	}
}

func TestMessageRejectsUnknownFields(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodPost, "/v1/message", `{"text":"ping","tempreature":1}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400 for an unknown field", w.Code)
	}
}

func TestMessageRejectsEmptyBody(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodPost, "/v1/message", "")

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", w.Code)
	}
	if got := decode[ErrorBody](t, w); got.Error.Code != "empty_body" {
		t.Errorf("code = %q", got.Error.Code)
	}
}

func TestMessageRejectsOversizedBody(t *testing.T) {
	h := newTestServer(t, nil)
	huge := `{"text":"` + strings.Repeat("a", 2<<20) + `"}`
	w := do(t, h, http.MethodPost, "/v1/message", huge)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413", w.Code)
	}
}

func TestDeniedSkillReturns403(t *testing.T) {
	h := newTestServer(t, map[string][]string{})
	w := do(t, h, http.MethodPost, "/v1/message", `{"text":"what time is it"}`)

	if w.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want 403, body = %s", w.Code, w.Body.String())
	}
	got := decode[ErrorBody](t, w)
	if got.Error.Code != "permission_denied" {
		t.Errorf("code = %q", got.Error.Code)
	}
	if got.Error.RequestID == "" {
		t.Error("the error did not carry a request id")
	}
}

func TestUnknownSessionReturns404(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodGet, "/v1/sessions/does-not-exist", "")

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
	if got := decode[ErrorBody](t, w); got.Error.Code != "session_not_found" {
		t.Errorf("code = %q", got.Error.Code)
	}
}

func TestMethodNotAllowed(t *testing.T) {
	h := newTestServer(t, nil)
	if w := do(t, h, http.MethodGet, "/v1/message", ""); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("GET /v1/message returned %d, want 405", w.Code)
	}
	if w := do(t, h, http.MethodPost, "/healthz", `{}`); w.Code != http.StatusMethodNotAllowed {
		t.Errorf("POST /healthz returned %d, want 405", w.Code)
	}
}

func TestUnknownRouteReturns404(t *testing.T) {
	h := newTestServer(t, nil)
	if w := do(t, h, http.MethodGet, "/v1/nope", ""); w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want 404", w.Code)
	}
}

func TestRequestIDIsGenerated(t *testing.T) {
	h := newTestServer(t, nil)
	w := do(t, h, http.MethodGet, "/healthz", "")

	if w.Header().Get(HeaderRequestID) == "" {
		t.Error("no request id was set on the response")
	}
}

func TestRequestIDIsEchoed(t *testing.T) {
	h := newTestServer(t, nil)
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set(HeaderRequestID, "client-supplied-id")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	if got := w.Header().Get(HeaderRequestID); got != "client-supplied-id" {
		t.Errorf("request id = %q, want the client value to be preserved", got)
	}
}

func TestOverlongRequestIDIsReplaced(t *testing.T) {
	h := newTestServer(t, nil)
	r := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	r.Header.Set(HeaderRequestID, strings.Repeat("x", 200))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)

	got := w.Header().Get(HeaderRequestID)
	if len(got) > 64 {
		t.Errorf("request id of length %d was accepted", len(got))
	}
}

func TestRecovererTurnsPanicIntoFiveHundred(t *testing.T) {
	panicking := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("boom")
	})
	h := chain(panicking, requestID, accessLog(logging.Discard()), recoverer(logging.Discard()))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/boom", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if got := decode[ErrorBody](t, w); got.Error.Code != "internal_error" {
		t.Errorf("code = %q", got.Error.Code)
	}
}

func TestChainAppliesOutermostFirst(t *testing.T) {
	var order []string
	mark := func(name string) Middleware {
		return func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				order = append(order, name)
				next.ServeHTTP(w, r)
			})
		}
	}
	h := chain(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}), mark("first"), mark("second"))
	h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/", nil))

	if strings.Join(order, ",") != "first,second" {
		t.Errorf("middleware ran in order %v", order)
	}
}

func TestWriteJSONHandlesUnencodableValue(t *testing.T) {
	w := httptest.NewRecorder()
	writeJSON(w, logging.Discard(), http.StatusOK, make(chan int))

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want 500", w.Code)
	}
	if !bytes.Contains(w.Body.Bytes(), []byte("encode_failed")) {
		t.Errorf("body = %s", w.Body.String())
	}
}

func TestRequestIDFromEmptyContext(t *testing.T) {
	if got := RequestIDFrom(httptest.NewRequest(http.MethodGet, "/", nil).Context()); got != "" {
		t.Errorf("RequestIDFrom = %q, want empty", got)
	}
}
