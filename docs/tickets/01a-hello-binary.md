# Ticket 01a — Hello, binary

**Depends on:** [00 — Go primer](00-go-primer.md) (read it first)
**Effort:** 30–45 minutes
**Part of:** [01 — Bootstrap](01-bootstrap.md)

## Goal

A real executable at `cmd/kreminders` that answers `version` with a single line
and explains itself (and exits non-zero) for anything else.

```sh
go run ./cmd/kreminders version     # kreminders 0.0.1-dev
go run ./cmd/kreminders             # usage on stderr, exit 2
go run ./cmd/kreminders nope        # same
```

## Why this slice

Before configuration, HTTP or logging, you need to know how a Go program is
shaped: where the entry point lives, how arguments arrive, how a process reports
failure. Everything after this is a variation on these four ideas.

## What you'll learn

- `package main`, `func main`, and why `cmd/<name>/` is the convention
- `os.Args` and how arguments are actually laid out
- `fmt` printing to stdout vs stderr
- Process exit codes and what 0 / 1 / 2 conventionally mean
- `go run` vs `go build`

## Steps

1. Create the directory `cmd/kreminders/` and one file inside it. It must
   declare `package main` and nothing else — note that the file name doesn't
   matter, the directory name does.
2. Print the version line. Run it immediately with `go run ./cmd/kreminders
   version`. The compiler will complain about anything unused; fix by removing,
   not by adding `_` everywhere.
3. Add the fallback: print a short usage text and set a non-zero exit status.
   Decide what "usage" says — it's the first thing a human reads in six months.
4. Run the three commands from the goal and check `echo $?` after each.
5. `go build ./...` then commit.

Keep the whole thing in `main()` for now. Deliberately. Ticket 01b splits it
out when there's a test worth writing.

## Acceptance criteria

- [ ] `go run ./cmd/kreminders version` prints exactly one non-empty line
      containing the word `version` or the version string.
- [ ] `go run ./cmd/kreminders` prints usage to **stderr**. Check the exit code
      with a built binary, not `go run`:
      `go build -o /tmp/kreminders ./cmd/kreminders && /tmp/kreminders >/dev/null; echo $?`
      prints 2. (`go run` is a wrapper: it prints `exit status 2` itself and
      exits 1.)
- [ ] Same for an unknown argument.
- [ ] `go build ./...` succeeds and `gofmt -l .` prints nothing.
- [ ] Committed with a message that says what works, e.g.
      `Add kreminders binary with version and usage`.

## Questions to answer

- Why is the entry point in `cmd/kreminders/` rather than at the repository
  root? What would break (or not) if it were at the root?
- `os.Args[0]` is not the first argument. What is it, and how does that change
  the code that reads `version`?
- Why does usage go to stderr while the version goes to stdout? What does that
  mean for `kreminders version | grep`?
- What's the difference between `go run` and `go build`? Look inside the
  binary's directory after `go build ./...` and find it.

## Pitfalls

- Creating `kreminders.go` at the repository root: it compiles, but it's not
  where the layout says `main` lives, and ticket 02 onwards will feel
  inconsistent.
- Using `log.Fatal` for usage errors: it prints a timestamp and ignores your
  chosen exit code.
- Calling `os.Exit` and then expecting deferred code to run. It won't — which
  matters enormously in ticket 01e.
- Forgetting that Go cares about unused imports: a compile error here is not a
  sign you're doing it wrong.
- Checking exit codes with `go run`: it reports its own failure and exits 1, so
  a program that exits 2 looks like 1. Build a binary when exit status matters.

## When you're stuck

If `go run` says "directory not found", you're probably running it from a
different directory than the module root; check `pwd` and `go env GOMOD`. If it
says "expected 'package'", the file is missing its first line.
