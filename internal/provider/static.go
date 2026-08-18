package provider

import (
	"context"
	"sync"
	"time"
)

// Static is a test double that returns a fixed reply, an optional error, and
// records the requests it received.
type Static struct {
	ProviderName string
	Reply        string
	Err          error
	Delay        time.Duration

	mu       sync.Mutex
	requests []Request
}

// Name implements Provider.
func (s *Static) Name() string {
	if s.ProviderName == "" {
		return "static"
	}
	return s.ProviderName
}

// Complete implements Provider, honouring ctx during the configured delay.
func (s *Static) Complete(ctx context.Context, req Request) (Response, error) {
	s.mu.Lock()
	s.requests = append(s.requests, req)
	s.mu.Unlock()

	if s.Delay > 0 {
		timer := time.NewTimer(s.Delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return Response{}, ctx.Err()
		case <-timer.C:
		}
	}
	if err := ctx.Err(); err != nil {
		return Response{}, err
	}
	if s.Err != nil {
		return Response{}, s.Err
	}
	return Response{Text: s.Reply, Provider: s.Name(), Stop: "end_turn"}, nil
}

// Requests returns a copy of every request the double has seen.
func (s *Static) Requests() []Request {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Request, len(s.requests))
	copy(out, s.requests)
	return out
}

// Calls reports how many times Complete was invoked.
func (s *Static) Calls() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return len(s.requests)
}
