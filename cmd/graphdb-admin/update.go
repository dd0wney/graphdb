package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"

	"github.com/dd0wney/cluso-graphdb/pkg/updater"
)

const (
	defaultManifestURL = "https://raw.githubusercontent.com/dd0wney/graphdb/main/releases.json"
)

func handleUpdateCommand(args []string) {
	updateCmd := flag.NewFlagSet("update", flag.ExitOnError)
	channel := updateCmd.String("channel", "stable", "Update channel (stable or beta)")
	manifestURL := updateCmd.String("manifest", defaultManifestURL, "URL to release manifest")
	dryRun := updateCmd.Bool("dry-run", false, "Check for updates without applying")
	force := updateCmd.Bool("force", false, "Force update even if version is current")

	updateCmd.Parse(args)

	currentVersion := "v1.0.0" // This should be injected at build time

	fmt.Printf("Checking for updates (channel: %s)...\n", *channel)
	
	status, err := updater.CheckForUpdates(context.Background(), currentVersion, *channel, *manifestURL)
	if err != nil {
		log.Fatalf("Update check failed: %v", err)
	}

	if !status.UpdateAvailable && !*force {
		fmt.Printf("GraphDB is up to date (current version: %s)\n", currentVersion)
		return
	}

	fmt.Printf("A new version is available: %s\n", status.LatestVersion)
	fmt.Printf("Release Date: %s\n", status.LatestRelease.ReleaseDate.Format("2006-01-02"))
	fmt.Printf("Description: %s\n", status.LatestRelease.Description)

	if *dryRun {
		fmt.Println("Dry run mode: update not applied.")
		return
	}

	// Apply update
	fmt.Println("Downloading update...")
	tempPath := os.TempDir() + "/graphdb-new"
	err = updater.DownloadRelease(context.Background(), status.LatestRelease, tempPath)
	if err != nil {
		log.Fatalf("Download failed: %v", err)
	}

	fmt.Println("Applying update...")
	err = updater.ApplyUpdate(tempPath)
	if err != nil {
		log.Fatalf("Failed to apply update: %v", err)
	}

	fmt.Println("✅ GraphDB Admin CLI successfully updated to", status.LatestVersion)
	fmt.Println("Please restart any running instances of GraphDB.")
}

func printUpdateUsage() {
	usage := `Software update commands

Usage:
  graphdb-admin update [options]

Options:
  --channel NAME    Update channel (default: stable)
  --manifest URL    URL to release manifest
  --dry-run         Check for updates without applying
  --force           Force update even if already up to date
`
	fmt.Print(usage)
}
