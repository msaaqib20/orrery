// Package journal implements orrery's append-only event log.
//
// Every externally visible decision the runtime makes is written here as one
// JSON object per line. The log is the audit trail and the debugging tool of
// last resort: it is never rewritten in place, and readers may replay it from
// the beginning at any time.
package journal

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Event kinds written by the runtime.
const (
	KindRequest    = "request"
	KindRouted     = "routed"
	KindSkill      = "skill"
	KindProvider   = "provider"
	KindReply      = "reply"
	KindDenied     = "denied"
	KindError      = "error"
	KindLifecycle  = "lifecycle"
	maxLineBytes   = 1 << 20
	journalDirPerm = 0o750
	journalPerm    = 0o640
)

// Event is a single journal record.
type Event struct {
	Seq       int64          `json:"seq"`
	At        time.Time      `json:"at"`
	Kind      string         `json:"kind"`
	SessionID string         `json:"session_id,omitempty"`
	Fields    map[string]any `json:"fields,omitempty"`
}

// Journal is the write side of the log.
type Journal interface {
	Append(Event) (Event, error)
	Close() error
}

// Memory is an in-process journal. It is used by tests and by deployments that
// explicitly opt out of on-disk history.
type Memory struct {
	mu     sync.Mutex
	seq    int64
	events []Event
	closed bool
}

// NewMemory returns an empty in-memory journal.
func NewMemory() *Memory { return &Memory{} }

// Append records e, assigning it the next sequence number.
func (m *Memory) Append(e Event) (Event, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return Event{}, fmt.Errorf("journal: append after close")
	}
	m.seq++
	e.Seq = m.seq
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}
	m.events = append(m.events, e)
	return e, nil
}

// Events returns a copy of everything recorded so far.
func (m *Memory) Events() []Event {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Event, len(m.events))
	copy(out, m.events)
	return out
}

// Kinds returns the kind of each recorded event, in order.
func (m *Memory) Kinds() []string {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]string, len(m.events))
	for i, e := range m.events {
		out[i] = e.Kind
	}
	return out
}

// Close marks the journal closed. Further appends fail.
func (m *Memory) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// File is a journal backed by a newline-delimited JSON file.
type File struct {
	mu     sync.Mutex
	f      *os.File
	w      *bufio.Writer
	seq    int64
	closed bool
}

// OpenFile opens (or creates) the journal at path, creating parent directories
// as needed. If the file already holds events, sequence numbering resumes from
// the highest sequence found rather than restarting at one.
func OpenFile(path string) (*File, error) {
	if err := os.MkdirAll(filepath.Dir(path), journalDirPerm); err != nil {
		return nil, fmt.Errorf("journal: create directory: %w", err)
	}

	seq, err := highestSeq(path)
	if err != nil {
		return nil, err
	}

	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, journalPerm)
	if err != nil {
		return nil, fmt.Errorf("journal: open: %w", err)
	}
	return &File{f: f, w: bufio.NewWriter(f), seq: seq}, nil
}

// Append writes e to disk and flushes it. Durability is favoured over
// throughput here: a journal that loses its tail on a crash is worthless as an
// audit trail.
func (j *File) Append(e Event) (Event, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return Event{}, fmt.Errorf("journal: append after close")
	}

	j.seq++
	e.Seq = j.seq
	if e.At.IsZero() {
		e.At = time.Now().UTC()
	}

	line, err := json.Marshal(e)
	if err != nil {
		j.seq--
		return Event{}, fmt.Errorf("journal: encode event: %w", err)
	}
	if _, err := j.w.Write(append(line, '\n')); err != nil {
		return Event{}, fmt.Errorf("journal: write: %w", err)
	}
	if err := j.w.Flush(); err != nil {
		return Event{}, fmt.Errorf("journal: flush: %w", err)
	}
	return e, nil
}

// Close flushes and closes the underlying file.
func (j *File) Close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed {
		return nil
	}
	j.closed = true
	if err := j.w.Flush(); err != nil {
		j.f.Close()
		return fmt.Errorf("journal: flush on close: %w", err)
	}
	return j.f.Close()
}

// Replay walks every event in the journal at path in write order. A missing
// file replays as an empty log, which keeps first-boot handling simple.
func Replay(path string, fn func(Event) error) error {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("journal: open for replay: %w", err)
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), maxLineBytes)
	line := 0
	for sc.Scan() {
		line++
		raw := sc.Bytes()
		if len(raw) == 0 {
			continue
		}
		var e Event
		if err := json.Unmarshal(raw, &e); err != nil {
			return fmt.Errorf("journal: line %d: %w", line, err)
		}
		if err := fn(e); err != nil {
			return err
		}
	}
	if err := sc.Err(); err != nil {
		return fmt.Errorf("journal: scan: %w", err)
	}
	return nil
}

func highestSeq(path string) (int64, error) {
	var highest int64
	err := Replay(path, func(e Event) error {
		if e.Seq > highest {
			highest = e.Seq
		}
		return nil
	})
	return highest, err
}
