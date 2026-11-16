package main

import (
	"strings"
	"testing"
)

// TestDockerBuildsForTheTargetArch guards coord task
// v1.4-docker-arm64-binary-arch. Both image workflows build linux/amd64 and
// linux/arm64, but each go build in the Dockerfile set GOARCH=amd64, so the
// arm64 image held amd64 binaries on an arm64 base. No CI job runs the arm64
// image, so this reads the file that decides the architecture.
func TestDockerBuildsForTheTargetArch(t *testing.T) {
	dockerfile := readRepoFile(t, "Dockerfile")

	if strings.Contains(dockerfile, "GOARCH=amd64") {
		t.Error("Dockerfile hardcodes GOARCH=amd64; the arm64 image would hold amd64 binaries")
	}
	// The builder stage runs on the build host and cross-compiles: without
	// --platform=$BUILDPLATFORM, buildx runs the arm64 compile under emulation.
	if !strings.Contains(dockerfile, "FROM --platform=$BUILDPLATFORM golang:") {
		t.Error("the builder stage does not run on $BUILDPLATFORM")
	}
	builds := strings.Count(dockerfile, "go build")
	targeted := strings.Count(dockerfile, "GOARCH=${TARGETARCH}")
	if builds == 0 || targeted != builds {
		t.Errorf("%d go build commands, %d of them set GOARCH=${TARGETARCH}; want all", builds, targeted)
	}
	// TARGETARCH is a predefined build argument, but a stage sees it only
	// after an ARG line inside that stage.
	argAt := strings.Index(dockerfile, "ARG TARGETARCH")
	fromAt := strings.Index(dockerfile, "AS builder")
	if argAt < 0 || fromAt < 0 || argAt < fromAt {
		t.Error("Dockerfile does not declare ARG TARGETARCH inside the builder stage")
	}
}
