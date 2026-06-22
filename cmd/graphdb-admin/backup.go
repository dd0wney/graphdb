package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/dd0wney/graphdb/pkg/backup"
)

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

func printBackupUsage() {
	usage := `GraphDB backup tooling

Usage:
  graphdb-admin backup <subcommand> [options]

Available Subcommands:
  verify <archive.tar.gz>   Check an archive's integrity against its manifest

Create a backup with the server endpoint:
  curl -fSL -X POST -H "Authorization: Bearer $TOKEN" https://host/admin/backup -o backup.tar.gz

See docs/BACKUP_RESTORE.md for the full backup & restore runbook.
`
	fmt.Print(usage)
}
