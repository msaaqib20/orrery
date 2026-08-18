package skill

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/msaaqib20/orrery/internal/permission"
)

// Clock reports the current date and time.
type Clock struct {
	// Location formats the answer in a specific zone. Defaults to UTC.
	Location *time.Location
}

// Descriptor implements Skill.
func (c Clock) Descriptor() Descriptor {
	return Descriptor{
		Name:         "clock",
		Summary:      "Reports the current date and time.",
		Patterns:     []string{"what time", "current time", "what date", "todays date", "time is it"},
		Capabilities: []permission.Capability{permission.CapClockRead},
		Priority:     20,
		Examples:     []string{"what time is it", "what is today's date"},
	}
}

// Execute implements Skill.
func (c Clock) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	loc := c.Location
	if loc == nil {
		loc = time.UTC
	}
	now := in.Now
	if now.IsZero() {
		now = time.Now()
	}
	now = now.In(loc)

	return Output{
		Text: now.Format("Monday, 2 January 2006 at 15:04:05 MST"),
		Fields: map[string]any{
			"rfc3339": now.Format(time.RFC3339),
			"zone":    loc.String(),
		},
	}, nil
}

// Help lists the skills the daemon can handle without a model.
type Help struct {
	Registry *Registry
}

// Descriptor implements Skill.
func (h Help) Descriptor() Descriptor {
	return Descriptor{
		Name:         "help",
		Summary:      "Lists the skills this daemon can answer directly.",
		Patterns:     []string{"help", "what can you do", "list skills", "commands"},
		Capabilities: nil,
		Priority:     5,
		Examples:     []string{"help", "what can you do"},
	}
}

// Execute implements Skill.
func (h Help) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	if h.Registry == nil {
		return Output{}, fmt.Errorf("%w: help has no registry", ErrUnhandled)
	}

	descs := h.Registry.Descriptors()
	var b strings.Builder
	b.WriteString(fmt.Sprintf("%d skills are available:\n", len(descs)))
	for _, d := range descs {
		b.WriteString(fmt.Sprintf("  %-8s %s\n", d.Name, d.Summary))
	}
	b.WriteString("Anything else is passed to the configured provider.")

	return Output{
		Text:   b.String(),
		Fields: map[string]any{"count": len(descs)},
	}, nil
}

// Recall reports what the daemon is holding for the current session. It is the
// smallest possible skill that needs a capability, and doubles as a worked
// example of the permission path.
type Recall struct{}

// Descriptor implements Skill.
func (Recall) Descriptor() Descriptor {
	return Descriptor{
		Name:         "recall",
		Summary:      "Summarises the conversation history held for this session.",
		Patterns:     []string{"what did i say", "conversation history", "session history", "recall"},
		Capabilities: []permission.Capability{permission.CapSessionRead},
		Priority:     20,
		Examples:     []string{"what did i say", "show session history"},
	}
}

// Execute implements Skill.
func (Recall) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}

	if len(in.Turns) == 0 {
		return Output{
			Text:   "This session has no history yet.",
			Fields: map[string]any{"turns": 0},
		}, nil
	}

	var b strings.Builder
	b.WriteString(fmt.Sprintf("This session holds %d turns:\n", len(in.Turns)))
	for _, t := range in.Turns {
		text := t.Text
		if len(text) > 80 {
			text = text[:77] + "..."
		}
		b.WriteString(fmt.Sprintf("  [%s] %s\n", t.Role, text))
	}

	return Output{
		Text:   strings.TrimRight(b.String(), "\n"),
		Fields: map[string]any{"turns": len(in.Turns), "session_id": in.SessionID},
	}, nil
}

// Ping is a liveness probe reachable through the normal message path, so an
// operator can confirm routing works without depending on a provider.
type Ping struct{}

// Descriptor implements Skill.
func (Ping) Descriptor() Descriptor {
	return Descriptor{
		Name:         "ping",
		Summary:      "Confirms the runtime is answering requests.",
		Patterns:     []string{"ping", "are you there", "status"},
		Capabilities: nil,
		Priority:     1,
		Examples:     []string{"ping"},
	}
}

// Execute implements Skill.
func (Ping) Execute(ctx context.Context, in Input) (Output, error) {
	if err := ctx.Err(); err != nil {
		return Output{}, err
	}
	return Output{Text: "pong", Fields: map[string]any{"session_id": in.SessionID}}, nil
}

// RegisterBuiltins wires every built-in skill into r. Help is given a
// reference to the registry it is being added to, so it always describes the
// live set rather than a snapshot.
func RegisterBuiltins(r *Registry) error {
	skills := []Skill{
		Clock{},
		Help{Registry: r},
		Math{},
		Ping{},
		Recall{},
	}
	for _, s := range skills {
		if err := r.Register(s); err != nil {
			return err
		}
	}
	return nil
}
