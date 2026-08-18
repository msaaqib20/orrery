// Package permission implements orrery's capability policy.
//
// Skills declare the capabilities they need; the policy decides whether a
// given skill may exercise a given capability. The default posture is deny:
// a capability that nobody granted is refused, and the refusal is journalled.
package permission

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
)

// Capability names a privileged operation.
type Capability string

// Capabilities recognised by the built-in skills. The set is deliberately
// small; new capabilities should be added here rather than invented ad hoc so
// that an operator can enumerate everything that is grantable.
const (
	CapClockRead    Capability = "clock.read"
	CapSessionRead  Capability = "session.read"
	CapProviderCall Capability = "provider.call"
	CapFSRead       Capability = "fs.read"
	CapFSWrite      Capability = "fs.write"
	CapNetHTTP      Capability = "net.http"
	CapShellExec    Capability = "shell.exec"
)

// Wildcard matches any subject or any capability.
const Wildcard = "*"

// ErrDenied is returned when a capability check fails.
var ErrDenied = errors.New("permission denied")

// Known returns every capability the policy layer ships with, sorted.
func Known() []Capability {
	caps := []Capability{
		CapClockRead, CapFSRead, CapFSWrite, CapNetHTTP,
		CapProviderCall, CapSessionRead, CapShellExec,
	}
	sort.Slice(caps, func(i, j int) bool { return caps[i] < caps[j] })
	return caps
}

// Decision is the outcome of a single check.
type Decision struct {
	Allowed bool
	Subject string
	Cap     Capability
	Reason  string
}

// Policy maps subjects (skill names, or "*") to the capabilities they hold.
type Policy struct {
	mu           sync.RWMutex
	defaultAllow bool
	grants       map[string]map[Capability]bool
}

// NewPolicy returns an empty policy. When defaultAllow is false — the
// recommended posture — anything not explicitly granted is denied.
func NewPolicy(defaultAllow bool) *Policy {
	return &Policy{
		defaultAllow: defaultAllow,
		grants:       make(map[string]map[Capability]bool),
	}
}

// Grant gives subject the listed capabilities.
func (p *Policy) Grant(subject string, caps ...Capability) {
	p.mu.Lock()
	defer p.mu.Unlock()

	set, ok := p.grants[subject]
	if !ok {
		set = make(map[Capability]bool)
		p.grants[subject] = set
	}
	for _, c := range caps {
		set[c] = true
	}
}

// Revoke removes capabilities from a subject.
func (p *Policy) Revoke(subject string, caps ...Capability) {
	p.mu.Lock()
	defer p.mu.Unlock()

	set, ok := p.grants[subject]
	if !ok {
		return
	}
	for _, c := range caps {
		delete(set, c)
	}
}

// FromGrants builds a policy from a config-shaped map of subject to capability
// strings. Unknown capability names are rejected so a typo in a config file
// cannot silently widen or narrow the policy.
func FromGrants(defaultAllow bool, grants map[string][]string) (*Policy, error) {
	known := make(map[Capability]bool)
	for _, c := range Known() {
		known[c] = true
	}

	p := NewPolicy(defaultAllow)
	var errs []error
	for subject, caps := range grants {
		for _, raw := range caps {
			name := Capability(strings.TrimSpace(raw))
			if name == "" {
				continue
			}
			if name != Wildcard && !known[name] && !strings.HasSuffix(string(name), ".*") {
				errs = append(errs, fmt.Errorf("unknown capability %q granted to %q", name, subject))
				continue
			}
			p.Grant(subject, name)
		}
		// Record the subject even if it was granted nothing, so that
		// Subjects() reflects the operator's stated intent.
		p.Grant(subject)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return p, nil
}

// Check evaluates a single capability request.
func (p *Policy) Check(subject string, cap Capability) Decision {
	p.mu.RLock()
	defer p.mu.RUnlock()

	d := Decision{Subject: subject, Cap: cap}

	if p.holds(subject, cap) {
		d.Allowed = true
		d.Reason = "granted"
		return d
	}
	if p.holds(Wildcard, cap) {
		d.Allowed = true
		d.Reason = "granted to *"
		return d
	}
	if p.defaultAllow {
		d.Allowed = true
		d.Reason = "default allow"
		return d
	}
	d.Reason = "no matching grant"
	return d
}

// CheckAll requires every capability in caps. It returns the first denial
// wrapped in ErrDenied so callers can use errors.Is.
func (p *Policy) CheckAll(subject string, caps []Capability) error {
	for _, c := range caps {
		if d := p.Check(subject, c); !d.Allowed {
			return fmt.Errorf("%w: %s may not use %s (%s)", ErrDenied, subject, c, d.Reason)
		}
	}
	return nil
}

// Subjects lists every subject with an entry in the policy, sorted.
func (p *Policy) Subjects() []string {
	p.mu.RLock()
	defer p.mu.RUnlock()

	out := make([]string, 0, len(p.grants))
	for s := range p.grants {
		out = append(out, s)
	}
	sort.Strings(out)
	return out
}

// GrantsFor lists the capabilities held by a subject, sorted.
func (p *Policy) GrantsFor(subject string) []Capability {
	p.mu.RLock()
	defer p.mu.RUnlock()

	set := p.grants[subject]
	out := make([]Capability, 0, len(set))
	for c := range set {
		out = append(out, c)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// holds reports whether subject has been granted cap, honouring both the
// "*" capability and prefix wildcards such as "fs.*".
// The caller must hold at least a read lock.
func (p *Policy) holds(subject string, cap Capability) bool {
	set, ok := p.grants[subject]
	if !ok {
		return false
	}
	if set[cap] || set[Wildcard] {
		return true
	}
	for granted := range set {
		if matchesPrefix(string(granted), string(cap)) {
			return true
		}
	}
	return false
}

// matchesPrefix reports whether a grant such as "fs.*" covers "fs.read".
func matchesPrefix(granted, cap string) bool {
	if !strings.HasSuffix(granted, ".*") {
		return false
	}
	prefix := strings.TrimSuffix(granted, "*")
	return strings.HasPrefix(cap, prefix)
}
