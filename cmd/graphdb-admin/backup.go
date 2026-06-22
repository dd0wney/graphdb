package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dd0wney/graphdb/pkg/backup"
)

// targetSnapshotMode reports the snapshot mode the server would use for the
// restored data directory, mirroring cmd/server's GRAPHDB_STORAGE_MODE check.
// A backup's snapshot file is mode-specific: restoring an mmap snapshot into a
// JSON-mode server (or vice versa) silently loads an empty graph, so restore
// refuses a mismatch up front.
func targetSnapshotMode() string {
	if os.Getenv("GRAPHDB_STORAGE_MODE") == "mmap" {
		return "mmap"
	}
	return "json"
}

// handleBackupCommand implements `graphdb-admin backup <subcommand>`. Backup
// archives are produced by the server's POST /admin/backup endpoint; this
// command provides the offline operator tooling to inspect them.
func handleBackupCommand(args []string) {
	if len(args) < 1 {
		printBackupUsage()
		os.Exit(1)
	}
	sub, rest := args[0], args[1:]
	switch sub {
	case "verify":
		if err := runBackupVerify(rest); err != nil {
			fmt.Fprintf(os.Stderr, "verify failed: %v\n", err)
			os.Exit(1)
		}
	case "restore":
		if err := runBackupRestore(rest); err != nil {
			fmt.Fprintf(os.Stderr, "restore failed: %v\n", err)
			os.Exit(1)
		}
	case "help", "--help", "-h":
		printBackupUsage()
	default:
		fmt.Fprintf(os.Stderr, "Unknown backup subcommand: %s\n\n", sub)
		printBackupUsage()
		os.Exit(1)
	}
}

// runBackupVerify checks a backup archive's integrity against its manifest. It
// returns an error (rather than exiting) so it is unit-testable.
func runBackupVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: graphdb-admin backup verify <archive.tar.gz>")
	}
	path := fs.Arg(0)
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer f.Close()

	man, err := backup.Verify(f)
	if err != nil {
		return err
	}
	fmt.Printf("✅ %s verified OK\n", path)
	fmt.Printf("   graphdb version: %s\n", man.GraphdbVersion)
	fmt.Printf("   created (UTC):   %s\n", man.CreatedAtUTC)
	fmt.Printf("   snapshot mode:   %s\n", man.SnapshotMode)
	fmt.Printf("   files:           %d\n", len(man.Files))
	return nil
}

// runBackupRestore validates a backup archive and extracts it into a data
// directory. It verifies integrity and snapshot-mode compatibility BEFORE
// writing anything, so a bad archive or a mode mismatch can never half-restore
// over the target. It returns an error rather than exiting so it is testable.
func runBackupRestore(args []string) error {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	into := fs.String("into", "", "target data directory to restore into (required)")
	dryRun := fs.Bool("dry-run", false, "validate and report only; do not write")
	force := fs.Bool("force", false, "allow restoring into a non-empty directory")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if *into == "" {
		return fmt.Errorf("usage: graphdb-admin backup restore --into <dataDir> [--dry-run] [--force] <archive.tar.gz>")
	}
	if fs.NArg() < 1 {
		return fmt.Errorf("usage: graphdb-admin backup restore --into <dataDir> [--dry-run] [--force] <archive.tar.gz>")
	}
	archivePath := fs.Arg(0)

	// 1. Verify integrity before touching the target.
	vf, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	man, verr := backup.Verify(vf)
	vf.Close()
	if verr != nil {
		return verr
	}

	// 2. Snapshot-mode compatibility (a "none" archive has no snapshot to clash).
	target := targetSnapshotMode()
	if man.SnapshotMode != "none" && man.SnapshotMode != target {
		return fmt.Errorf("snapshot mode mismatch: archive is %q but this environment restores in %q mode "+
			"(set GRAPHDB_STORAGE_MODE=%s to match, or restore a %s-mode archive)",
			man.SnapshotMode, target, man.SnapshotMode, target)
	}

	// 3. Non-empty-target guard.
	if !*force {
		if entries, derr := os.ReadDir(*into); derr == nil && len(entries) > 0 {
			return fmt.Errorf("target %q is not empty; refusing to overwrite without --force", *into)
		}
	}

	fmt.Printf("Archive %s verified OK (graphdb %s, %s mode, %d files).\n",
		archivePath, man.GraphdbVersion, man.SnapshotMode, len(man.Files))

	if *dryRun {
		fmt.Printf("Dry run: would restore %d files into %s. Not writing.\n", len(man.Files), *into)
		return nil
	}

	// 4. Extract (re-open the archive for a clean read).
	xf, err := os.Open(archivePath)
	if err != nil {
		return fmt.Errorf("open archive: %w", err)
	}
	defer xf.Close()
	if err := backup.Extract(xf, *into); err != nil {
		return err
	}
	fmt.Printf("✅ Restored %d files into %s. Start the server to load the snapshot and replay the WAL.\n",
		len(man.Files), *into)
	return nil
}

func printBackupUsage() {
	usage := `GraphDB backup tooling

Usage:
  graphdb-admin backup <subcommand> [options]

Available Subcommands:
  verify <archive.tar.gz>                       Check an archive's integrity against its manifest
  restore --into <dataDir> [--dry-run] [--force] <archive.tar.gz>
                                                Verify, then extract an archive into a data directory

Create a backup with the server endpoint:
  curl -fSL -X POST -H "Authorization: Bearer $TOKEN" https://host/admin/backup -o backup.tar.gz

See docs/BACKUP_RESTORE.md for the full backup & restore runbook.
`
	fmt.Print(usage)
}
