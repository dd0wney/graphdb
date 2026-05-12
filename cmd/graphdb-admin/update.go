package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"github.com/dd0wney/cluso-graphdb/pkg/updater"
)

const defaultManifestURL = "https://raw.githubusercontent.com/dd0wney/graphdb/main/releases.json"

// handleUpdateCommand implements `graphdb-admin update`. It checks the
// release manifest, optionally downloads + verifies + atomically swaps
// the running graphdb-admin binary, and exits.
//
// The "current version" is read from updater.Version, which the caller
// (main.go) is expected to set at startup from this binary's Version
// package var (which in turn is set at build time via -ldflags).
func handleUpdateCommand(args []string) {
	fs := flag.NewFlagSet("update", flag.ExitOnError)
	channel := fs.String("channel", "stable", "Update channel (stable, beta, ...)")
	manifestURL := fs.String("manifest", defaultManifestURL, "URL to release manifest")
	dryRun := fs.Bool("dry-run", false, "Check for updates without applying")
	force := fs.Bool("force", false, "Apply update even if already current")
	if err := fs.Parse(args); err != nil {
		log.Fatalf("Failed to parse flags: %v", err)
	}

	ctx := context.Background()
	fmt.Printf("Checking for updates (channel=%s, current=%s)...\n", *channel, updater.Version)

	status, err := updater.CheckForUpdates(ctx, *manifestURL, *channel)
	if err != nil {
		log.Fatalf("Update check failed: %v", err)
	}

	if !status.UpdateAvailable && !*force {
		fmt.Printf("graphdb-admin is up to date (%s).\n", updater.Version)
		return
	}

	fmt.Printf("Update available: %s\n", status.LatestVersion)
	if status.LatestRelease != nil {
		if !status.LatestRelease.ReleaseDate.IsZero() {
			fmt.Printf("Release date: %s\n", status.LatestRelease.ReleaseDate.Format("2006-01-02"))
		}
		if status.LatestRelease.Description != "" {
			fmt.Printf("Notes: %s\n", status.LatestRelease.Description)
		}
	}

	if *dryRun {
		fmt.Println("Dry run: not applying.")
		return
	}

	tmpPath := filepath.Join(os.TempDir(), "graphdb-admin.new")
	fmt.Println("Downloading + verifying...")
	if err := updater.DownloadRelease(ctx, status.LatestRelease, tmpPath); err != nil {
		log.Fatalf("Download failed: %v", err)
	}

	fmt.Println("Applying...")
	if err := updater.ApplyUpdate(tmpPath); err != nil {
		log.Fatalf("Apply failed: %v", err)
	}

	fmt.Printf("✓ graphdb-admin updated to %s.\n", status.LatestVersion)
	fmt.Println("Restart any long-running processes to pick up the new version.")
}

func printUpdateUsage() {
	usage := `Software update commands

Usage:
  graphdb-admin update [options]

Options:
  --channel NAME    Update channel (default: stable)
  --manifest URL    URL to release manifest
  --dry-run         Check for updates without applying
  --force           Apply update even if already current

The current binary's version is set at build time via:
  go build -ldflags "-X main.Version=v1.2.3"

If unset, version is "dev" and every check reports an update available
(intentional for development builds).
`
	fmt.Print(usage)
}
