// Command nexus-backup is the operator tool for NEXUS durable-store backup,
// verification and restore (docs/operations-runbook.md).
//
// Usage:
//
//	nexus-backup backup  -data-dir <dir> -out <artifact.tar>
//	nexus-backup verify  -in <artifact.tar>
//	nexus-backup restore -in <artifact.tar> -dest <empty-or-missing-dir>
//
// It is a single narrow utility with three explicit modes — not a general
// command framework. It never prints record contents, credentials or secrets.
// The application must be stopped before a backup (quiescence) and must not be
// using the restore destination.
package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/Nomssky/NEXUS/internal/backup"
	"github.com/Nomssky/NEXUS/internal/foundation/version"
)

func main() {
	os.Exit(run(os.Args[1:]))
}

func run(args []string) int {
	if len(args) == 0 {
		usage()
		return 2
	}
	mode := args[0]
	rest := args[1:]

	switch mode {
	case "backup":
		return cmdBackup(rest)
	case "verify":
		return cmdVerify(rest)
	case "restore":
		return cmdRestore(rest)
	case "-h", "--help", "help":
		usage()
		return 0
	case "-version", "--version", "version":
		fmt.Printf("nexus-backup %s (%s)\n", version.Version, version.Commit)
		return 0
	default:
		fmt.Fprintf(os.Stderr, "nexus-backup: unknown mode %q\n", mode)
		usage()
		return 2
	}
}

func usage() {
	fmt.Fprint(os.Stderr, `nexus-backup — NEXUS durable-store backup/restore

Modes:
  backup  -data-dir <dir> -out <artifact.tar>   create a verified backup
  verify  -in <artifact.tar>                    verify an artifact without mutating anything
  restore -in <artifact.tar> -dest <dir>        restore into an empty/missing directory

Notes:
  * Stop the application before backing up (quiescence); the store writes
    atomically, so a copy taken with no writer active is consistent.
  * The destination must be empty or absent; restore never overwrites data.
  * Backups contain durable store records only, never process-local runtime
    state, filesystem-sandbox files, or secrets.
`)
}

func cmdBackup(args []string) int {
	fs := flag.NewFlagSet("backup", flag.ContinueOnError)
	dataDir := fs.String("data-dir", "", "durable store data directory (source)")
	out := fs.String("out", "", "backup artifact path to create (must not exist)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *dataDir == "" || *out == "" {
		fmt.Fprintln(os.Stderr, "nexus-backup backup: -data-dir and -out are required")
		return 2
	}
	man, err := backup.Create(*dataDir, *out)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus-backup backup: %v\n", err)
		return 1
	}
	fmt.Printf("backup: %d file(s) → %s (format v%d, created %s)\n",
		len(man.Files), *out, man.FormatVersion, man.CreatedAt)
	return 0
}

func cmdVerify(args []string) int {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	in := fs.String("in", "", "backup artifact to verify")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" {
		fmt.Fprintln(os.Stderr, "nexus-backup verify: -in is required")
		return 2
	}
	man, err := backup.Verify(*in)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus-backup verify: %v\n", err)
		return 1
	}
	fmt.Printf("verify: OK — %d file(s), format v%d, created %s\n", len(man.Files), man.FormatVersion, man.CreatedAt)
	return 0
}

func cmdRestore(args []string) int {
	fs := flag.NewFlagSet("restore", flag.ContinueOnError)
	in := fs.String("in", "", "backup artifact to restore")
	dest := fs.String("dest", "", "destination directory (must be empty or absent)")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *in == "" || *dest == "" {
		fmt.Fprintln(os.Stderr, "nexus-backup restore: -in and -dest are required")
		return 2
	}
	man, err := backup.Restore(*in, *dest)
	if err != nil {
		fmt.Fprintf(os.Stderr, "nexus-backup restore: %v\n", err)
		return 1
	}
	fmt.Printf("restore: OK — %d file(s) restored and reopened into %s (format v%d)\n",
		len(man.Files), *dest, man.FormatVersion)
	return 0
}
