package main

import (
	"github.com/dd0wney/graphdb/pkg/api"
	"github.com/dd0wney/graphdb/pkg/buildinfo"
	"github.com/dd0wney/graphdb/pkg/updater"
)

// wireBuildVersion gives every reader of the build version (GET /health, the
// backup manifest, the update status) the same value. linked is main.Version.
// Call it once, before the server accepts a request.
func wireBuildVersion(linked string) {
	version := buildinfo.Resolve(linked)
	api.BuildVersion = version
	updater.Version = version
}
