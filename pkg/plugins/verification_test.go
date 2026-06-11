package plugins

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// M-15 (AUDIT_security_2026-06-10): plugin.Open runs the .so's init()
// before any interface assertion, so write access to the plugin dir was
// RCE as the server. Loading now (1) requires an absolute, owner-only
// plugin dir, and (2) verifies each .so against a SHA-256 manifest
// BEFORE plugin.Open — fail-closed, with an explicit
// GRAPHDB_PLUGIN_ALLOW_UNVERIFIED=true transition opt-out.

func testLoader(t *testing.T) *PluginLoader {
	t.Helper()
	return NewPluginLoader(nil, slog.New(slog.NewTextHandler(os.Stderr, nil)))
}

func writeFakePlugin(t *testing.T, dir, name string) (path string, digest string) {
	t.Helper()
	content := []byte("not-a-real-shared-object-" + name)
	path = filepath.Join(dir, name)
	if err := os.WriteFile(path, content, 0o600); err != nil {
		t.Fatalf("write fake plugin: %v", err)
	}
	sum := sha256.Sum256(content)
	return path, hex.EncodeToString(sum[:])
}

func writeManifest(t *testing.T, dir string, lines ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, pluginManifestName), []byte(strings.Join(lines, "\n")+"\n"), 0o600); err != nil {
		t.Fatalf("write manifest: %v", err)
	}
}

func pluginTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return dir
}

func TestLoadPlugins_RejectsRelativeDir(t *testing.T) {
	err := testLoader(t).LoadPluginsFromDir(context.Background(), "plugins")
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative plugin dir must be rejected, got: %v", err)
	}
}

func TestLoadPlugins_RejectsGroupWritableDir(t *testing.T) {
	dir := pluginTestDir(t)
	writeFakePlugin(t, dir, "p.so")
	if err := os.Chmod(dir, 0o770); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	err := testLoader(t).LoadPluginsFromDir(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "writable") {
		t.Fatalf("group-writable plugin dir must be rejected, got: %v", err)
	}
}

func TestLoadPlugins_MissingManifestFailsClosed(t *testing.T) {
	dir := pluginTestDir(t)
	writeFakePlugin(t, dir, "p.so")
	err := testLoader(t).LoadPluginsFromDir(context.Background(), dir)
	if err == nil || !strings.Contains(err.Error(), "manifest") {
		t.Fatalf("missing manifest must fail closed, got: %v", err)
	}
}

func TestLoadPlugins_AllowUnverifiedEnvOptOut(t *testing.T) {
	dir := pluginTestDir(t)
	// No manifest, no plugins — just the gate itself.
	t.Setenv("GRAPHDB_PLUGIN_ALLOW_UNVERIFIED", "true")
	if err := testLoader(t).LoadPluginsFromDir(context.Background(), dir); err != nil {
		t.Fatalf("opt-out should bypass the manifest requirement, got: %v", err)
	}
}

func TestVerifyPlugin_HashMismatchBlocksBeforeOpen(t *testing.T) {
	dir := pluginTestDir(t)
	path, _ := writeFakePlugin(t, dir, "evil.so")
	writeManifest(t, dir, fmt.Sprintf("%064d  evil.so", 0)) // wrong digest

	manifest, err := loadPluginManifest(dir)
	if err != nil {
		t.Fatalf("loadPluginManifest: %v", err)
	}
	err = verifyPluginHash(path, manifest)
	if err == nil || !strings.Contains(err.Error(), "mismatch") {
		t.Fatalf("tampered plugin must be rejected before plugin.Open, got: %v", err)
	}
}

func TestVerifyPlugin_NotInManifestBlocked(t *testing.T) {
	dir := pluginTestDir(t)
	path, _ := writeFakePlugin(t, dir, "rogue.so")
	writeManifest(t, dir, fmt.Sprintf("%064d  other.so", 0))

	manifest, err := loadPluginManifest(dir)
	if err != nil {
		t.Fatalf("loadPluginManifest: %v", err)
	}
	err = verifyPluginHash(path, manifest)
	if err == nil || !strings.Contains(err.Error(), "not in") {
		t.Fatalf("unlisted plugin must be rejected, got: %v", err)
	}
}

func TestVerifyPlugin_MatchingHashPasses(t *testing.T) {
	dir := pluginTestDir(t)
	path, digest := writeFakePlugin(t, dir, "good.so")
	writeManifest(t, dir, digest+"  good.so")

	manifest, err := loadPluginManifest(dir)
	if err != nil {
		t.Fatalf("loadPluginManifest: %v", err)
	}
	if err := verifyPluginHash(path, manifest); err != nil {
		t.Fatalf("matching digest must verify, got: %v", err)
	}
}

// End-to-end through LoadPluginsFromDir: a verified-but-garbage .so gets
// PAST verification (its failure is plugin.Open's, logged and skipped),
// while a tampered one never reaches plugin.Open. Both load zero plugins;
// the observable difference is the returned error/nil and logs — what we
// pin here is that the dir-level call succeeds and loads nothing instead
// of crashing or loading the tampered file.
func TestLoadPlugins_VerifiedGarbageIsOpenFailureNotVerificationFailure(t *testing.T) {
	dir := pluginTestDir(t)
	_, digest := writeFakePlugin(t, dir, "good.so")
	writeManifest(t, dir, digest+"  good.so")

	loader := testLoader(t)
	if err := loader.LoadPluginsFromDir(context.Background(), dir); err != nil {
		t.Fatalf("verified dir load should not error at the dir level: %v", err)
	}
	if n := len(loader.GetAllPlugins()); n != 0 {
		t.Fatalf("garbage .so cannot have loaded, got %d plugins", n)
	}
}
