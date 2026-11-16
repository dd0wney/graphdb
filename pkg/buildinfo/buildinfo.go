// Package buildinfo decides which version string a graphdb binary reports.
//
// A release build sets the version at link time (-X main.Version=... from
// goreleaser, or the VERSION build argument in the Dockerfile). Other builds do
// not: `go install github.com/dd0wney/graphdb/cmd/server@v1.12.1` and a plain
// `go build` leave the linked value at its default. Those builds still carry
// what the Go toolchain recorded, which runtime/debug.ReadBuildInfo returns.
//
// Every place that shows the version (GET /health, backup manifests, the update
// status) reads one value that Resolve produced, so the places cannot disagree.
package buildinfo

import (
	"runtime/debug"
	"strings"

	"golang.org/x/mod/semver"
)

// Unset is the linked default in cmd/server. Resolve treats it, and the empty
// string, as "the link step did not set a version".
const Unset = "dev"

// Resolve returns the version this binary reports. linked is the value the link
// step set (main.Version in cmd/server).
func Resolve(linked string) string {
	return resolve(linked, debug.ReadBuildInfo)
}

// resolve is Resolve with the build-info reader passed in, so a test can supply
// the module version and VCS settings that a real build would record.
func resolve(linked string, read func() (*debug.BuildInfo, bool)) string {
	if v, ok := releaseVersion(linked); ok {
		return v
	}
	info, ok := read()
	if !ok || info == nil {
		return Unset
	}
	// Since Go 1.24 a checkout build records a pseudo-version here
	// ("v1.12.1-0.20261116003439-fa43632b4e99+dirty"). "(devel)" remains for
	// a build with VCS stamping off (-buildvcs=false); it is not a version, so
	// it falls through to the commit.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}
	return Unset + commitSuffix(info.Settings)
}

// releaseVersion reports whether linked is a release version, in the "v" form
// that updater.IsNewer compares. goreleaser {{.Version}} and
// docker/metadata-action give "1.12.1"; a branch image gives "main", which is
// not a version, so the caller falls through to what the toolchain recorded.
func releaseVersion(linked string) (string, bool) {
	if linked == "" || linked == Unset {
		return "", false
	}
	v := linked
	if !strings.HasPrefix(v, "v") {
		v = "v" + v
	}
	if !semver.IsValid(v) {
		return "", false
	}
	return v, true
}

// commitSuffix names the commit a checkout build came from, as semver build
// metadata ("+<12 hex>", "+<12 hex>.dirty"), so two dev builds stay
// distinguishable while a semver comparison still ignores the suffix. It is ""
// when the build recorded no revision, which is the case in a Docker build.
func commitSuffix(settings []debug.BuildSetting) string {
	var revision, modified string
	for _, s := range settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision == "" {
		return ""
	}
	if len(revision) > 12 {
		revision = revision[:12]
	}
	if modified == "true" {
		return "+" + revision + ".dirty"
	}
	return "+" + revision
}
