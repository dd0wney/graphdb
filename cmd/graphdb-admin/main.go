package main

import (
	"fmt"
	"os"
)

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
	case "help", "--help", "-h":
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
  help        Show this help message
  version     Show version information

Use "graphdb-admin <command> --help" for more information about a command.
`
	fmt.Print(usage)
}

func printVersion() {
	fmt.Println("GraphDB Admin CLI v1.0.0")
	fmt.Println("Build: 2025-11-23")
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
