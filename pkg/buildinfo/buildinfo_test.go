package buildinfo

import (
	"runtime/debug"
	"testing"
)

func reader(mainVersion string, settings map[string]string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		info := &debug.BuildInfo{Main: debug.Module{Version: mainVersion}}
		for k, v := range settings {
			info.Settings = append(info.Settings, debug.BuildSetting{Key: k, Value: v})
		}
		return info, true
	}
}

func noBuildInfo() (*debug.BuildInfo, bool) { return nil, false }

// TestResolve covers the cases whose answer does not depend on a policy
// choice, then the rule for a checkout build: dev plus the commit as semver
// build metadata.
func TestResolve(t *testing.T) {
	cases := []struct {
		name   string
		linked string
		read   func() (*debug.BuildInfo, bool)
		want   string
	}{
		{"linked release wins over module version", "v1.12.1", reader("v1.11.0", nil), "v1.12.1"},
		{"goreleaser form without v is kept as given", "1.12.1", reader("(devel)", nil), "1.12.1"},
		{"go install of a tag gives the module version", Unset, reader("v1.12.1", nil), "v1.12.1"},
		{"empty linked value counts as unset", "", reader("v1.12.1", nil), "v1.12.1"},
		{"no build info and no linked value", Unset, noBuildInfo, Unset},
		{"empty everything is never empty", "", reader("", nil), Unset},
		{"checkout build names its commit", Unset,
			reader("(devel)", map[string]string{"vcs.revision": "fa43632d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b", "vcs.modified": "false"}),
			"dev+fa43632d1e2f"},
		{"checkout build with local changes is marked dirty", Unset,
			reader("(devel)", map[string]string{"vcs.revision": "fa43632d1e2f3a4b5c6d7e8f9a0b1c2d3e4f5a6b", "vcs.modified": "true"}),
			"dev+fa43632d1e2f.dirty"},
		{"devel without a revision is plain dev", "", reader("(devel)", nil), Unset},
		{"short revision is kept whole", Unset, reader("(devel)", map[string]string{"vcs.revision": "abc123"}), "dev+abc123"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := resolve(tc.linked, tc.read); got != tc.want {
				t.Fatalf("resolve(%q) = %q, want %q", tc.linked, got, tc.want)
			}
		})
	}
}
