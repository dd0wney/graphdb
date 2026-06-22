package main

import (
	"fmt"
	"os"

	"github.com/dd0wney/graphdb/pkg/updater"
)

// Version is the build version, set at compile time via:
//
//	go build -ldflags "-X main.Version=v1.2.3"
//
// Defaults to "dev" for unset builds. The init() below forwards this to
// pkg/updater so that `graphdb-admin update` reports the correct
// running version when checking the release manifest.
var Version = "dev"

func init() {
	updater.Version = Version
}

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	command := os.Args[1]

	switch command {
	case "security":
		if len(os.Args) < 3 {
			printSecurityUsage()
			os.Exit(1)
		}
		handleSecurityCommand(os.Args[2:])
	case "update":
		handleUpdateCommand(os.Args[2:])
	case "backup":
		handleBackupCommand(os.Args[2:])
	case "login":
		handleLoginCommand(os.Args[2:])
	case "mint-token":
		handleMintTokenCommand(os.Args[2:])
	case "help", "--help", "-h":
		if len(os.Args) >= 3 && os.Args[2] == "update" {
			printUpdateUsage()
			return
		}
		if len(os.Args) >= 3 && (os.Args[2] == "login" || os.Args[2] == "mint-token") {
			printAuthUsage()
			return
		}
		printUsage()
	case "version", "--version", "-v":
		printVersion()
	default:
		fmt.Fprintf(os.Stderr, "Unknown command: %s\n\n", command)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	usage := `GraphDB Admin CLI - Administrative tools for GraphDB

Usage:
  graphdb-admin <command> [options]

Available Commands:
  security    Security management commands
  update      Check for and apply software updates
  backup      Backup archive tooling (verify)
  login       Log in to a running server and print an access token
  mint-token  Mint a JWT offline from JWT_SECRET
  help        Show this help message
  version     Show version information

Use "graphdb-admin <command> --help" for more information about a command.
`
	fmt.Print(usage)
}

func printVersion() {
	fmt.Printf("GraphDB Admin CLI %s\n", Version)
}

func printSecurityUsage() {
	usage := `Security management commands

Usage:
  graphdb-admin security <subcommand> [options]

Available Subcommands:
  init              Initialize security features
  rotate-keys       Rotate encryption keys
  audit-export      Export audit logs
  health            Check security health status
  key-info          Display encryption key information

Global Flags:
  --server-url URL  GraphDB server URL (default: http://localhost:8080)
  --api-key KEY     API key for authentication
  --token TOKEN     JWT token for authentication

Examples:
  # Initialize security features
  graphdb-admin security init

  # Rotate encryption keys
  graphdb-admin security rotate-keys --token=<jwt-token>

  # Export audit logs
  graphdb-admin security audit-export --output=audit.json

  # Check security health
  graphdb-admin security health --server-url=http://localhost:8080
`
	fmt.Print(usage)
}
