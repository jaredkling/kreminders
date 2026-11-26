# Ticket 01b — Subcommands, version info, and your first test

**Depends on:** 01a
**Effort:** 1 hour
**Part of:** [01 — Bootstrap](01-bootstrap.md)

## Goal

The binary has real subcommands and prints useful build information for
`version`:

```sh
go run ./cmd/kreminders version
# kreminders 0.0.1-dev
# commit:    unknown
# go:        go1.26.10
go run ./cmd/kreminders            # usage, exit 2
```

…and the decision-making part of the program is covered by a table-driven test,
so adding `serve` in 01d doesn't require clicking around manually.

## Why this slice

This is where the program stops being a script: you separate "decide what to do"
from "exit the process", which is the seam every later ticket relies on. It's
also a gentle introduction to interfaces (`io.Writer`) and to `testing`.

## What you'll learn

- Functions with multiple return values, and returning an exit status
- The `io.Writer` interface, and why tests like it more than `os.Stdout`
- Writing tests with no global state: `bytes.Buffer`, `t.Run`, table cases
- Constants vs package-level `var`s, and `-ldflags -X`
- `runtime.Version()`

## Steps

1. Move the body of `main` into a function that takes the arguments (excluding
   `os.Args[0]`) and writers for normal and error output, and returns an exit
   code. `main` becomes: call it, then exit.
2. Add `version` printing three lines. Keep the values in package-level `var`s
   so a build can override them, and use `runtime.Version()` for the Go line.
3. Write `cmd/kreminders/main_test.go` with a table of cases: `version`, no
   args, unknown arg, `version extra` (decide and document the behaviour).
   Assert both the output and the returned exit code.
4. Try overriding one value at build time with `-ldflags -X` in a scratch build.
   You don't have to keep the flag anywhere; just see it work.
5. `go test ./... -race`, `go vet ./...`, `gofmt -l .`, commit.

## Acceptance criteria

- [ ] `version` prints version, commit and Go version, each on its own line.
- [ ] No args and unknown args print usage to the error writer and return 2.
- [ ] `go test ./cmd/kreminders -v` shows your named cases passing.
- [ ] The test does not touch the real stdout/stderr and does not call
      `os.Exit`.
- [ ] `go run ./cmd/kreminders version | wc -l` prints 3.
- [ ] `gofmt -l .`, `go vet ./...`, `go test ./... -race -count=1` all clean.
- [ ] Committed as one slice.

## Questions to answer

- Why must `os.Exit` stay in `main` and out of the dispatch function? What does
  that buy the test?
- Returning an `int` exit code versus an `error`: which one will you want in
  01d when binding a port fails, and why?
- Where should the usage text live so that adding `serve` doesn't mean editing
  it in two places?
- What is the difference between `var version = ...` and
  `const version = ...` when you later use `-ldflags -X`?
- Is `version extra` an error or an ignored argument? Whichever you pick, the
  test is the documentation.

## Pitfalls

- Capturing the terminal by swapping `os.Stdout` in a test. It works on Linux,
  is racy under `-race`, and is a symptom the seam is in the wrong place.
- Asserting exact multi-line strings including trailing newlines and then
  fighting whitespace. `strings.Contains` per line is usually enough here.
- `%v` printing an empty commit as a blank line; decide what "unknown" looks
  like before writing the test.
- Using `fmt.Println` for errors; `fmt.Fprintln(os.Stderr, ...)` or your error
  writer is the point.
- Letting the `version` subcommand grow flags. Not needed, now or later.

## When you're stuck

If passing writers around feels awkward, write the test first with
`bytes.Buffer` and let the test dictate the signature. That's not cheating —
it's how most of Go is designed. If `-ldflags -X` doesn't take effect, check the
variable is a package-level `var` of type `string`, and that you're passing the
*same* package path (`main.version`) that the linker sees.
