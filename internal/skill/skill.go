// Package skill defines deterministic handlers that answer without calling a
// language model, plus the registry that holds them.
//
// A skill declares the capabilities it needs up front. The runtime checks
// those against the policy before Execute is ever called, so a skill cannot
// acquire privilege by asking for it at the wrong moment.
package skill

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/session"
)

// Errors returned by this package.
var (
	ErrAlreadyExists = errors.New("skill: already registered")
	ErrNotFound      = errors.New("skill: not found")
	ErrInvalid       = errors.New("skill: invalid descriptor")
	ErrUnhandled     = errors.New("skill: could not handle the request")
)

// Descriptor is a skill's static self-description. It is the only thing the
// router sees, which keeps routing independent of execution.
type Descriptor struct {
	Name         string                  `json:"name"`
	Summary      string                  `json:"summary"`
	Patterns     []string                `json:"patterns"`
	Capabilities []permission.Capability `json:"capabilities"`
	Priority     int                     `json:"priority"`
	Examples     []string                `json:"examples,omitempty"`
}

// Validate checks the descriptor is usable.
func (d Descriptor) Validate() error {
	if strings.TrimSpace(d.Name) == "" {
		return fmt.Errorf("%w: name must not be empty", ErrInvalid)
	}
	if strings.TrimSpace(d.Summary) == "" {
		return fmt.Errorf("%w: %s has no summary", ErrInvalid, d.Name)
	}
	if len(d.Patterns) == 0 {
		return fmt.Errorf("%w: %s declares no patterns", ErrInvalid, d.Name)
	}
	for _, p := range d.Patterns {
		if strings.TrimSpace(p) == "" {
			return fmt.Errorf("%w: %s has an empty pattern", ErrInvalid, d.Name)
		}
	}
	return nil
}

// Input is everything a skill is given. It is a value, not a pointer, and the
// turns are already a copy: a skill cannot reach back into shared state.
type Input struct {
	Text      string
	SessionID string
	Turns     []session.Turn
	Now       time.Time
}

// Output is a skill's answer.
type Output struct {
	Text   string
	Fields map[string]any
}

// Skill is a deterministic request handler.
//
// Execute must honour ctx and must be safe for concurrent use.
type Skill interface {
	Descriptor() Descriptor
	Execute(ctx context.Context, in Input) (Output, error)
}

// Registry holds the skills available at runtime.
type Registry struct {
	mu     sync.RWMutex
	skills map[string]Skill
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{skills: make(map[string]Skill)}
}

// Register validates a skill's descriptor and adds it.
func (r *Registry) Register(s Skill) error {
	if s == nil {
		return errors.New("skill: cannot register nil")
	}
	d := s.Descriptor()
	if err := d.Validate(); err != nil {
		return err
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.skills[d.Name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, d.Name)
	}
	r.skills[d.Name] = s
	return nil
}

// MustRegister panics if registration fails. It is for wiring built-ins at
// start-up, where a failure is a programming error rather than a runtime one.
func (r *Registry) MustRegister(s Skill) {
	if err := r.Register(s); err != nil {
		panic(err)
	}
}

// Get looks a skill up by name.
func (r *Registry) Get(name string) (Skill, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	s, ok := r.skills[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return s, nil
}

// Descriptors returns every descriptor, sorted by name for stable output.
func (r *Registry) Descriptors() []Descriptor {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]Descriptor, 0, len(r.skills))
	for _, s := range r.skills {
		out = append(out, s.Descriptor())
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Names lists registered skill names, sorted.
func (r *Registry) Names() []string {
	descs := r.Descriptors()
	out := make([]string, len(descs))
	for i, d := range descs {
		out[i] = d.Name
	}
	return out
}

// Len reports how many skills are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.skills)
}

// RequiredCapabilities returns the union of every capability any registered
// skill declares. Operators use it to see what a deployment could ask for.
func (r *Registry) RequiredCapabilities() []permission.Capability {
	seen := make(map[permission.Capability]bool)
	for _, d := range r.Descriptors() {
		for _, c := range d.Capabilities {
			seen[c] = true
		}
	}
	out := make([]permission.Capability, 0, len(seen))
	for c := range seen {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
