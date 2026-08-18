// Package provider defines the completion backend interface.
//
// orrery treats language models as replaceable parts. Nothing above this
// package knows which backend is in use; adding a new one means implementing
// Provider and registering it, with no changes to the runtime.
package provider

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"sync"
	"time"
)

// Errors returned by this package.
var (
	ErrNotFound      = errors.New("provider: not found")
	ErrAlreadyExists = errors.New("provider: already registered")
	ErrEmptyRequest  = errors.New("provider: request has no messages")
)

// Role mirrors the conversational roles understood by chat-style backends.
type Role string

// Recognised roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message is one entry in a completion request.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
}

// Request is a backend-neutral completion request.
type Request struct {
	System    string    `json:"system,omitempty"`
	Messages  []Message `json:"messages"`
	MaxTokens int       `json:"max_tokens,omitempty"`
}

// Validate reports whether the request is well formed.
func (r Request) Validate() error {
	if len(r.Messages) == 0 {
		return ErrEmptyRequest
	}
	for i, m := range r.Messages {
		if m.Content == "" {
			return fmt.Errorf("provider: message %d has empty content", i)
		}
	}
	return nil
}

// LastUser returns the most recent user message, if any.
func (r Request) LastUser() (Message, bool) {
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == RoleUser {
			return r.Messages[i], true
		}
	}
	return Message{}, false
}

// Response is a backend-neutral completion result.
type Response struct {
	Text     string        `json:"text"`
	Provider string        `json:"provider"`
	Elapsed  time.Duration `json:"elapsed"`
	Stop     string        `json:"stop,omitempty"`
}

// Provider is a completion backend.
//
// Implementations must honour ctx cancellation and must be safe for concurrent
// use by multiple goroutines.
type Provider interface {
	Name() string
	Complete(ctx context.Context, req Request) (Response, error)
}

// Registry holds the providers available at runtime.
type Registry struct {
	mu sync.RWMutex
	m  map[string]Provider
}

// NewRegistry returns an empty registry.
func NewRegistry() *Registry {
	return &Registry{m: make(map[string]Provider)}
}

// Register adds p under its own name.
func (r *Registry) Register(p Provider) error {
	if p == nil {
		return errors.New("provider: cannot register nil")
	}
	name := p.Name()
	if name == "" {
		return errors.New("provider: name must not be empty")
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.m[name]; exists {
		return fmt.Errorf("%w: %s", ErrAlreadyExists, name)
	}
	r.m[name] = p
	return nil
}

// Get looks a provider up by name.
func (r *Registry) Get(name string) (Provider, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	p, ok := r.m[name]
	if !ok {
		return nil, fmt.Errorf("%w: %s", ErrNotFound, name)
	}
	return p, nil
}

// Names lists registered providers, sorted.
func (r *Registry) Names() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	out := make([]string, 0, len(r.m))
	for n := range r.m {
		out = append(out, n)
	}
	sort.Strings(out)
	return out
}

// Len reports how many providers are registered.
func (r *Registry) Len() int {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return len(r.m)
}
