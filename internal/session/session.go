// Package session stores bounded conversation history.
//
// A session is a rolling window of turns, not an archive: the store keeps at
// most MaxTurns entries per session and evicts sessions that have been idle
// longer than the TTL. Anything that needs to survive that window belongs in
// the journal instead.
package session

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"sync"
	"time"
)

// Role identifies the author of a turn.
type Role string

// Recognised roles.
const (
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleSystem    Role = "system"
)

// Valid reports whether r is a known role.
func (r Role) Valid() bool {
	return r == RoleUser || r == RoleAssistant || r == RoleSystem
}

// Errors returned by the store.
var (
	ErrNotFound  = errors.New("session: not found")
	ErrEmptyText = errors.New("session: turn text must not be empty")
	ErrBadRole   = errors.New("session: unknown role")
)

// Turn is one utterance in a conversation.
type Turn struct {
	Role Role      `json:"role"`
	Text string    `json:"text"`
	At   time.Time `json:"at"`
}

// Session is a bounded conversation.
type Session struct {
	ID        string    `json:"id"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
	Turns     []Turn    `json:"turns"`
}

// Clone returns a deep copy, so callers can never mutate stored state.
func (s *Session) Clone() *Session {
	if s == nil {
		return nil
	}
	out := *s
	out.Turns = make([]Turn, len(s.Turns))
	copy(out.Turns, s.Turns)
	return &out
}

// Store holds live sessions in memory.
type Store struct {
	mu       sync.RWMutex
	sessions map[string]*Session
	maxTurns int
	ttl      time.Duration
	now      func() time.Time
}

// NewStore returns a store bounded by maxTurns per session and an idle ttl.
func NewStore(maxTurns int, ttl time.Duration) *Store {
	if maxTurns <= 0 {
		maxTurns = 1
	}
	return &Store{
		sessions: make(map[string]*Session),
		maxTurns: maxTurns,
		ttl:      ttl,
		now:      func() time.Time { return time.Now().UTC() },
	}
}

// SetClock replaces the store's time source. Tests use this to exercise TTL
// behaviour without sleeping.
func (s *Store) SetClock(now func() time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if now != nil {
		s.now = now
	}
}

// NewID returns a random session identifier.
func NewID() string {
	var b [12]byte
	if _, err := rand.Read(b[:]); err != nil {
		// crypto/rand failing is not a recoverable condition for a process
		// that needs unforgeable identifiers.
		panic(fmt.Sprintf("session: read random bytes: %v", err))
	}
	return hex.EncodeToString(b[:])
}

// Ensure returns the session with the given id, creating it if necessary. An
// empty id allocates a fresh session.
func (s *Store) Ensure(id string) *Session {
	s.mu.Lock()
	defer s.mu.Unlock()

	if id == "" {
		id = NewID()
	}
	sess, ok := s.sessions[id]
	if !ok {
		now := s.now()
		sess = &Session{ID: id, CreatedAt: now, UpdatedAt: now}
		s.sessions[id] = sess
	}
	return sess.Clone()
}

// Get returns a copy of a session that already exists.
func (s *Store) Get(id string) (*Session, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	sess, ok := s.sessions[id]
	if !ok {
		return nil, ErrNotFound
	}
	return sess.Clone(), nil
}

// Append adds a turn to a session, creating the session if needed and trimming
// the oldest turns once the window is full.
func (s *Store) Append(id string, turn Turn) (*Session, error) {
	if !turn.Role.Valid() {
		return nil, fmt.Errorf("%w: %q", ErrBadRole, turn.Role)
	}
	if turn.Text == "" {
		return nil, ErrEmptyText
	}

	s.mu.Lock()
	defer s.mu.Unlock()

	now := s.now()
	if turn.At.IsZero() {
		turn.At = now
	}
	if id == "" {
		id = NewID()
	}

	sess, ok := s.sessions[id]
	if !ok {
		sess = &Session{ID: id, CreatedAt: now, UpdatedAt: now}
		s.sessions[id] = sess
	}

	sess.Turns = append(sess.Turns, turn)
	if over := len(sess.Turns) - s.maxTurns; over > 0 {
		sess.Turns = append([]Turn(nil), sess.Turns[over:]...)
	}
	sess.UpdatedAt = now

	return sess.Clone(), nil
}

// Prune drops sessions idle for longer than the TTL and returns how many were
// removed. A non-positive TTL disables eviction.
func (s *Store) Prune() int {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.ttl <= 0 {
		return 0
	}
	cutoff := s.now().Add(-s.ttl)
	removed := 0
	for id, sess := range s.sessions {
		if sess.UpdatedAt.Before(cutoff) {
			delete(s.sessions, id)
			removed++
		}
	}
	return removed
}

// Delete removes a single session.
func (s *Store) Delete(id string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.sessions[id]; !ok {
		return false
	}
	delete(s.sessions, id)
	return true
}

// Len reports how many sessions are resident.
func (s *Store) Len() int {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.sessions)
}

// IDs returns the identifiers of every resident session.
func (s *Store) IDs() []string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.sessions))
	for id := range s.sessions {
		out = append(out, id)
	}
	return out
}
