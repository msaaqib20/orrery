package permission

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestDefaultPostureIsDeny(t *testing.T) {
	p := NewPolicy(false)
	d := p.Check("clock", CapClockRead)
	if d.Allowed {
		t.Error("an ungranted capability was allowed under a deny-by-default policy")
	}
	if d.Reason == "" {
		t.Error("denial did not carry a reason")
	}
}

func TestGrantAllowsExactCapability(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("clock", CapClockRead)

	if !p.Check("clock", CapClockRead).Allowed {
		t.Error("granted capability was denied")
	}
	if p.Check("clock", CapFSWrite).Allowed {
		t.Error("an unrelated capability leaked through the grant")
	}
	if p.Check("other", CapClockRead).Allowed {
		t.Error("a grant to one subject leaked to another")
	}
}

func TestWildcardSubject(t *testing.T) {
	p := NewPolicy(false)
	p.Grant(Wildcard, CapSessionRead)

	if !p.Check("anything", CapSessionRead).Allowed {
		t.Error("a capability granted to * was denied")
	}
	if p.Check("anything", CapShellExec).Allowed {
		t.Error("the * subject granted more than it was given")
	}
}

func TestWildcardCapability(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("trusted", Wildcard)

	if !p.Check("trusted", CapShellExec).Allowed {
		t.Error("a subject holding * was denied")
	}
}

func TestPrefixWildcard(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("files", "fs.*")

	if !p.Check("files", CapFSRead).Allowed {
		t.Error("fs.* did not cover fs.read")
	}
	if !p.Check("files", CapFSWrite).Allowed {
		t.Error("fs.* did not cover fs.write")
	}
	if p.Check("files", CapNetHTTP).Allowed {
		t.Error("fs.* leaked into net.http")
	}
}

func TestDefaultAllow(t *testing.T) {
	p := NewPolicy(true)
	d := p.Check("anything", CapShellExec)
	if !d.Allowed {
		t.Error("default-allow policy denied a capability")
	}
	if !strings.Contains(d.Reason, "default") {
		t.Errorf("reason = %q, want it to mention the default", d.Reason)
	}
}

func TestRevoke(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("clock", CapClockRead)
	p.Revoke("clock", CapClockRead)

	if p.Check("clock", CapClockRead).Allowed {
		t.Error("revoked capability is still allowed")
	}
	p.Revoke("never-granted", CapClockRead) // must not panic
}

func TestCheckAll(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("skill", CapClockRead, CapSessionRead)

	if err := p.CheckAll("skill", []Capability{CapClockRead, CapSessionRead}); err != nil {
		t.Errorf("CheckAll on fully granted caps returned %v", err)
	}
	if err := p.CheckAll("skill", nil); err != nil {
		t.Errorf("CheckAll on an empty set returned %v", err)
	}

	err := p.CheckAll("skill", []Capability{CapClockRead, CapShellExec})
	if !errors.Is(err, ErrDenied) {
		t.Fatalf("CheckAll returned %v, want ErrDenied", err)
	}
	if !strings.Contains(err.Error(), string(CapShellExec)) {
		t.Errorf("error %q does not name the denied capability", err)
	}
}

func TestFromGrants(t *testing.T) {
	p, err := FromGrants(false, map[string][]string{
		"clock":  {"clock.read"},
		"files":  {"fs.*"},
		"help":   {},
		"anyone": {"*"},
	})
	if err != nil {
		t.Fatalf("FromGrants: %v", err)
	}

	if !p.Check("clock", CapClockRead).Allowed {
		t.Error("clock.read was not applied")
	}
	if !p.Check("files", CapFSWrite).Allowed {
		t.Error("fs.* was not applied")
	}
	if !p.Check("anyone", CapNetHTTP).Allowed {
		t.Error("* was not applied")
	}

	subjects := strings.Join(p.Subjects(), ",")
	if subjects != "anyone,clock,files,help" {
		t.Errorf("Subjects() = %q, want a sorted list including the empty grant", subjects)
	}
}

func TestFromGrantsRejectsUnknownCapability(t *testing.T) {
	_, err := FromGrants(false, map[string][]string{"rogue": {"launch.missiles"}})
	if err == nil {
		t.Fatal("expected an unknown capability name to be rejected")
	}
	if !strings.Contains(err.Error(), "launch.missiles") {
		t.Errorf("error %q does not name the offending capability", err)
	}
}

func TestGrantsFor(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("skill", CapSessionRead, CapClockRead)

	got := p.GrantsFor("skill")
	if len(got) != 2 {
		t.Fatalf("GrantsFor returned %d capabilities, want 2", len(got))
	}
	if got[0] != CapClockRead || got[1] != CapSessionRead {
		t.Errorf("GrantsFor returned %v, want a sorted list", got)
	}
	if len(p.GrantsFor("absent")) != 0 {
		t.Error("GrantsFor on an unknown subject returned entries")
	}
}

func TestKnownIsSorted(t *testing.T) {
	known := Known()
	if len(known) == 0 {
		t.Fatal("Known() is empty")
	}
	for i := 1; i < len(known); i++ {
		if known[i-1] >= known[i] {
			t.Fatalf("Known() is not sorted at index %d: %v", i, known)
		}
	}
}

func TestPolicyIsConcurrencySafe(t *testing.T) {
	p := NewPolicy(false)
	p.Grant("skill", CapClockRead)

	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for k := 0; k < 100; k++ {
				p.Check("skill", CapClockRead)
				p.Grant("skill", CapSessionRead)
				p.GrantsFor("skill")
				p.Subjects()
			}
		}()
	}
	wg.Wait()
}
