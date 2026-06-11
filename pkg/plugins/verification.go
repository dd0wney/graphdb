package plugins

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

// M-15 (AUDIT_security_2026-06-10): plugin.Open executes the .so's init()
// before any interface assertion, so a writable plugin directory (or a
// CWD-relative default) was remote-code-execution as the server. Loading
// is now fail-closed: absolute owner-only directory, and every .so must
// match a SHA-256 manifest BEFORE plugin.Open. The manifest-generation
// tooling lives with the plugin builds (graphdb-enterprise); regenerate
// with `shasum -a 256 *.so > MANIFEST.sha256` (or sha256sum) in the
// plugin directory.

// pluginManifestName is the manifest file inside the plugin directory,
// in sha256sum output format: "<hex-digest>  <filename>" per line.
const pluginManifestName = "MANIFEST.sha256"

// allowUnverifiedEnv is the explicit transition opt-out: skips the
// manifest requirement (NOT the directory checks) with a loud warning.
const allowUnverifiedEnv = "GRAPHDB_PLUGIN_ALLOW_UNVERIFIED"

type pluginManifest map[string]string // basename -> lowercase hex sha256

// verifyPluginDir enforces the directory-level preconditions: absolute
// path (no CWD-relative "./plugins" ambush), owned by the server's uid,
// and not writable by group or other.
func verifyPluginDir(dir string) error {
	if !filepath.IsAbs(dir) {
		return fmt.Errorf("plugin directory %q must be an absolute path", dir)
	}
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("failed to stat plugin directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("plugin path %q is not a directory", dir)
	}
	if info.Mode().Perm()&0o022 != 0 {
		return fmt.Errorf("plugin directory %q is group/other-writable (%v) — anyone with write access gains code execution as the server; chmod 700 it", dir, info.Mode().Perm())
	}
	if stat, ok := info.Sys().(*syscall.Stat_t); ok && int(stat.Uid) != os.Getuid() {
		return fmt.Errorf("plugin directory %q is owned by uid %d, not the server's uid %d", dir, stat.Uid, os.Getuid())
	}
	return nil
}

// loadPluginManifest parses MANIFEST.sha256 from the plugin directory.
func loadPluginManifest(dir string) (pluginManifest, error) {
	f, err := os.Open(filepath.Join(dir, pluginManifestName))
	if err != nil {
		return nil, fmt.Errorf("failed to open plugin manifest: %w", err)
	}
	defer f.Close()

	manifest := make(pluginManifest)
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// sha256sum format: digest, whitespace (possibly with a binary-mode
		// '*' prefix on the name), filename.
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return nil, fmt.Errorf("malformed manifest line %q (want \"<sha256> <file>\")", line)
		}
		digest, name := strings.ToLower(fields[0]), strings.TrimPrefix(fields[1], "*")
		if len(digest) != sha256.Size*2 {
			return nil, fmt.Errorf("malformed digest for %q in manifest", name)
		}
		manifest[filepath.Base(name)] = digest
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("failed to read plugin manifest: %w", err)
	}
	return manifest, nil
}

// verifyPluginHash checks one .so against the manifest. Fail-closed:
// unlisted files are as fatal as mismatched ones.
func verifyPluginHash(path string, manifest pluginManifest) error {
	want, ok := manifest[filepath.Base(path)]
	if !ok {
		return fmt.Errorf("plugin %q is not in the manifest — refusing to load", filepath.Base(path))
	}
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("failed to open plugin for verification: %w", err)
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return fmt.Errorf("failed to hash plugin: %w", err)
	}
	got := hex.EncodeToString(h.Sum(nil))
	if got != want {
		return fmt.Errorf("plugin %q SHA-256 mismatch: manifest %s, file %s — refusing to load", filepath.Base(path), want, got)
	}
	return nil
}

func allowUnverifiedPlugins() bool {
	return os.Getenv(allowUnverifiedEnv) == "true"
}
