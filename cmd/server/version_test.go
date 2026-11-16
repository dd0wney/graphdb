package main

import (
	"os"
	"strings"
	"testing"

	"github.com/dd0wney/graphdb/pkg/api"
	"github.com/dd0wney/graphdb/pkg/updater"
)

// TestWireBuildVersion_ReachesEveryReader guards GitHub #766: the server set
// the build version for backup manifests only, so /health said "1.0.0" and the
// update status said "dev" on the same binary.
func TestWireBuildVersion_ReachesEveryReader(t *testing.T) {
	origAPI, origUpdater := api.BuildVersion, updater.Version
	t.Cleanup(func() { api.BuildVersion, updater.Version = origAPI, origUpdater })

	wireBuildVersion("v9.8.7-wire-test")

	if api.BuildVersion != "v9.8.7-wire-test" {
		t.Errorf("api.BuildVersion = %q, want %q", api.BuildVersion, "v9.8.7-wire-test")
	}
	if updater.Version != "v9.8.7-wire-test" {
		t.Errorf("updater.Version = %q, want %q", updater.Version, "v9.8.7-wire-test")
	}
}

// TestWireBuildVersion_ReleaseStillSeesUpdates guards the review finding on
// #766: goreleaser links "1.12.1" with no "v", which updater.IsNewer rejects,
// so a release server would report that no update exists, ever.
func TestWireBuildVersion_ReleaseStillSeesUpdates(t *testing.T) {
	origAPI, origUpdater := api.BuildVersion, updater.Version
	t.Cleanup(func() { api.BuildVersion, updater.Version = origAPI, origUpdater })

	wireBuildVersion("1.12.1")

	if !updater.IsNewer(updater.Version, "v1.13.0") {
		t.Fatalf("IsNewer(%q, v1.13.0) = false: a release server cannot see a newer release", updater.Version)
	}
}

// TestDockerBuildsSetTheVersion guards the Docker half of GitHub #766. The
// Dockerfile ran `git describe`, but .dockerignore keeps .git out of the build
// context, so every published image linked an empty version and nothing failed.
// No CI job builds the image and asks it for its version, so this reads the
// files that decide it.
func TestDockerBuildsSetTheVersion(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")
	if strings.Contains(dockerfile, "$(git describe") {
		t.Error("Dockerfile runs git describe; .dockerignore excludes .git, so it links an empty version")
	}
	// An ARG declared before the first FROM is out of scope inside the stage,
	// so ${VERSION} would expand to "" while every substring still matched.
	argAt, fromAt := strings.Index(dockerfile, "ARG VERSION"), strings.Index(dockerfile, "FROM golang")
	if argAt < 0 {
		t.Error("Dockerfile declares no VERSION build argument")
	} else if fromAt < 0 || argAt < fromAt {
		t.Error("Dockerfile declares VERSION outside the builder stage, where the go build cannot see it")
	}
	if !strings.Contains(dockerfile, "-X main.Version=${VERSION}") {
		t.Error("Dockerfile does not link VERSION into main.Version")
	}

	for _, wf := range []string{".github/workflows/docker-publish.yml", ".github/workflows/release.yml"} {
		if !strings.Contains(readRepoFile(t, wf), "VERSION=${{ steps.meta.outputs.version }}") {
			t.Errorf("%s does not pass the image version as the VERSION build argument", wf)
		}
	}
}

func readRepoFile(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile("../../" + rel)
	if err != nil {
		t.Fatalf("read %s: %v", rel, err)
	}
	return string(b)
}
