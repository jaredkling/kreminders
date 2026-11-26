package main

import (
	"fmt"
	"io"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	subcmd := os.Args[1]
	switch subcmd {
	case "version":
		fmt.Println("Version: v0.0.1-dev")
	case "help":
		usage(os.Stdout)
	default:
		usage(os.Stderr)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprintln(w, "Kreminders: A simple single user reminder service using golang.")
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Usage:")
	fmt.Fprintln(w, "\tkreminders version - Prints version information")
	fmt.Fprintln(w, "\tkreminders help - Prints this message")
}
