package session

import (
	"errors"
	"sync"
	"testing"
	"time"
)

func TestEnsureCreatesAndReuses(t *testing.T) {
	store := NewStore(10, time.Hour)

	first := store.Ensure("abc")
	if first.ID != "abc" {
		t.Fatalf("ID = %q", first.ID)
	}
	if store.Len() != 1 {
		t.Fatalf("Len = %d, want 1", store.Len())
	}

	store.Ensure("abc")
	if store.Len() != 1 {
		t.Errorf("Ensure created a duplicate session, Len = %d", store.Len())
	}
}

func TestEnsureAllocatesIDWhenBlank(t *testing.T) {
	store := NewStore(10, time.Hour)
	sess := store.Ensure("")
	if sess.ID == "" {
		t.Fatal("Ensure(\"\") did not allocate an ID")
	}
	if len(sess.ID) != 24 {
		t.Errorf("generated ID %q has length %d, want 24 hex characters", sess.ID, len(sess.ID))
	}
}

func TestNewIDIsUnique(t *testing.T) {
	seen := make(map[string]bool, 512)
	for i := 0; i < 512; i++ {
		id := NewID()
		if seen[id] {
			t.Fatalf("NewID returned a duplicate: %s", id)
		}
		seen[id] = true
	}
}

func TestGetUnknownSession(t *testing.T) {
	store := NewStore(10, time.Hour)
	if _, err := store.Get("nope"); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get returned %v, want ErrNotFound", err)
	}
}

func TestAppendValidatesTurn(t *testing.T) {
	store := NewStore(10, time.Hour)

	if _, err := store.Append("s", Turn{Role: "narrator", Text: "hi"}); !errors.Is(err, ErrBadRole) {
		t.Errorf("bad role returned %v, want ErrBadRole", err)
	}
	if _, err := store.Append("s", Turn{Role: RoleUser, Text: ""}); !errors.Is(err, ErrEmptyText) {
		t.Errorf("empty text returned %v, want ErrEmptyText", err)
	}
	if store.Len() != 0 {
		t.Errorf("a rejected turn created a session, Len = %d", store.Len())
	}
}

func TestAppendTrimsToWindow(t *testing.T) {
	store := NewStore(3, time.Hour)

	for _, text := range []string{"one", "two", "three", "four", "five"} {
		if _, err := store.Append("s", Turn{Role: RoleUser, Text: text}); err != nil {
			t.Fatalf("Append(%q): %v", text, err)
		}
	}

	sess, err := store.Get("s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(sess.Turns) != 3 {
		t.Fatalf("kept %d turns, want 3", len(sess.Turns))
	}
	if sess.Turns[0].Text != "three" {
		t.Errorf("oldest kept turn is %q, want \"three\"", sess.Turns[0].Text)
	}
	if sess.Turns[2].Text != "five" {
		t.Errorf("newest turn is %q, want \"five\"", sess.Turns[2].Text)
	}
}

func TestAppendStampsTime(t *testing.T) {
	store := NewStore(10, time.Hour)
	fixed := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return fixed })

	sess, err := store.Append("s", Turn{Role: RoleUser, Text: "hello"})
	if err != nil {
		t.Fatalf("Append: %v", err)
	}
	if !sess.Turns[0].At.Equal(fixed) {
		t.Errorf("turn timestamp = %v, want %v", sess.Turns[0].At, fixed)
	}
	if !sess.UpdatedAt.Equal(fixed) {
		t.Errorf("UpdatedAt = %v, want %v", sess.UpdatedAt, fixed)
	}
}

func TestCloneIsolatesCallers(t *testing.T) {
	store := NewStore(10, time.Hour)
	if _, err := store.Append("s", Turn{Role: RoleUser, Text: "original"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	got, err := store.Get("s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	got.Turns[0].Text = "tampered"

	again, err := store.Get("s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if again.Turns[0].Text != "original" {
		t.Error("mutating a returned session changed stored state")
	}
}

func TestCloneOfNil(t *testing.T) {
	var s *Session
	if s.Clone() != nil {
		t.Error("Clone of a nil session should be nil")
	}
}

func TestPruneEvictsIdleSessions(t *testing.T) {
	store := NewStore(10, 30*time.Minute)
	now := time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)
	store.SetClock(func() time.Time { return now })

	if _, err := store.Append("stale", Turn{Role: RoleUser, Text: "old"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	now = now.Add(time.Hour)
	if _, err := store.Append("fresh", Turn{Role: RoleUser, Text: "new"}); err != nil {
		t.Fatalf("Append: %v", err)
	}

	if removed := store.Prune(); removed != 1 {
		t.Fatalf("Prune removed %d sessions, want 1", removed)
	}
	if _, err := store.Get("stale"); !errors.Is(err, ErrNotFound) {
		t.Error("the idle session survived pruning")
	}
	if _, err := store.Get("fresh"); err != nil {
		t.Error("the active session was pruned")
	}
}

func TestPruneDisabledByNonPositiveTTL(t *testing.T) {
	store := NewStore(10, 0)
	if _, err := store.Append("s", Turn{Role: RoleUser, Text: "hi"}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if removed := store.Prune(); removed != 0 {
		t.Errorf("Prune removed %d sessions with TTL disabled", removed)
	}
}

func TestDelete(t *testing.T) {
	store := NewStore(10, time.Hour)
	store.Ensure("s")

	if !store.Delete("s") {
		t.Error("Delete of an existing session returned false")
	}
	if store.Delete("s") {
		t.Error("Delete of a missing session returned true")
	}
}

func TestIDs(t *testing.T) {
	store := NewStore(10, time.Hour)
	store.Ensure("a")
	store.Ensure("b")

	ids := store.IDs()
	if len(ids) != 2 {
		t.Fatalf("IDs returned %d entries, want 2", len(ids))
	}
}

func TestStoreIsConcurrencySafe(t *testing.T) {
	store := NewStore(50, time.Hour)
	var wg sync.WaitGroup

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 50; k++ {
				if _, err := store.Append("shared", Turn{Role: RoleUser, Text: "x"}); err != nil {
					t.Errorf("Append: %v", err)
					return
				}
				if _, err := store.Get("shared"); err != nil {
					t.Errorf("Get: %v", err)
					return
				}
				store.Prune()
			}
		}()
	}
	wg.Wait()

	sess, err := store.Get("shared")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(sess.Turns) != 50 {
		t.Errorf("kept %d turns, want the window size of 50", len(sess.Turns))
	}
}

func TestNewStoreClampsWindow(t *testing.T) {
	store := NewStore(0, time.Hour)
	for i := 0; i < 3; i++ {
		if _, err := store.Append("s", Turn{Role: RoleUser, Text: "x"}); err != nil {
			t.Fatalf("Append: %v", err)
		}
	}
	sess, err := store.Get("s")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if len(sess.Turns) != 1 {
		t.Errorf("kept %d turns, want the clamped window of 1", len(sess.Turns))
	}
}
