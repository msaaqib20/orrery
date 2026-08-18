package provider

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// Echo is a deterministic, dependency-free provider.
//
// It exists so that a fresh clone of orrery runs end to end with no API key,
// no network and no model download. It is also the reference implementation:
// a new backend should behave like Echo with respect to context cancellation
// and response metadata.
type Echo struct {
	// Prefix is prepended to every reply. Defaults to "echo:".
	Prefix string
	// Now supplies the clock, for tests.
	Now func() time.Time
}

// Name implements Provider.
func (e Echo) Name() string { return "echo" }

// Complete implements Provider. It summarises the conversation instead of
// generating text, which keeps its output stable enough to assert on.
func (e Echo) Complete(ctx context.Context, req Request) (Response, error) {
	start := e.now()

	if err := req.Validate(); err != nil {
		return Response{}, err
	}
	// A backend that ignores cancellation will stall the whole daemon under
	// load, so check before doing any work and treat it as a hard failure.
	if err := ctx.Err(); err != nil {
		return Response{}, fmt.Errorf("provider echo: %w", err)
	}

	prefix := e.Prefix
	if prefix == "" {
		prefix = "echo:"
	}

	var b strings.Builder
	b.WriteString(prefix)
	b.WriteString(" ")

	if last, ok := req.LastUser(); ok {
		b.WriteString(strings.TrimSpace(last.Content))
	} else {
		b.WriteString("(no user message)")
	}
	if n := len(req.Messages); n > 1 {
		b.WriteString(fmt.Sprintf(" [context: %d messages]", n))
	}

	return Response{
		Text:     b.String(),
		Provider: e.Name(),
		Elapsed:  e.now().Sub(start),
		Stop:     "end_turn",
	}, nil
}

func (e Echo) now() time.Time {
	if e.Now != nil {
		return e.Now()
	}
	return time.Now()
}
