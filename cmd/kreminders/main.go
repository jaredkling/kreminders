package main

import (
	"fmt"
	"io"
	"os"
	"runtime"
)

// version and commit are placeholders that a build can override:
//
//	go build -ldflags "-X main.version=0.1.0 -X main.commit=$(git rev-parse --short HEAD)" \
//	  -o /tmp/kreminders ./cmd/kreminders
//
// -X only works on package-level string variables, and it fails silently if the
// symbol does not exist or is not a string.
var (
	version = "0.0.0-dev"
	commit  = "unknown"
)

func main() {
	os.Exit(handleStartup(os.Args[1:], os.Stdout, os.Stderr))
}

// handleStartup dispatches the subcommand and returns the process exit code.
// It is the only place that knows which argument means what; it knows nothing
// about the real stdout/stderr, which is what makes it testable.
func handleStartup(args []string, output, diagnostics io.Writer) int {
	if len(args) == 0 {
		usage(diagnostics)
		return 2
	}

	switch args[0] {
	case "version":
		printVersion(output)
	case "help":
		usage(output)
	default:
		usage(diagnostics)
		return 2
	}

	return 0
}

func printVersion(w io.Writer) {
	fmt.Fprintf(w, "kreminders:\t%s\n", version)
	fmt.Fprintf(w, "commit:\t%s\n", commit)
	fmt.Fprintf(w, "go:\t%s\n", runtime.Version())
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Kreminders: A simple single user reminder service using golang.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "\tkreminders version - Prints version information")
	fmt.Fprintln(w, "\tkreminders help - Prints this message")
}
