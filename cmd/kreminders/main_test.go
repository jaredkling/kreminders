package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestHandleStartup(t *testing.T) {
	tests := []struct {
		name       string
		args       []string
		wantCode   int
		wantStdout []string // substrings that must appear on stdout; nil = must be empty
		wantStderr []string // substrings that must appear on stderr; nil = must be empty
	}{
		{
			name:       "version",
			args:       []string{"version"},
			wantCode:   0,
			wantStdout: []string{"kreminders:\t" + version, "commit:\t" + commit, "go:\tgo"},
		},
		{
			// Extra arguments are ignored; the test documents that choice.
			name:       "version with extra args",
			args:       []string{"version", "extra"},
			wantCode:   0,
			wantStdout: []string{"kreminders:\t" + version},
		},
		{
			name:       "help",
			args:       []string{"help"},
			wantCode:   0,
			wantStdout: []string{"Usage:", "kreminders help"},
		},
		{
			name:       "no arguments",
			args:       nil,
			wantCode:   2,
			wantStderr: []string{"Usage:", "kreminders version"},
		},
		{
			name:       "unknown subcommand",
			args:       []string{"nope"},
			wantCode:   2,
			wantStderr: []string{"Usage:"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stdout, stderr bytes.Buffer

			gotCode := handleStartup(tt.args, &stdout, &stderr)

			if gotCode != tt.wantCode {
				t.Errorf("exit code = %d, want %d", gotCode, tt.wantCode)
			}
			assertStream(t, "stdout", stdout.String(), tt.wantStdout)
			assertStream(t, "stderr", stderr.String(), tt.wantStderr)
		})
	}
}

func TestPrintVersionUsesBuildVariables(t *testing.T) {
	oldVersion, oldCommit := version, commit
	t.Cleanup(func() { version, commit = oldVersion, oldCommit })

	version, commit = "1.2.3", "abc1234"

	var buf bytes.Buffer
	printVersion(&buf)

	for _, want := range []string{
		"kreminders:\t1.2.3",
		"commit:\tabc1234",
		"go:\tgo",
	} {
		if !strings.Contains(buf.String(), want) {
			t.Errorf("output %q does not contain %q", buf.String(), want)
		}
	}

	if got := strings.Count(buf.String(), "\n"); got != 3 {
		t.Errorf("got %d lines, want 3: %q", got, buf.String())
	}
}

// assertStream checks the wanted substrings in order and, when none are wanted,
// requires the stream to be untouched.
func assertStream(t *testing.T, name, got string, want []string) {
	t.Helper()

	if len(want) == 0 {
		if got != "" {
			t.Errorf("%s should be empty, got %q", name, got)
		}
		return
	}

	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("%s = %q, does not contain %q", name, got, w)
		}
	}
}
