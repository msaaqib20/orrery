// Package runtime wires the pieces together and owns the request lifecycle.
//
// One request follows exactly one path:
//
//	journal(request) -> route -> [permission check -> skill] or [provider]
//	                 -> record turns -> journal(reply)
//
// Routing, permission and execution are separate steps on purpose. A skill is
// selected before it is authorised, and authorised before it is run, so a
// denial is a first-class outcome that appears in the journal rather than an
// error buried inside a handler.
package runtime

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/msaaqib20/orrery/internal/journal"
	"github.com/msaaqib20/orrery/internal/permission"
	"github.com/msaaqib20/orrery/internal/provider"
	"github.com/msaaqib20/orrery/internal/router"
	"github.com/msaaqib20/orrery/internal/session"
	"github.com/msaaqib20/orrery/internal/skill"
)

// Sources a reply can come from.
const (
	SourceSkill    = "skill"
	SourceProvider = "provider"
)

// Errors returned by the runtime.
var (
	ErrEmptyText  = errors.New("runtime: text must not be empty")
	ErrNotReady   = errors.New("runtime: missing dependency")
	ErrNoSession  = errors.New("runtime: session not found")
	defaultSystem = "You are orrery, a local assistant daemon. Answer briefly and plainly."
)

// Options configures a Runtime. Every field except Now and SystemPrompt is
// required; New reports which one is missing rather than panicking later.
type Options struct {
	Logger          *slog.Logger
	Sessions        *session.Store
	Skills          *skill.Registry
	Router          *router.Router
	Providers       *provider.Registry
	Policy          *permission.Policy
	Journal         journal.Journal
	ProviderName    string
	ProviderTimeout time.Duration
	MaxTokens       int
	SystemPrompt    string
	Now             func() time.Time
}

// Runtime is the orchestrator.
type Runtime struct {
	log             *slog.Logger
	sessions        *session.Store
	skills          *skill.Registry
	router          *router.Router
	providers       *provider.Registry
	policy          *permission.Policy
	journal         journal.Journal
	providerName    string
	providerTimeout time.Duration
	maxTokens       int
	systemPrompt    string
	now             func() time.Time
}

// Request is one inbound message.
type Request struct {
	SessionID string `json:"session_id,omitempty"`
	Text      string `json:"text"`
}

// Reply is the runtime's answer, including how it was produced.
type Reply struct {
	SessionID string        `json:"session_id"`
	Text      string        `json:"text"`
	Source    string        `json:"source"`
	Skill     string        `json:"skill,omitempty"`
	Provider  string        `json:"provider,omitempty"`
	Score     float64       `json:"score,omitempty"`
	Elapsed   time.Duration `json:"-"`
	ElapsedMS int64         `json:"elapsed_ms"`
}

// New validates the options and builds a Runtime. The router is loaded from
// the skill registry here, so the two can never drift apart.
func New(opts Options) (*Runtime, error) {
	switch {
	case opts.Sessions == nil:
		return nil, fmt.Errorf("%w: sessions", ErrNotReady)
	case opts.Skills == nil:
		return nil, fmt.Errorf("%w: skills", ErrNotReady)
	case opts.Router == nil:
		return nil, fmt.Errorf("%w: router", ErrNotReady)
	case opts.Providers == nil:
		return nil, fmt.Errorf("%w: providers", ErrNotReady)
	case opts.Policy == nil:
		return nil, fmt.Errorf("%w: policy", ErrNotReady)
	case opts.Journal == nil:
		return nil, fmt.Errorf("%w: journal", ErrNotReady)
	case opts.ProviderName == "":
		return nil, fmt.Errorf("%w: provider name", ErrNotReady)
	}

	if _, err := opts.Providers.Get(opts.ProviderName); err != nil {
		return nil, fmt.Errorf("runtime: configured provider: %w", err)
	}

	rt := &Runtime{
		log:             opts.Logger,
		sessions:        opts.Sessions,
		skills:          opts.Skills,
		router:          opts.Router,
		providers:       opts.Providers,
		policy:          opts.Policy,
		journal:         opts.Journal,
		providerName:    opts.ProviderName,
		providerTimeout: opts.ProviderTimeout,
		maxTokens:       opts.MaxTokens,
		systemPrompt:    opts.SystemPrompt,
		now:             opts.Now,
	}
	if rt.log == nil {
		rt.log = slog.Default()
	}
	if rt.now == nil {
		rt.now = func() time.Time { return time.Now().UTC() }
	}
	if rt.providerTimeout <= 0 {
		rt.providerTimeout = 30 * time.Second
	}
	if rt.systemPrompt == "" {
		rt.systemPrompt = defaultSystem
	}

	rt.router.Load(rt.skills.Descriptors())
	return rt, nil
}

// Handle processes one request end to end.
func (rt *Runtime) Handle(ctx context.Context, req Request) (Reply, error) {
	start := rt.now()

	text := strings.TrimSpace(req.Text)
	if text == "" {
		return Reply{}, ErrEmptyText
	}

	sess := rt.sessions.Ensure(req.SessionID)
	sessionID := sess.ID

	rt.record(journal.Event{
		Kind:      journal.KindRequest,
		SessionID: sessionID,
		Fields:    map[string]any{"text": text},
	})

	if _, err := rt.sessions.Append(sessionID, session.Turn{
		Role: session.RoleUser,
		Text: text,
		At:   start,
	}); err != nil {
		return Reply{}, fmt.Errorf("runtime: record user turn: %w", err)
	}

	reply, err := rt.dispatch(ctx, sessionID, text, start)
	if err != nil {
		rt.record(journal.Event{
			Kind:      journal.KindError,
			SessionID: sessionID,
			Fields:    map[string]any{"error": err.Error()},
		})
		return Reply{}, err
	}

	if _, err := rt.sessions.Append(sessionID, session.Turn{
		Role: session.RoleAssistant,
		Text: reply.Text,
		At:   rt.now(),
	}); err != nil {
		return Reply{}, fmt.Errorf("runtime: record assistant turn: %w", err)
	}

	reply.SessionID = sessionID
	reply.Elapsed = rt.now().Sub(start)
	reply.ElapsedMS = reply.Elapsed.Milliseconds()

	rt.record(journal.Event{
		Kind:      journal.KindReply,
		SessionID: sessionID,
		Fields: map[string]any{
			"source":     reply.Source,
			"skill":      reply.Skill,
			"provider":   reply.Provider,
			"elapsed_ms": reply.ElapsedMS,
		},
	})
	return reply, nil
}

// dispatch chooses between the skill path and the provider path.
func (rt *Runtime) dispatch(ctx context.Context, sessionID, text string, at time.Time) (Reply, error) {
	match, routed := rt.router.Route(text)
	if !routed {
		return rt.callProvider(ctx, sessionID, text)
	}

	rt.record(journal.Event{
		Kind:      journal.KindRouted,
		SessionID: sessionID,
		Fields: map[string]any{
			"skill":   match.Name,
			"score":   match.Score,
			"pattern": match.Pattern,
		},
	})

	reply, handled, err := rt.callSkill(ctx, sessionID, text, match, at)
	if err != nil {
		return Reply{}, err
	}
	if handled {
		return reply, nil
	}
	// A routed skill that declines is not a failure. The provider is the
	// fallback for exactly this case.
	return rt.callProvider(ctx, sessionID, text)
}

// callSkill authorises and runs a matched skill. The bool reports whether the
// skill actually produced an answer.
func (rt *Runtime) callSkill(ctx context.Context, sessionID, text string, match router.Match, at time.Time) (Reply, bool, error) {
	s, err := rt.skills.Get(match.Name)
	if err != nil {
		return Reply{}, false, fmt.Errorf("runtime: %w", err)
	}
	desc := s.Descriptor()

	if err := rt.policy.CheckAll(desc.Name, desc.Capabilities); err != nil {
		rt.record(journal.Event{
			Kind:      journal.KindDenied,
			SessionID: sessionID,
			Fields:    map[string]any{"skill": desc.Name, "error": err.Error()},
		})
		rt.log.Warn("skill denied", slog.String("skill", desc.Name), slog.String("error", err.Error()))
		// A denial must not leak into the provider path: silently answering
		// anyway would defeat the point of having a policy.
		return Reply{}, false, err
	}

	turns := rt.turnsFor(sessionID)
	out, err := s.Execute(ctx, skill.Input{
		Text:      text,
		SessionID: sessionID,
		Turns:     turns,
		Now:       at,
	})
	if err != nil {
		if ctx.Err() != nil {
			return Reply{}, false, err
		}
		rt.log.Debug("skill declined",
			slog.String("skill", desc.Name),
			slog.String("error", err.Error()))
		rt.record(journal.Event{
			Kind:      journal.KindSkill,
			SessionID: sessionID,
			Fields:    map[string]any{"skill": desc.Name, "handled": false, "error": err.Error()},
		})
		return Reply{}, false, nil
	}

	rt.record(journal.Event{
		Kind:      journal.KindSkill,
		SessionID: sessionID,
		Fields:    map[string]any{"skill": desc.Name, "handled": true},
	})

	return Reply{
		Text:   out.Text,
		Source: SourceSkill,
		Skill:  desc.Name,
		Score:  match.Score,
	}, true, nil
}

// callProvider runs the configured completion backend.
func (rt *Runtime) callProvider(ctx context.Context, sessionID, text string) (Reply, error) {
	p, err := rt.providers.Get(rt.providerName)
	if err != nil {
		return Reply{}, fmt.Errorf("runtime: %w", err)
	}

	req := provider.Request{
		System:    rt.systemPrompt,
		Messages:  rt.messagesFor(sessionID, text),
		MaxTokens: rt.maxTokens,
	}

	callCtx, cancel := context.WithTimeout(ctx, rt.providerTimeout)
	defer cancel()

	resp, err := p.Complete(callCtx, req)
	if err != nil {
		rt.record(journal.Event{
			Kind:      journal.KindProvider,
			SessionID: sessionID,
			Fields:    map[string]any{"provider": p.Name(), "error": err.Error()},
		})
		return Reply{}, fmt.Errorf("runtime: provider %s: %w", p.Name(), err)
	}

	rt.record(journal.Event{
		Kind:      journal.KindProvider,
		SessionID: sessionID,
		Fields:    map[string]any{"provider": p.Name(), "stop": resp.Stop},
	})

	return Reply{
		Text:     resp.Text,
		Source:   SourceProvider,
		Provider: resp.Provider,
	}, nil
}

// messagesFor builds the provider request from session history. The current
// user turn has already been appended, so it is not added again here.
func (rt *Runtime) messagesFor(sessionID, fallback string) []provider.Message {
	turns := rt.turnsFor(sessionID)
	if len(turns) == 0 {
		return []provider.Message{{Role: provider.RoleUser, Content: fallback}}
	}

	msgs := make([]provider.Message, 0, len(turns))
	for _, t := range turns {
		role := provider.RoleUser
		if t.Role == session.RoleAssistant {
			role = provider.RoleAssistant
		}
		msgs = append(msgs, provider.Message{Role: role, Content: t.Text})
	}
	return msgs
}

func (rt *Runtime) turnsFor(sessionID string) []session.Turn {
	sess, err := rt.sessions.Get(sessionID)
	if err != nil {
		return nil
	}
	return sess.Turns
}

// record writes to the journal. A journal failure is logged but never fails
// the request: losing an audit line is bad, dropping a user's answer is worse.
func (rt *Runtime) record(e journal.Event) {
	if e.At.IsZero() {
		e.At = rt.now()
	}
	if _, err := rt.journal.Append(e); err != nil {
		rt.log.Error("journal append failed",
			slog.String("kind", e.Kind),
			slog.String("error", err.Error()))
	}
}

// Skills exposes the registered skill descriptors.
func (rt *Runtime) Skills() []skill.Descriptor { return rt.skills.Descriptors() }

// Providers lists the registered provider names.
func (rt *Runtime) Providers() []string { return rt.providers.Names() }

// ProviderName reports the provider the runtime will fall back to.
func (rt *Runtime) ProviderName() string { return rt.providerName }

// Session returns a copy of a stored session.
func (rt *Runtime) Session(id string) (*session.Session, error) {
	sess, err := rt.sessions.Get(id)
	if err != nil {
		return nil, fmt.Errorf("%w: %s", ErrNoSession, id)
	}
	return sess, nil
}

// Prune evicts idle sessions and records how many went.
func (rt *Runtime) Prune() int {
	n := rt.sessions.Prune()
	if n > 0 {
		rt.log.Debug("pruned idle sessions", slog.Int("count", n))
	}
	return n
}

// Close releases the journal.
func (rt *Runtime) Close() error {
	rt.record(journal.Event{
		Kind:   journal.KindLifecycle,
		Fields: map[string]any{"event": "runtime_close"},
	})
	return rt.journal.Close()
}
