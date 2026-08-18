// Package version exposes build metadata for the orrery binaries.
//
// Version, Commit and BuiltAt are intended to be overridden at link time:
//
//	go build -ldflags "-X github.com/msaaqib20/orrery/internal/version.Version=1.2.3"
package version

import (
	goruntime "runtime"
	"strings"
)

// Values injected at build time. See the Makefile's LDFLAGS.
var (
	Version = "0.1.0-alpha"
	Commit  = "unknown"
	BuiltAt = "unknown"
)

// Info is a serialisable snapshot of the build metadata.
type Info struct {
	Version   string `json:"version"`
	Commit    string `json:"commit"`
	BuiltAt   string `json:"built_at"`
	GoVersion string `json:"go_version"`
	Platform  string `json:"platform"`
}

// Get returns the current build metadata.
func Get() Info {
	return Info{
		Version:   Version,
		Commit:    Commit,
		BuiltAt:   BuiltAt,
		GoVersion: goruntime.Version(),
		Platform:  goruntime.GOOS + "/" + goruntime.GOARCH,
	}
}

// String renders the metadata as a single human-readable line.
func (i Info) String() string {
	var b strings.Builder
	b.WriteString("orrery ")
	b.WriteString(i.Version)
	if i.Commit != "" && i.Commit != "unknown" {
		b.WriteString(" (")
		b.WriteString(shortCommit(i.Commit))
		b.WriteString(")")
	}
	b.WriteString(" ")
	b.WriteString(i.GoVersion)
	b.WriteString(" ")
	b.WriteString(i.Platform)
	return b.String()
}

func shortCommit(c string) string {
	if len(c) > 12 {
		return c[:12]
	}
	return c
}
