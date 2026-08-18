package version

import "testing"

func TestGetPopulatesRuntimeFields(t *testing.T) {
	info := Get()
	if info.GoVersion == "" {
		t.Error("GoVersion is empty")
	}
	if info.Platform == "" {
		t.Error("Platform is empty")
	}
	if info.Version == "" {
		t.Error("Version is empty")
	}
}

func TestStringIncludesVersion(t *testing.T) {
	info := Info{Version: "1.2.3", Commit: "abcdef0123456789", GoVersion: "go1.22", Platform: "linux/amd64"}
	got := info.String()
	want := "orrery 1.2.3 (abcdef012345) go1.22 linux/amd64"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestStringOmitsUnknownCommit(t *testing.T) {
	info := Info{Version: "1.2.3", Commit: "unknown", GoVersion: "go1.22", Platform: "linux/amd64"}
	got := info.String()
	want := "orrery 1.2.3 go1.22 linux/amd64"
	if got != want {
		t.Errorf("String() = %q, want %q", got, want)
	}
}

func TestShortCommitLeavesShortValues(t *testing.T) {
	if got := shortCommit("abc"); got != "abc" {
		t.Errorf("shortCommit(abc) = %q", got)
	}
}
