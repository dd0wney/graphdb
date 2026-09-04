package backup_test

// Gate for threading pkg/vfs through the archive-write path.
//
// Before this, WriteArchive called os.Stat, os.Open and filepath.Walk
// directly: no driver observed a single one of its operations, so a fault
// driver or a crash sweep pointed at this package could not reach it at all.
// A path outside the seam produces no signal — "found nothing" and "never
// looked" render identically. See pkg/vfs's package comment.
//
// filepath.Walk visits entries in LEXICAL order. vfs.FileSystem.ReadDir
// carries no such guarantee across drivers (see the method's doc comment in
// pkg/vfs/vfs.go), so the walk this file exercises must sort explicitly
// rather than depend on the driver returning entries pre-sorted — which is
// what the OS happens to do and what the interface does not promise. Getting
// this wrong would not break the archive; it would silently reorder which
// file lands where in the tar stream for the same input, which is exactly
// the kind of difference a manifest-comparing consumer would notice as
// corruption.
//
// RED-FIRST. These tests were written and run against the pre-change
// archive.go, which has no WriteArchiveWithFS at all. What that run printed
// is in the pull request body, under "Golden data provenance" — a compile
// failure, which is the correct red state for a test exercising a function
// that does not yet exist.

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/dd0wney/graphdb/pkg/backup"
	"github.com/dd0wney/graphdb/pkg/vfs"
	"github.com/dd0wney/graphdb/pkg/vfs/vfstest"
)

// sha256Hex is the hex-encoded SHA-256 of b, in the same form
// preChangeGolden and the manifest's own File.Sha256 field use.
func sha256Hex(b []byte) string {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:])
}

// goldenFile is one archive member's identity: relative path, exact size,
// and content hash.
type goldenFile struct {
	path   string
	size   int64
	sha256 string
}

// preChangeGolden was captured by running the PRE-CHANGE backup.WriteArchive
// (filepath.Walk, os.Open, os.Stat — none of it threaded) against the
// fixture buildVFSFixture builds, before any line in archive.go or
// extract.go on this branch was touched. It is not a value predicted and
// then satisfied by writing code to match; it is what the old code actually
// produced. archive.go and extract.go have not changed on main since this
// branch's parent commit (a980270), so origin/main's copies of those two
// files are the pre-change code. See the pull request body, under "Golden
// data provenance", for the exact command sequence and the full captured
// output, including the pre-change manifest.json.
//
// Comparing against this — rather than comparing two runs of the new code
// against each other — is what makes the equivalence test below mean
// anything. Two runs of a broken implementation can still agree with each
// other.
var preChangeGolden = []goldenFile{
	{path: "snapshot.json", size: 15, sha256: "1568fe64257987cf2b6d6daa773460ab3e8c22ca6c0167afe5bfae43b16160b8"},
	{path: "wal/a.log", size: 5, sha256: "5582158026680fc71d5d802b2c5a05456f436637a0099aa91f25a20c53ae2df6"},
	{path: "wal/b.log", size: 5, sha256: "11a2a79ceb45d906ede0e7b28bbb4ea0c82ade1dbc7e5642c911ae796eebd6fd"},
	{path: "wal/quarantine.tmp/inner.log", size: 20, sha256: "12643d4e6e86f3a7667ee43e5e8607059f3b35ec56b3fa3e870ac61aded21f5e"},
	{path: "wal/sub/a.log", size: 9, sha256: "d3ed41267818636d95b5363496e792cd258e3323ed56157d50e0c5a9e148e849"},
	{path: "wal/sub/z.log", size: 9, sha256: "15cb47a801503ffeaa4a69696346a5a4d4753cc74e9ee2c2597d70f0890bb5f9"},
	{path: "auth/roles.json", size: 10, sha256: "91d404e96d7fce710bb9b50109c1943c97faf5fa72f7ce4efed28ac84e9164d8"},
	{path: "lsa/tenant1.lsa", size: 11, sha256: "057952eb50d9ab1e5a60d24e7f51c046a589a3631dc2004e827f2c204abcdd28"},
	{path: "edgestore/nested/shard1.dat", size: 18, sha256: "b43305eecbb8e5c00b3885fd6a37a680de01540950442e5d093eeb9f56356796"},
	{path: "edgestore/shard10.dat", size: 12, sha256: "2f10089bb95249365e68588e5bc82bb7cfbc99cb1f1cb7e8d36c80ae7efaf4d4"},
	{path: "edgestore/shard2.dat", size: 11, sha256: "1509e898457f62084c197c971b92dbe9168f04f9f1144d19da034fcd8c0e081c"},
}

// goldenOrder is preChangeGolden's paths, in the order the pre-change code
// produced them.
func goldenOrder() []string {
	out := make([]string, len(preChangeGolden))
	for i, g := range preChangeGolden {
		out[i] = g.path
	}
	return out
}

// buildVFSFixture writes the exact fixture preChangeGolden was captured
// from: files under wal/, auth/, lsa/ and edgestore/ whose names do NOT sort
// in creation order (b.log before a.log; shard10.dat before shard2.dat,
// where lexical order and numeric order disagree), one nested directory
// (wal/sub, edgestore/nested), one .tmp file that must be excluded
// (wal/ignore.tmp), one directory whose NAME ends in .tmp and holds a
// regular file (wal/quarantine.tmp/inner.log), and one empty directory
// (lsa/emptydir).
//
// wal/quarantine.tmp/inner.log exercises the subtle case archive.go's doc
// comment on walkFilesWithFS claims to preserve: the walk excludes a
// .tmp-suffixed FILE from the archive, but a .tmp-suffixed DIRECTORY is
// still recursed into, so a regular file inside it is still archived. The
// pre-change filepath.Walk callback (pkg/backup/archive.go at commit
// a980270, this branch's parent) skips a .tmp-suffixed entry as an archive
// member but never returns filepath.SkipDir, so it still descends into a
// .tmp-named directory and archives the regular files inside it. Verify
// directly: `git show a980270:pkg/backup/archive.go`.
//
// lsa/emptydir is not an oversight left unarchived: neither the pre-change
// nor the new walk ever emits an archive member for a directory itself, and
// an empty directory has no files under it to recurse into, so it correctly
// contributes zero members to the archive either way — the omission is
// deliberate and matches on both sides of this change.
//
// A single-file or already-sorted fixture would make the ordering tests
// below vacuous: an unsorted walk and a correctly-sorted walk produce the
// same output for input that was already in order, so a passing test would
// prove nothing about whether the sort is real.
func buildVFSFixture(t *testing.T, dir string) {
	t.Helper()
	write := func(rel, body string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", rel, err)
		}
		if err := os.WriteFile(full, []byte(body), 0o600); err != nil {
			t.Fatalf("fixture: write %s: %v", rel, err)
		}
	}
	mkdirEmpty := func(rel string) {
		full := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(full, 0o755); err != nil {
			t.Fatalf("fixture: mkdir %s: %v", rel, err)
		}
	}
	write("snapshot.json", "snap-json-bytes")
	write("wal/b.log", "wal-b")
	write("wal/a.log", "wal-a")
	write("wal/ignore.tmp", "should be excluded")
	write("wal/sub/z.log", "wal-sub-z")
	write("wal/sub/a.log", "wal-sub-a")
	write("wal/quarantine.tmp/inner.log", "wal-quarantine-inner")
	write("auth/roles.json", "auth-roles")
	write("lsa/tenant1.lsa", "lsa-tenant1")
	mkdirEmpty("lsa/emptydir")
	write("edgestore/shard10.dat", "edge-shard10")
	write("edgestore/shard2.dat", "edge-shard2")
	write("edgestore/nested/shard1.dat", "edge-nested-shard1")
}

// decodedArchive is one archive's tar members, decoded and kept in the order
// they appeared in the stream, with the manifest parsed out separately.
type decodedArchive struct {
	order    []string
	content  map[string][]byte
	manifest backup.Manifest
}

func decodeArchive(t *testing.T, data []byte) decodedArchive {
	t.Helper()
	gz, err := gzip.NewReader(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decodeArchive: gzip: %v", err)
	}
	defer func() { _ = gz.Close() }()

	da := decodedArchive{content: map[string][]byte{}}
	sawManifest := false
	tr := tar.NewReader(gz)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("decodeArchive: tar: %v", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			t.Fatalf("decodeArchive: read %s: %v", hdr.Name, err)
		}
		if hdr.Name == backup.ManifestName {
			if err := json.Unmarshal(b, &da.manifest); err != nil {
				t.Fatalf("decodeArchive: manifest: %v", err)
			}
			sawManifest = true
			continue
		}
		da.order = append(da.order, hdr.Name)
		da.content[hdr.Name] = b
	}
	if !sawManifest {
		t.Fatalf("decodeArchive: no %s in the stream, archive is malformed", backup.ManifestName)
	}
	return da
}

// diffArchives reports every way two archives differ: member order, member
// set, per-member content, and manifest fields.
//
// CreatedAtUTC is excluded on purpose. It is time.Now().UTC() at the moment
// WriteArchive ran, a property the format had before this change and one
// this change does not touch. Two archives built from the same input at two
// different instants can never agree there, so including it would make
// every equivalence assertion fail regardless of whether the code is
// correct.
func diffArchives(a, b decodedArchive) []string {
	var diffs []string
	if !reflect.DeepEqual(a.order, b.order) {
		diffs = append(diffs, fmt.Sprintf("member order differs:\n  a: %v\n  b: %v", a.order, b.order))
	}
	seen := make(map[string]bool, len(a.content))
	for name, ab := range a.content {
		seen[name] = true
		bb, ok := b.content[name]
		if !ok {
			diffs = append(diffs, fmt.Sprintf("%s: present in a, absent from b", name))
			continue
		}
		if !bytes.Equal(ab, bb) {
			diffs = append(diffs, fmt.Sprintf("%s: content differs (%d bytes vs %d bytes)", name, len(ab), len(bb)))
		}
	}
	for name := range b.content {
		if !seen[name] {
			diffs = append(diffs, fmt.Sprintf("%s: present in b, absent from a", name))
		}
	}
	am, bm := a.manifest, b.manifest
	am.CreatedAtUTC, bm.CreatedAtUTC = "", ""
	if !reflect.DeepEqual(am, bm) {
		diffs = append(diffs, fmt.Sprintf("manifest differs (CreatedAtUTC excluded):\n  a: %+v\n  b: %+v", am, bm))
	}
	return diffs
}

func buildArchiveBytes(t *testing.T, fsys vfs.FileSystem, dir string) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := backup.WriteArchiveWithFS(fsys, &buf, dir, "golden-version"); err != nil {
		t.Fatalf("WriteArchiveWithFS: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("WriteArchiveWithFS produced an empty archive, this test proves nothing")
	}
	return buf.Bytes()
}

// The control for every equivalence assertion in this file: diffArchives
// must be ABLE to report a difference, or a passing "archives are
// equivalent" test proves nothing. An equivalence check that always reports
// "no difference" — because it is comparing the wrong fields, or because
// both inputs collapsed to empty — is indistinguishable from a working one
// until it is shown a case that must fail.
func TestDiffArchives_ReportsADifferenceForDifferentInput(t *testing.T) {
	dirA := t.TempDir()
	buildVFSFixture(t, dirA)
	dirB := t.TempDir()
	buildVFSFixture(t, dirB)
	// Perturb one file's content in B only, so B's archive disagrees with A's
	// on both the content bytes and the manifest's recorded hash.
	perturbed := filepath.Join(dirB, "wal", "a.log")
	if err := os.WriteFile(perturbed, []byte("DELIBERATELY-DIFFERENT-CONTENT"), 0o600); err != nil {
		t.Fatal(err)
	}

	a := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dirA))
	b := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dirB))
	if len(a.order) == 0 || len(b.order) == 0 {
		t.Fatal("a fixture archive is empty, this test proves nothing")
	}

	diffs := diffArchives(a, b)
	if len(diffs) == 0 {
		t.Fatal("diffArchives reported no difference between two archives built from " +
			"deliberately different input — the comparison cannot tell an identical " +
			"pair from a broken check, so a passing equivalence test elsewhere in " +
			"this file would prove nothing")
	}
}

// The positive counterpart: the same input, archived twice, must diff clean.
// Without this, the control above only shows the instrument can say "yes";
// this shows it can also correctly say "no".
func TestDiffArchives_ReportsNoDifferenceForTheSameInput(t *testing.T) {
	dir := t.TempDir()
	buildVFSFixture(t, dir)

	a := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dir))
	b := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dir))
	if len(a.order) == 0 || len(b.order) == 0 {
		t.Fatal("a fixture archive is empty, this test proves nothing")
	}
	if diffs := diffArchives(a, b); len(diffs) != 0 {
		t.Fatalf("two archives of the same input differ:\n%v", diffs)
	}
}

// WriteArchiveWithFS must produce byte-for-byte the same members, in the
// same order, with the same manifest (CreatedAtUTC aside) as the pre-change
// implementation did for the identical input. This is the test that proves
// the vfs seam changed WHERE the bytes come from and not WHAT they are —
// per docs/STABILITY_POLICY.md, the backup archive format is
// customer-data-equivalent and may not change without a version bump.
func TestWriteArchiveWithFS_MatchesPreChangeFormat(t *testing.T) {
	dir := t.TempDir()
	buildVFSFixture(t, dir)

	got := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dir))
	if len(got.order) == 0 {
		t.Fatal("archive has no non-manifest members, this test proves nothing")
	}

	if want := goldenOrder(); !reflect.DeepEqual(got.order, want) {
		t.Fatalf("member order = %v, want (pre-change) %v", got.order, want)
	}
	for _, g := range preChangeGolden {
		b, ok := got.content[g.path]
		if !ok {
			t.Errorf("%s: missing from the new archive (present pre-change)", g.path)
			continue
		}
		if int64(len(b)) != g.size {
			t.Errorf("%s: size = %d, want (pre-change) %d", g.path, len(b), g.size)
		}
		if sum := sha256Hex(b); sum != g.sha256 {
			t.Errorf("%s: sha256 = %s, want (pre-change) %s", g.path, sum, g.sha256)
		}
	}
	if got.manifest.ManifestVersion != 1 || got.manifest.GraphdbVersion != "golden-version" ||
		got.manifest.SnapshotMode != "json" || len(got.manifest.Files) != len(preChangeGolden) {
		t.Errorf("manifest fields (other than CreatedAtUTC) do not match pre-change: %+v", got.manifest)
	}
}

// The walk must sort explicitly. A driver whose ReadDir returns entries in
// the opposite order from the OS driver must still produce the pre-change
// LEXICAL order, and that order must be stable across repeated runs.
//
// vfstest's Handles, Roles and Faults all forward ReadDir's result
// unchanged — none of them can stand in for a driver with different
// ordering, so this file defines a minimal one purpose-built for this test.
type reverseReadDirFS struct {
	vfs.FileSystem
}

func (r reverseReadDirFS) ReadDir(name string) ([]os.DirEntry, error) {
	entries, err := r.FileSystem.ReadDir(name)
	if err != nil {
		return nil, err
	}
	reversed := make([]os.DirEntry, len(entries))
	for i, e := range entries {
		reversed[len(entries)-1-i] = e
	}
	return reversed, nil
}

func TestWriteArchiveWithFS_OrderIsStableAndLexicalRegardlessOfDriverOrder(t *testing.T) {
	dir := t.TempDir()
	buildVFSFixture(t, dir)
	want := goldenOrder()

	plain := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dir))
	if len(plain.order) == 0 {
		t.Fatal("archive is empty, this test proves nothing")
	}
	if !reflect.DeepEqual(plain.order, want) {
		t.Fatalf("plain-driver order = %v, want %v", plain.order, want)
	}

	reversed := reverseReadDirFS{vfs.OS()}
	for i := 0; i < 4; i++ {
		got := decodeArchive(t, buildArchiveBytes(t, reversed, dir))
		if len(got.order) == 0 {
			t.Fatal("archive is empty, this test proves nothing")
		}
		if !reflect.DeepEqual(got.order, want) {
			t.Fatalf("run %d: a driver returning ReadDir entries in reverse order produced "+
				"archive order %v, want the same lexical order %v every driver must — "+
				"the walk is depending on driver order instead of sorting explicitly",
				i, got.order, want)
		}
	}
}

// WriteArchiveWithFS must read through the supplied driver, not bypass it to
// the os package.
func TestWriteArchiveWithFS_ReadsThroughTheSuppliedDriver(t *testing.T) {
	dir := t.TempDir()
	buildVFSFixture(t, dir)

	handles := vfstest.NewHandles(vfs.OS())
	var buf bytes.Buffer
	if err := backup.WriteArchiveWithFS(handles, &buf, dir, "v"); err != nil {
		t.Fatalf("WriteArchiveWithFS: %v", err)
	}
	// The control: without asserting Opened() > 0, an implementation that
	// never touched the driver at all and one that correctly archived an
	// empty store would both leave Outstanding() empty, and this assertion
	// would mean nothing either way.
	if handles.Opened() == 0 {
		t.Fatal("the driver observed zero Open calls while archiving a non-empty data " +
			"directory, so WriteArchiveWithFS did not read through the supplied driver")
	}
	if got := handles.Outstanding(); len(got) != 0 {
		t.Errorf("WriteArchiveWithFS left handles open: %v", got)
	}

	// Strong form: a driver that refuses every open must fail the write. An
	// implementation that fell back to the os package for reads would still
	// succeed here, because the refusal never reaches the code path that
	// actually touches the disk.
	faults := vfstest.NewFaults(vfs.OS(), "write-archive-refuses-open")
	faults.FailOpen(vfstest.Always)
	var buf2 bytes.Buffer
	err := backup.WriteArchiveWithFS(faults, &buf2, dir, "v")
	if err == nil {
		t.Fatal("WriteArchiveWithFS succeeded while the driver refused every open, so it " +
			"read the archived files from somewhere other than the supplied driver")
	}
	if !errors.Is(err, vfstest.ErrInjected) {
		t.Errorf("WriteArchiveWithFS failed for a reason other than the driver's refusal: %v", err)
	}
}

// WriteArchive (the pre-existing, unchanged-signature entry point) must keep
// delegating through the default driver and produce output identical to
// calling WriteArchiveWithFS(vfs.OS(), ...) directly.
func TestWriteArchive_StillDelegatesThroughTheDefaultDriver(t *testing.T) {
	dir := t.TempDir()
	buildVFSFixture(t, dir)

	var viaDefault bytes.Buffer
	if err := backup.WriteArchive(&viaDefault, dir, "golden-version"); err != nil {
		t.Fatalf("WriteArchive: %v", err)
	}
	if viaDefault.Len() == 0 {
		t.Fatal("WriteArchive produced an empty archive, this test proves nothing")
	}

	a := decodeArchive(t, viaDefault.Bytes())
	b := decodeArchive(t, buildArchiveBytes(t, vfs.OS(), dir))
	if diffs := diffArchives(a, b); len(diffs) != 0 {
		t.Fatalf("WriteArchive != WriteArchiveWithFS(vfs.OS(), ...):\n%v", diffs)
	}
}
