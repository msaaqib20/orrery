package journal

import (
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestMemoryAssignsSequenceNumbers(t *testing.T) {
	j := NewMemory()
	for i := 0; i < 3; i++ {
		if _, err := j.Append(Event{Kind: KindRequest}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	events := j.Events()
	if len(events) != 3 {
		t.Fatalf("got %d events, want 3", len(events))
	}
	for i, e := range events {
		if e.Seq != int64(i+1) {
			t.Errorf("event %d has seq %d, want %d", i, e.Seq, i+1)
		}
		if e.At.IsZero() {
			t.Errorf("event %d has no timestamp", i)
		}
	}
}

func TestMemoryPreservesExplicitTimestamp(t *testing.T) {
	j := NewMemory()
	at := time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	got, err := j.Append(Event{Kind: KindReply, At: at})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !got.At.Equal(at) {
		t.Errorf("At = %v, want %v", got.At, at)
	}
}

func TestMemoryRejectsAppendAfterClose(t *testing.T) {
	j := NewMemory()
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := j.Append(Event{Kind: KindRequest}); err == nil {
		t.Error("expected append after close to fail")
	}
}

func TestMemoryEventsReturnsACopy(t *testing.T) {
	j := NewMemory()
	if _, err := j.Append(Event{Kind: KindRequest}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	events := j.Events()
	events[0].Kind = "tampered"
	if j.Events()[0].Kind != KindRequest {
		t.Error("Events() handed out a reference to internal state")
	}
}

func TestMemoryKinds(t *testing.T) {
	j := NewMemory()
	for _, k := range []string{KindRequest, KindRouted, KindReply} {
		if _, err := j.Append(Event{Kind: k}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	got := strings.Join(j.Kinds(), ",")
	want := "request,routed,reply"
	if got != want {
		t.Errorf("Kinds() = %q, want %q", got, want)
	}
}

func TestMemoryAppendIsConcurrencySafe(t *testing.T) {
	j := NewMemory()
	var wg sync.WaitGroup
	const writers = 16
	const each = 32

	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < each; k++ {
				if _, err := j.Append(Event{Kind: KindRequest}); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
			}
		}()
	}
	wg.Wait()

	events := j.Events()
	if len(events) != writers*each {
		t.Fatalf("got %d events, want %d", len(events), writers*each)
	}
	seen := make(map[int64]bool, len(events))
	for _, e := range events {
		if seen[e.Seq] {
			t.Fatalf("sequence number %d was issued twice", e.Seq)
		}
		seen[e.Seq] = true
	}
}

func TestFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "journal.jsonl")

	j, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if _, err := j.Append(Event{Kind: KindRequest, SessionID: "s1", Fields: map[string]any{"text": "hello"}}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if _, err := j.Append(Event{Kind: KindReply, SessionID: "s1"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	var replayed []Event
	if err := Replay(path, func(e Event) error {
		replayed = append(replayed, e)
		return nil
	}); err != nil {
		t.Fatalf("Replay: %v", err)
	}

	if len(replayed) != 2 {
		t.Fatalf("replayed %d events, want 2", len(replayed))
	}
	if replayed[0].Fields["text"] != "hello" {
		t.Errorf("fields did not survive the round trip: %v", replayed[0].Fields)
	}
	if replayed[1].Seq != 2 {
		t.Errorf("second event has seq %d, want 2", replayed[1].Seq)
	}
}

func TestFileResumesSequenceAcrossReopen(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")

	first, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	for i := 0; i < 5; i++ {
		if _, err := first.Append(Event{Kind: KindRequest}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	if err := first.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	second, err := OpenFile(path)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	defer second.Close()

	got, err := second.Append(Event{Kind: KindRequest})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if got.Seq != 6 {
		t.Errorf("seq after reopen = %d, want 6", got.Seq)
	}
}

func TestFileRejectsAppendAfterClose(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	if err := j.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if _, err := j.Append(Event{Kind: KindRequest}); err == nil {
		t.Error("expected append after close to fail")
	}
	if err := j.Close(); err != nil {
		t.Errorf("second Close should be a no-op, got %v", err)
	}
}

func TestReplayOfMissingFileIsEmpty(t *testing.T) {
	calls := 0
	err := Replay(filepath.Join(t.TempDir(), "absent.jsonl"), func(Event) error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("Replay of a missing file returned %v, want nil", err)
	}
	if calls != 0 {
		t.Errorf("callback ran %d times for a missing file", calls)
	}
}

func TestReplayStopsOnCallbackError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "journal.jsonl")
	j, err := OpenFile(path)
	if err != nil {
		t.Fatalf("OpenFile: %v", err)
	}
	for i := 0; i < 3; i++ {
		if _, err := j.Append(Event{Kind: KindRequest}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	j.Close()

	calls := 0
	err = Replay(path, func(Event) error {
		calls++
		return errStop
	})
	if err != errStop {
		t.Fatalf("Replay returned %v, want the callback error", err)
	}
	if calls != 1 {
		t.Errorf("callback ran %d times, want 1", calls)
	}
}

var errStop = errSentinel("stop")

type errSentinel string

func (e errSentinel) Error() string { return string(e) }
