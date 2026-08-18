package provider

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestRequestValidate(t *testing.T) {
	if err := (Request{}).Validate(); !errors.Is(err, ErrEmptyRequest) {
		t.Errorf("empty request returned %v, want ErrEmptyRequest", err)
	}
	bad := Request{Messages: []Message{{Role: RoleUser, Content: ""}}}
	if err := bad.Validate(); err == nil {
		t.Error("expected a message with empty content to be rejected")
	}
	good := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}
	if err := good.Validate(); err != nil {
		t.Errorf("valid request returned %v", err)
	}
}

func TestRequestLastUser(t *testing.T) {
	req := Request{Messages: []Message{
		{Role: RoleUser, Content: "first"},
		{Role: RoleAssistant, Content: "middle"},
		{Role: RoleUser, Content: "last"},
	}}
	got, ok := req.LastUser()
	if !ok {
		t.Fatal("LastUser reported no user message")
	}
	if got.Content != "last" {
		t.Errorf("LastUser returned %q, want \"last\"", got.Content)
	}

	none := Request{Messages: []Message{{Role: RoleAssistant, Content: "only"}}}
	if _, ok := none.LastUser(); ok {
		t.Error("LastUser found a user message where there is none")
	}
}

func TestEchoCompletes(t *testing.T) {
	e := Echo{}
	resp, err := e.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "  hello world  "}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "echo: hello world" {
		t.Errorf("Text = %q", resp.Text)
	}
	if resp.Provider != "echo" {
		t.Errorf("Provider = %q", resp.Provider)
	}
	if resp.Stop != "end_turn" {
		t.Errorf("Stop = %q", resp.Stop)
	}
}

func TestEchoReportsContextSize(t *testing.T) {
	resp, err := Echo{}.Complete(context.Background(), Request{Messages: []Message{
		{Role: RoleUser, Content: "one"},
		{Role: RoleAssistant, Content: "two"},
		{Role: RoleUser, Content: "three"},
	}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Text, "context: 3 messages") {
		t.Errorf("Text = %q, want it to report the context size", resp.Text)
	}
}

func TestEchoHonoursCustomPrefix(t *testing.T) {
	resp, err := Echo{Prefix: ">>"}.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleUser, Content: "hi"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != ">> hi" {
		t.Errorf("Text = %q", resp.Text)
	}
}

func TestEchoHandlesNoUserMessage(t *testing.T) {
	resp, err := Echo{}.Complete(context.Background(), Request{
		Messages: []Message{{Role: RoleAssistant, Content: "solo"}},
	})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if !strings.Contains(resp.Text, "no user message") {
		t.Errorf("Text = %q", resp.Text)
	}
}

func TestEchoRejectsCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := Echo{}.Complete(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, context.Canceled) {
		t.Errorf("Complete returned %v, want context.Canceled", err)
	}
}

func TestEchoRejectsInvalidRequest(t *testing.T) {
	if _, err := (Echo{}).Complete(context.Background(), Request{}); !errors.Is(err, ErrEmptyRequest) {
		t.Errorf("Complete returned %v, want ErrEmptyRequest", err)
	}
}

func TestEchoElapsedUsesInjectedClock(t *testing.T) {
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	calls := 0
	e := Echo{Now: func() time.Time {
		calls++
		return base.Add(time.Duration(calls) * time.Millisecond)
	}}

	resp, err := e.Complete(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Elapsed <= 0 {
		t.Errorf("Elapsed = %v, want a positive duration", resp.Elapsed)
	}
}

func TestRegistryRegisterAndGet(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if r.Len() != 1 {
		t.Errorf("Len = %d, want 1", r.Len())
	}

	got, err := r.Get("echo")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got.Name() != "echo" {
		t.Errorf("Get returned %q", got.Name())
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(Echo{}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("duplicate Register returned %v, want ErrAlreadyExists", err)
	}
}

func TestRegistryRejectsNilAndUnnamed(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(nil); err == nil {
		t.Error("expected registering nil to fail")
	}
	if err := r.Register(&Static{ProviderName: ""}); err != nil {
		t.Errorf("Static falls back to a default name, got %v", err)
	}
}

func TestRegistryGetUnknown(t *testing.T) {
	r := NewRegistry()
	if _, err := r.Get("absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}

func TestRegistryNamesAreSorted(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(&Static{ProviderName: "zeta"}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(&Static{ProviderName: "alpha"}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	got := strings.Join(r.Names(), ",")
	if got != "alpha,echo,zeta" {
		t.Errorf("Names() = %q, want sorted output", got)
	}
}

func TestRegistryIsConcurrencySafe(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(Echo{}); err != nil {
		t.Fatalf("Register: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				if _, err := r.Get("echo"); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				r.Names()
				r.Len()
			}
		}()
	}
	wg.Wait()
}

func TestStaticRecordsRequests(t *testing.T) {
	s := &Static{Reply: "fixed"}
	req := Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}}

	resp, err := s.Complete(context.Background(), req)
	if err != nil {
		t.Fatalf("Complete: %v", err)
	}
	if resp.Text != "fixed" {
		t.Errorf("Text = %q", resp.Text)
	}
	if s.Calls() != 1 {
		t.Errorf("Calls = %d, want 1", s.Calls())
	}
	if len(s.Requests()) != 1 {
		t.Errorf("Requests returned %d entries", len(s.Requests()))
	}
}

func TestStaticReturnsConfiguredError(t *testing.T) {
	sentinel := errors.New("backend exploded")
	s := &Static{Err: sentinel}

	if _, err := s.Complete(context.Background(), Request{}); !errors.Is(err, sentinel) {
		t.Errorf("Complete returned %v, want the configured error", err)
	}
}

func TestStaticRespectsDeadline(t *testing.T) {
	s := &Static{Reply: "slow", Delay: 2 * time.Second}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()

	start := time.Now()
	_, err := s.Complete(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "hi"}}})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("Complete returned %v, want DeadlineExceeded", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Errorf("Complete waited %v instead of aborting at the deadline", elapsed)
	}
}
