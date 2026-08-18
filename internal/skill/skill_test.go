package skill

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/session"
)

// stub is a minimal Skill used to exercise the registry.
type stub struct {
	desc Descriptor
	out  Output
	err  error
}

func (s stub) Descriptor() Descriptor { return s.desc }
func (s stub) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	return s.out, s.err
}

func validDescriptor(name string) Descriptor {
	return Descriptor{
		Name:     name,
		Summary:  "a test skill",
		Patterns: []string{name},
	}
}

func TestDescriptorValidate(t *testing.T) {
	if err := validDescriptor("ok").Validate(); err != nil {
		t.Errorf("valid descriptor rejected: %v", err)
	}

	cases := map[string]Descriptor{
		"empty name":    {Summary: "s", Patterns: []string{"p"}},
		"empty summary": {Name: "n", Patterns: []string{"p"}},
		"no patterns":   {Name: "n", Summary: "s"},
		"blank pattern": {Name: "n", Summary: "s", Patterns: []string{"  "}},
	}
	for label, d := range cases {
		if err := d.Validate(); !errors.Is(err, ErrInvalid) {
			t.Errorf("%s: Validate returned %v, want ErrInvalid", label, err)
		}
	}
}

func TestRegistryRejectsInvalidSkill(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stub{desc: Descriptor{Name: "broken"}}); !errors.Is(err, ErrInvalid) {
		t.Errorf("Register returned %v, want ErrInvalid", err)
	}
	if err := r.Register(nil); err == nil {
		t.Error("expected registering nil to fail")
	}
	if r.Len() != 0 {
		t.Errorf("Len = %d after failed registrations", r.Len())
	}
}

func TestRegistryRejectsDuplicates(t *testing.T) {
	r := NewRegistry()
	if err := r.Register(stub{desc: validDescriptor("dup")}); err != nil {
		t.Fatalf("Register: %v", err)
	}
	if err := r.Register(stub{desc: validDescriptor("dup")}); !errors.Is(err, ErrAlreadyExists) {
		t.Errorf("Register returned %v, want ErrAlreadyExists", err)
	}
}

func TestRegistryGet(t *testing.T) {
	r := NewRegistry()
	r.MustRegister(stub{desc: validDescriptor("found")})

	if _, err := r.Get("found"); err != nil {
		t.Errorf("Get: %v", err)
	}
	if _, err := r.Get("absent"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}

func TestMustRegisterPanicsOnInvalid(t *testing.T) {
	defer func() {
		if recover() == nil {
			t.Error("MustRegister did not panic on an invalid skill")
		}
	}()
	NewRegistry().MustRegister(stub{desc: Descriptor{Name: "bad"}})
}

func TestRegistryDescriptorsAreSorted(t *testing.T) {
	r := NewRegistry()
	for _, n := range []string{"zulu", "alpha", "mike"} {
		r.MustRegister(stub{desc: validDescriptor(n)})
	}
	got := strings.Join(r.Names(), ",")
	if got != "alpha,mike,zulu" {
		t.Errorf("Names() = %q, want sorted output", got)
	}
}

func TestRequiredCapabilities(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	caps := r.RequiredCapabilities()
	want := map[permission.Capability]bool{
		permission.CapClockRead:   true,
		permission.CapSessionRead: true,
	}
	if len(caps) != len(want) {
		t.Fatalf("RequiredCapabilities() = %v, want %d entries", caps, len(want))
	}
	for _, c := range caps {
		if !want[c] {
			t.Errorf("unexpected capability %q", c)
		}
	}
}

func TestRegisterBuiltins(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	got := strings.Join(r.Names(), ",")
	want := "clock,help,math,ping,recall"
	if got != want {
		t.Errorf("Names() = %q, want %q", got, want)
	}
	for _, d := range r.Descriptors() {
		if err := d.Validate(); err != nil {
			t.Errorf("built-in %q has an invalid descriptor: %v", d.Name, err)
		}
	}
}

func TestClockFormatsInjectedTime(t *testing.T) {
	at := time.Date(2026, 8, 18, 14, 30, 0, 0, time.UTC)
	out, err := Clock{}.Execute(context.Background(), Input{Now: at})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.Text, "18 August 2026") {
		t.Errorf("Text = %q", out.Text)
	}
	if out.Fields["rfc3339"] != "2026-08-18T14:30:00Z" {
		t.Errorf("Fields[rfc3339] = %v", out.Fields["rfc3339"])
	}
}

func TestClockHonoursLocation(t *testing.T) {
	loc := time.FixedZone("TEST", 2*60*60)
	at := time.Date(2026, 8, 18, 12, 0, 0, 0, time.UTC)

	out, err := Clock{Location: loc}.Execute(context.Background(), Input{Now: at})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.Text, "14:00:00") {
		t.Errorf("Text = %q, want the time shifted into the configured zone", out.Text)
	}
}

func TestHelpListsRegisteredSkills(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	s, err := r.Get("help")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	out, err := s.Execute(context.Background(), Input{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	for _, name := range r.Names() {
		if !strings.Contains(out.Text, name) {
			t.Errorf("help output does not mention %q", name)
		}
	}
	if out.Fields["count"] != 5 {
		t.Errorf("Fields[count] = %v, want 5", out.Fields["count"])
	}
}

func TestHelpWithoutRegistry(t *testing.T) {
	if _, err := (Help{}).Execute(context.Background(), Input{}); !errors.Is(err, ErrUnhandled) {
		t.Errorf("Execute returned %v, want ErrUnhandled", err)
	}
}

func TestRecallSummarisesTurns(t *testing.T) {
	in := Input{
		SessionID: "s1",
		Turns: []session.Turn{
			{Role: session.RoleUser, Text: "hello"},
			{Role: session.RoleAssistant, Text: "hi"},
		},
	}
	out, err := Recall{}.Execute(context.Background(), in)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Fields["turns"] != 2 {
		t.Errorf("Fields[turns] = %v, want 2", out.Fields["turns"])
	}
	if !strings.Contains(out.Text, "hello") || !strings.Contains(out.Text, "hi") {
		t.Errorf("Text = %q", out.Text)
	}
}

func TestRecallTruncatesLongTurns(t *testing.T) {
	long := strings.Repeat("x", 200)
	out, err := Recall{}.Execute(context.Background(), Input{
		Turns: []session.Turn{{Role: session.RoleUser, Text: long}},
	})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !strings.Contains(out.Text, "...") {
		t.Error("a long turn was not truncated")
	}
	if len(out.Text) > 150 {
		t.Errorf("output is %d characters, want it bounded", len(out.Text))
	}
}

func TestRecallOnEmptySession(t *testing.T) {
	out, err := Recall{}.Execute(context.Background(), Input{})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Fields["turns"] != 0 {
		t.Errorf("Fields[turns] = %v, want 0", out.Fields["turns"])
	}
}

func TestPing(t *testing.T) {
	out, err := Ping{}.Execute(context.Background(), Input{SessionID: "s1"})
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if out.Text != "pong" {
		t.Errorf("Text = %q", out.Text)
	}
}

func TestBuiltinsHonourCancellation(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	for _, name := range r.Names() {
		s, err := r.Get(name)
		if err != nil {
			t.Fatalf("Get(%q): %v", name, err)
		}
		if _, err := s.Execute(ctx, Input{Text: "1 + 1"}); !errors.Is(err, context.Canceled) {
			t.Errorf("%s.Execute returned %v, want context.Canceled", name, err)
		}
	}
}

func TestRegistryIsConcurrencySafe(t *testing.T) {
	r := NewRegistry()
	if err := RegisterBuiltins(r); err != nil {
		t.Fatalf("RegisterBuiltins: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				r.Descriptors()
				r.Names()
				r.RequiredCapabilities()
				if _, err := r.Get("ping"); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()
}
