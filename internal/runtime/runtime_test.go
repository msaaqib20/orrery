package runtime

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msaaqib20/orrery/internal/journal"
	"github.com/msaaqib20/orrery/internal/logging"
	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/provider"
	"github.com/msaaqib20/orrery/internal/router"
	"github.com/msaaqib20/orrery/internal/session"
	"github.com/msaaqib20/orrery/internal/skill"
)

type harness struct {
	rt       *Runtime
	journal  *journal.Memory
	sessions *session.Store
	policy   *permission.Policy
	skills   *skill.Registry
	static   *provider.Static
}

func newHarness(t *testing.T, mutate func(*Options)) *harness {
	t.Helper()

	skills := skill.NewRegistry()
	if err := skill.RegisterBuiltins(skills); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	policy, err := permission.FromGrants(false, map[string][]string{
		"clock":  {"clock.read"},
		"recall": {"session.read"},
	})
	if err != nil {
		t.Fatalf("FromGrants: %v", err)
	}

	static := &provider.Static{ProviderName: "static", Reply: "provider answer"}
	providers := provider.NewRegistry()
	if err := providers.Register(static); err != nil {
		t.Fatalf("Register: %v", err)
	}

	mem := journal.NewMemory()
	sessions := session.NewStore(20, time.Hour)

	opts := Options{
		Logger:          logging.Discard(),
		Sessions:        sessions,
		Skills:          skills,
		Router:          router.New(0.6),
		Providers:       providers,
		Policy:          policy,
		Journal:         mem,
		ProviderName:    "static",
		ProviderTimeout: time.Second,
		MaxTokens:       256,
	}
	if mutate != nil {
		mutate(&opts)
	}

	rt, err := New(opts)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	return &harness{rt: rt, journal: mem, sessions: sessions, policy: policy, skills: skills, static: static}
}

func TestNewRejectsMissingDependencies(t *testing.T) {
	base := func() Options {
		providers := provider.NewRegistry()
		_ = providers.Register(provider.Echo{})
		return Options{
			Sessions:     session.NewStore(10, time.Hour),
			Skills:       skill.NewRegistry(),
			Router:       router.New(0.6),
			Providers:    providers,
			Policy:       permission.NewPolicy(false),
			Journal:      journal.NewMemory(),
			ProviderName: "echo",
		}
	}

	cases := map[string]func(*Options){
		"sessions":  func(o *Options) { o.Sessions = nil },
		"skills":    func(o *Options) { o.Skills = nil },
		"router":    func(o *Options) { o.Router = nil },
		"providers": func(o *Options) { o.Providers = nil },
		"policy":    func(o *Options) { o.Policy = nil },
		"journal":   func(o *Options) { o.Journal = nil },
		"provider":  func(o *Options) { o.ProviderName = "" },
	}
	for label, break_ := range cases {
		opts := base()
		break_(&opts)
		if _, err := New(opts); !errors.Is(err, ErrNotReady) {
			t.Errorf("%s: New returned %v, want ErrNotReady", label, err)
		}
	}
}

func TestNewRejectsUnregisteredProvider(t *testing.T) {
	opts := Options{
		Sessions:     session.NewStore(10, time.Hour),
		Skills:       skill.NewRegistry(),
		Router:       router.New(0.6),
		Providers:    provider.NewRegistry(),
		Policy:       permission.NewPolicy(false),
		Journal:      journal.NewMemory(),
		ProviderName: "absent",
	}
	if _, err := New(opts); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("New returned %v, want provider.ErrNotFound", err)
	}
}

func TestHandleRejectsEmptyText(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.rt.Handle(context.Background(), Request{Text: "   "}); !errors.Is(err, ErrEmptyText) {
		t.Errorf("Handle returned %v, want ErrEmptyText", err)
	}
}

func TestHandleRoutesToSkill(t *testing.T) {
	h := newHarness(t, nil)

	reply, err := h.rt.Handle(context.Background(), Request{Text: "ping"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Source != SourceSkill {
		t.Errorf("Source = %q, want %q", reply.Source, SourceSkill)
	}
	if reply.Skill != "ping" {
		t.Errorf("Skill = %q", reply.Skill)
	}
	if reply.Text != "pong" {
		t.Errorf("Text = %q", reply.Text)
	}
	if h.static.Calls() != 0 {
		t.Error("the provider was called even though a skill handled the request")
	}
}

func TestHandleFallsThroughToProvider(t *testing.T) {
	h := newHarness(t, nil)

	reply, err := h.rt.Handle(context.Background(), Request{Text: "write a haiku about tides"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Source != SourceProvider {
		t.Errorf("Source = %q, want %q", reply.Source, SourceProvider)
	}
	if reply.Text != "provider answer" {
		t.Errorf("Text = %q", reply.Text)
	}
	if h.static.Calls() != 1 {
		t.Errorf("provider called %d times, want 1", h.static.Calls())
	}
}

func TestHandleAllocatesSessionID(t *testing.T) {
	h := newHarness(t, nil)

	reply, err := h.rt.Handle(context.Background(), Request{Text: "ping"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.SessionID == "" {
		t.Fatal("Handle did not allocate a session ID")
	}

	sess, err := h.rt.Session(reply.SessionID)
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if len(sess.Turns) != 2 {
		t.Fatalf("session holds %d turns, want the user turn and the reply", len(sess.Turns))
	}
	if sess.Turns[0].Role != session.RoleUser || sess.Turns[1].Role != session.RoleAssistant {
		t.Errorf("turn roles = %v, %v", sess.Turns[0].Role, sess.Turns[1].Role)
	}
}

func TestHandleReusesSession(t *testing.T) {
	h := newHarness(t, nil)

	first, err := h.rt.Handle(context.Background(), Request{Text: "ping"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	second, err := h.rt.Handle(context.Background(), Request{SessionID: first.SessionID, Text: "ping"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if second.SessionID != first.SessionID {
		t.Errorf("session changed between turns: %q then %q", first.SessionID, second.SessionID)
	}

	sess, err := h.rt.Session(first.SessionID)
	if err != nil {
		t.Fatalf("Session: %v", err)
	}
	if len(sess.Turns) != 4 {
		t.Errorf("session holds %d turns, want 4 after two exchanges", len(sess.Turns))
	}
}

func TestProviderReceivesConversationHistory(t *testing.T) {
	h := newHarness(t, nil)

	first, err := h.rt.Handle(context.Background(), Request{Text: "tell me about herons"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if _, err := h.rt.Handle(context.Background(), Request{SessionID: first.SessionID, Text: "and egrets"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	reqs := h.static.Requests()
	if len(reqs) != 2 {
		t.Fatalf("provider saw %d requests, want 2", len(reqs))
	}
	last := reqs[1]
	if len(last.Messages) != 3 {
		t.Fatalf("second call carried %d messages, want 3", len(last.Messages))
	}
	if last.Messages[0].Content != "tell me about herons" {
		t.Errorf("first message = %q", last.Messages[0].Content)
	}
	if last.Messages[1].Role != provider.RoleAssistant {
		t.Errorf("second message role = %q, want assistant", last.Messages[1].Role)
	}
	if last.System == "" {
		t.Error("the system prompt was not passed through")
	}
	if last.MaxTokens != 256 {
		t.Errorf("MaxTokens = %d, want 256", last.MaxTokens)
	}
}

func TestDeniedSkillIsNotSilentlyReplaced(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		// A policy that grants nothing at all.
		o.Policy = permission.NewPolicy(false)
	})

	_, err := h.rt.Handle(context.Background(), Request{Text: "what time is it"})
	if !errors.Is(err, permission.ErrDenied) {
		t.Fatalf("Handle returned %v, want ErrDenied", err)
	}
	if h.static.Calls() != 0 {
		t.Error("a denied skill fell through to the provider")
	}

	var denied bool
	for _, k := range h.journal.Kinds() {
		if k == journal.KindDenied {
			denied = true
		}
	}
	if !denied {
		t.Error("the denial was not journalled")
	}
}

func TestSkillWithoutCapabilitiesNeedsNoGrant(t *testing.T) {
	h := newHarness(t, func(o *Options) {
		o.Policy = permission.NewPolicy(false)
	})

	reply, err := h.rt.Handle(context.Background(), Request{Text: "ping"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Skill != "ping" {
		t.Errorf("Skill = %q", reply.Skill)
	}
}

func TestDecliningSkillFallsThroughToProvider(t *testing.T) {
	h := newHarness(t, nil)

	// "what is" routes to math, but there is no expression to evaluate, so
	// the skill declines and the provider takes over.
	reply, err := h.rt.Handle(context.Background(), Request{Text: "what is the capital of Peru"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Source != SourceProvider {
		t.Errorf("Source = %q, want the provider fallback", reply.Source)
	}
	if h.static.Calls() != 1 {
		t.Errorf("provider called %d times, want 1", h.static.Calls())
	}
}

func TestMathSkillHandlesArithmetic(t *testing.T) {
	h := newHarness(t, nil)

	reply, err := h.rt.Handle(context.Background(), Request{Text: "what is 6 * 7"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Skill != "math" {
		t.Errorf("Skill = %q, want math", reply.Skill)
	}
	if !strings.HasSuffix(reply.Text, "42") {
		t.Errorf("Text = %q", reply.Text)
	}
}

func TestProviderErrorSurfaces(t *testing.T) {
	sentinel := errors.New("backend down")
	h := newHarness(t, nil)
	h.static.Err = sentinel

	if _, err := h.rt.Handle(context.Background(), Request{Text: "tell me a story"}); !errors.Is(err, sentinel) {
		t.Errorf("Handle returned %v, want the provider error", err)
	}

	var sawError bool
	for _, k := range h.journal.Kinds() {
		if k == journal.KindError {
			sawError = true
		}
	}
	if !sawError {
		t.Error("the provider failure was not journalled")
	}
}

func TestProviderTimeoutIsEnforced(t *testing.T) {
	h := newHarness(t, func(o *Options) { o.ProviderTimeout = 20 * time.Millisecond })
	h.static.Delay = 2 * time.Second

	start := time.Now()
	_, err := h.rt.Handle(context.Background(), Request{Text: "tell me a story"})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Handle returned %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Handle waited %v instead of honouring the provider timeout", elapsed)
	}
}

func TestJournalRecordsTheFullPath(t *testing.T) {
	h := newHarness(t, nil)

	if _, err := h.rt.Handle(context.Background(), Request{Text: "ping"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	got := strings.Join(h.journal.Kinds(), ",")
	want := "request,routed,skill,reply"
	if got != want {
		t.Errorf("journal recorded %q, want %q", got, want)
	}
}

func TestJournalFailureDoesNotFailTheRequest(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.journal.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	reply, err := h.rt.Handle(context.Background(), Request{Text: "ping"})
	if err != nil {
		t.Fatalf("a journal failure broke the request: %v", err)
	}
	if reply.Text != "pong" {
		t.Errorf("Text = %q", reply.Text)
	}
}

func TestSessionUnknownID(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.rt.Session("absent"); !errors.Is(err, ErrNoSession) {
		t.Errorf("Session returned %v, want ErrNoSession", err)
	}
}

func TestAccessors(t *testing.T) {
	h := newHarness(t, nil)

	if len(h.rt.Skills()) != 5 {
		t.Errorf("Skills() returned %d descriptors, want 5", len(h.rt.Skills()))
	}
	if h.rt.ProviderName() != "static" {
		t.Errorf("ProviderName() = %q", h.rt.ProviderName())
	}
	if len(h.rt.Providers()) != 1 {
		t.Errorf("Providers() = %v", h.rt.Providers())
	}
}

func TestPrune(t *testing.T) {
	h := newHarness(t, nil)
	if _, err := h.rt.Handle(context.Background(), Request{Text: "ping"}); err != nil {
		t.Fatalf("Handle: %v", err)
	}

	future := time.Now().UTC().Add(48 * time.Hour)
	h.sessions.SetClock(func() time.Time { return future })

	if n := h.rt.Prune(); n != 1 {
		t.Errorf("Prune removed %d sessions, want 1", n)
	}
}

func TestCloseJournalsLifecycle(t *testing.T) {
	h := newHarness(t, nil)
	if err := h.rt.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	events := h.journal.Events()
	if len(events) == 0 {
		t.Fatal("Close recorded nothing")
	}
	last := events[len(events)-1]
	if last.Kind != journal.KindLifecycle {
		t.Errorf("last event kind = %q, want %q", last.Kind, journal.KindLifecycle)
	}
}

func TestHandleIsConcurrencySafe(t *testing.T) {
	h := newHarness(t, nil)
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 25; k++ {
				if _, err := h.rt.Handle(context.Background(), Request{Text: "ping"}); err != nil {
					t.Errorf("Handle: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	if got := len(h.journal.Events()); got != 8*25*4 {
		t.Errorf("journal holds %d events, want %d", got, 8*25*4)
	}
}

func TestSpecificSkillsOutrankBroadPatterns(t *testing.T) {
	h := newHarness(t, nil)

	// Math's "what is" pattern also matches "what time is it". The tie must
	// break toward the more specific skill, or the clock becomes unreachable.
	reply, err := h.rt.Handle(context.Background(), Request{Text: "what time is it"})
	if err != nil {
		t.Fatalf("Handle: %v", err)
	}
	if reply.Skill != "clock" {
		t.Errorf("Skill = %q, want clock", reply.Skill)
	}
}
