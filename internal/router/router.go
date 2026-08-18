// Package router decides which skill, if any, should handle an utterance.
//
// Matching is deliberately deterministic and explainable: a pattern is a bag
// of keywords, and a match scores as the fraction of those keywords present in
// the input. No model, no embeddings, no hidden state — the same input always
// produces the same route, which is what makes the journal replayable.
package router

import (
	"sort"
	"strings"
	"sync"
	"unicode"

	"github.com/msaaqib20/orrery/internal/skill"
)

// DefaultMinScore is used when a non-positive threshold is supplied.
const DefaultMinScore = 0.6

// Match is a routing decision.
type Match struct {
	Name       string           `json:"name"`
	Score      float64          `json:"score"`
	Pattern    string           `json:"pattern"`
	Descriptor skill.Descriptor `json:"-"`
}

// Router scores utterances against skill descriptors.
type Router struct {
	mu       sync.RWMutex
	minScore float64
	descs    []skill.Descriptor
}

// New returns a router with the given acceptance threshold.
func New(minScore float64) *Router {
	if minScore <= 0 || minScore > 1 {
		minScore = DefaultMinScore
	}
	return &Router{minScore: minScore}
}

// Load replaces the router's view of the available skills.
func (r *Router) Load(descs []skill.Descriptor) {
	sorted := make([]skill.Descriptor, len(descs))
	copy(sorted, descs)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority > sorted[j].Priority
		}
		return sorted[i].Name < sorted[j].Name
	})

	r.mu.Lock()
	defer r.mu.Unlock()
	r.descs = sorted
}

// MinScore reports the acceptance threshold.
func (r *Router) MinScore() float64 {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.minScore
}

// Route returns the best match at or above the threshold. The boolean is false
// when nothing clears the bar, which is the runtime's signal to fall through to
// the provider.
func (r *Router) Route(text string) (Match, bool) {
	best, ok := r.best(text)
	if !ok {
		return Match{}, false
	}
	if best.Score < r.MinScore() {
		return Match{}, false
	}
	return best, true
}

// Candidates returns every non-zero scoring match, best first. It exists for
// debugging: when a route surprises an operator, this shows the runner-up.
func (r *Router) Candidates(text string) []Match {
	tokens := Normalize(text)
	if len(tokens) == 0 {
		return nil
	}

	r.mu.RLock()
	descs := r.descs
	r.mu.RUnlock()

	var out []Match
	for _, d := range descs {
		if m, ok := scoreDescriptor(d, tokens); ok {
			out = append(out, m)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Score != out[j].Score {
			return out[i].Score > out[j].Score
		}
		if out[i].Descriptor.Priority != out[j].Descriptor.Priority {
			return out[i].Descriptor.Priority > out[j].Descriptor.Priority
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func (r *Router) best(text string) (Match, bool) {
	candidates := r.Candidates(text)
	if len(candidates) == 0 {
		return Match{}, false
	}
	return candidates[0], true
}

// scoreDescriptor returns the best-scoring pattern for a single skill.
func scoreDescriptor(d skill.Descriptor, tokens []string) (Match, bool) {
	present := make(map[string]bool, len(tokens))
	for _, t := range tokens {
		present[t] = true
	}

	best := Match{Name: d.Name, Descriptor: d}
	found := false

	for _, pattern := range d.Patterns {
		words := Normalize(pattern)
		if len(words) == 0 {
			continue
		}
		hits := 0
		for _, w := range words {
			if present[w] {
				hits++
			}
		}
		if hits == 0 {
			continue
		}
		score := float64(hits) / float64(len(words))
		if score > best.Score {
			best.Score = score
			best.Pattern = pattern
			found = true
		}
	}
	return best, found
}

// Normalize lowercases text, strips punctuation and splits into tokens.
// Apostrophes are removed rather than treated as separators so that "today's"
// and "todays" produce the same token.
func Normalize(text string) []string {
	var b strings.Builder
	for _, r := range strings.ToLower(text) {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r):
			b.WriteRune(r)
		case r == '\'' || r == '\u2019':
			// drop
		default:
			b.WriteRune(' ')
		}
	}
	return strings.Fields(b.String())
}
