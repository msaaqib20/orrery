package router

import (
	"strings"
	"sync"
	"testing"

	"github.com/msaaqib20/orrery/internal/skill"
)

func testDescriptors() []skill.Descriptor {
	return []skill.Descriptor{
		{Name: "clock", Summary: "time", Patterns: []string{"what time", "current time"}, Priority: 20},
		{Name: "math", Summary: "arithmetic", Patterns: []string{"calculate", "what is"}, Priority: 10},
		{Name: "ping", Summary: "liveness", Patterns: []string{"ping"}, Priority: 1},
	}
}

func newLoadedRouter(minScore float64) *Router {
	r := New(minScore)
	r.Load(testDescriptors())
	return r
}

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"What TIME is it?":   "what time is it",
		"today's date":       "todays date",
		"  spaced   out  ":   "spaced out",
		"2 + 2":              "2 2",
		"caf\u00e9, please!": "caf\u00e9 please",
		"":                   "",
	}
	for in, want := range cases {
		got := strings.Join(Normalize(in), " ")
		if got != want {
			t.Errorf("Normalize(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestRouteExactPattern(t *testing.T) {
	r := newLoadedRouter(0.6)

	m, ok := r.Route("what time is it")
	if !ok {
		t.Fatal("expected a match")
	}
	if m.Name != "clock" {
		t.Errorf("routed to %q, want clock", m.Name)
	}
	if m.Score != 1 {
		t.Errorf("Score = %v, want 1 for a fully matched pattern", m.Score)
	}
	if m.Pattern != "what time" {
		t.Errorf("Pattern = %q", m.Pattern)
	}
}

func TestRouteBelowThresholdFallsThrough(t *testing.T) {
	r := newLoadedRouter(0.9)

	if _, ok := r.Route("time"); ok {
		t.Error("a half-matched pattern cleared a 0.9 threshold")
	}
}

func TestRouteUnknownText(t *testing.T) {
	r := newLoadedRouter(0.6)

	if m, ok := r.Route("write me a sonnet about herons"); ok {
		t.Errorf("unrelated text routed to %q", m.Name)
	}
}

func TestRouteEmptyText(t *testing.T) {
	r := newLoadedRouter(0.6)

	if _, ok := r.Route("   "); ok {
		t.Error("blank text produced a match")
	}
}

func TestRouteWithNoSkillsLoaded(t *testing.T) {
	r := New(0.6)
	if _, ok := r.Route("ping"); ok {
		t.Error("an empty router produced a match")
	}
}

func TestCandidatesAreOrdered(t *testing.T) {
	r := newLoadedRouter(0.5)

	candidates := r.Candidates("what time is it")
	if len(candidates) < 2 {
		t.Fatalf("got %d candidates, want at least 2", len(candidates))
	}
	for i := 1; i < len(candidates); i++ {
		if candidates[i-1].Score < candidates[i].Score {
			t.Fatalf("candidates are not ordered by score: %v", candidates)
		}
	}
	if candidates[0].Name != "clock" {
		t.Errorf("best candidate is %q, want clock", candidates[0].Name)
	}
}

func TestPriorityBreaksScoreTies(t *testing.T) {
	r := New(0.5)
	r.Load([]skill.Descriptor{
		{Name: "low", Summary: "s", Patterns: []string{"shared word"}, Priority: 1},
		{Name: "high", Summary: "s", Patterns: []string{"shared word"}, Priority: 99},
	})

	m, ok := r.Route("shared word")
	if !ok {
		t.Fatal("expected a match")
	}
	if m.Name != "high" {
		t.Errorf("tie broken toward %q, want the higher priority skill", m.Name)
	}
}

func TestRoutingIsDeterministic(t *testing.T) {
	r := New(0.5)
	r.Load([]skill.Descriptor{
		{Name: "bravo", Summary: "s", Patterns: []string{"same"}, Priority: 5},
		{Name: "alpha", Summary: "s", Patterns: []string{"same"}, Priority: 5},
	})

	for i := 0; i < 50; i++ {
		m, ok := r.Route("same")
		if !ok {
			t.Fatal("expected a match")
		}
		if m.Name != "alpha" {
			t.Fatalf("iteration %d routed to %q; ties must resolve by name", i, m.Name)
		}
	}
}

func TestPartialScore(t *testing.T) {
	r := New(0.4)
	r.Load([]skill.Descriptor{
		{Name: "s", Summary: "s", Patterns: []string{"one two three four"}},
	})

	m, ok := r.Route("one two only")
	if !ok {
		t.Fatal("expected a partial match to clear a 0.4 threshold")
	}
	if m.Score != 0.5 {
		t.Errorf("Score = %v, want 0.5 for two of four keywords", m.Score)
	}
}

func TestNewClampsInvalidThreshold(t *testing.T) {
	for _, in := range []float64{0, -1, 2} {
		if got := New(in).MinScore(); got != DefaultMinScore {
			t.Errorf("New(%v).MinScore() = %v, want the default", in, got)
		}
	}
	if got := New(0.25).MinScore(); got != 0.25 {
		t.Errorf("New(0.25).MinScore() = %v", got)
	}
}

func TestLoadCopiesInput(t *testing.T) {
	r := New(0.6)
	descs := testDescriptors()
	r.Load(descs)

	descs[0].Name = "tampered"

	m, ok := r.Route("what time is it")
	if !ok {
		t.Fatal("expected a match")
	}
	if m.Name != "clock" {
		t.Error("Load kept a reference to the caller's slice")
	}
}

func TestLoadIsConcurrencySafe(t *testing.T) {
	r := newLoadedRouter(0.6)
	var wg sync.WaitGroup

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				r.Load(testDescriptors())
			}
		}()
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				r.Route("what time is it")
				r.Candidates("ping")
			}
		}()
	}
	wg.Wait()
}

func TestSkipsEmptyPatterns(t *testing.T) {
	r := New(0.5)
	r.Load([]skill.Descriptor{
		{Name: "s", Summary: "s", Patterns: []string{"   ", "ping"}},
	})

	m, ok := r.Route("ping")
	if !ok {
		t.Fatal("expected a match on the non-empty pattern")
	}
	if m.Pattern != "ping" {
		t.Errorf("Pattern = %q", m.Pattern)
	}
}
