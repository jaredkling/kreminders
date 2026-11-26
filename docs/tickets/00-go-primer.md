# Ticket 00 — Go primer

**Depends on:** nothing
**Effort:** read once, refer back
**Read first:** nothing; this is the on-ramp

## Goal

Enough mental model of Go and its toolchain that the first ticket doesn't feel
like guessing. This ticket has no acceptance criteria and no code to write.
Read it, then start [`01a`](01a-hello-binary.md).

## The mental model

- **Module** = this repository. `go.mod` declares its path
  (`kling.dev/kreminders`) and the Go version. One module, many packages.
- **Package** = one directory. Every `.go` file in a directory starts with
  `package <name>`, and all files in the directory share that package.
- **`package main` + `func main()`** makes a directory an executable. Everything
  else is a library package that other packages import.
- **Import paths** are module path + directory: this repo's
  `internal/config` is imported as `kling.dev/kreminders/internal/config`.
- **`internal/`** is special: only code inside this module can import it. That's
  why the layout puts everything there except `cmd/`.
- **Capitalisation is visibility.** `Config` is exported (usable from other
  packages); `baseURL` is not. Same for fields and methods. If you get
  "undefined: x", check the capital letter first.
- **Errors are values.** Functions return `(result, error)` and callers check
  `if err != nil`. There are no exceptions. `errors.Is` asks "is this error (or
  something it wraps) this specific value?".
- **Zero values.** A declared-but-unassigned variable is `""`, `0`, `false`,
  `nil` — never garbage. This is why structs are often usable without a
  constructor.

## The loop

```
edit ──► go run ./cmd/kreminders ...   (does it behave?)
     ──► go test ./... -race -count=1  (does it still behave for everything else?)
     ──► gofmt -l .                    (must print nothing)
     ──► go vet ./...                  (suspicious code the compiler allows)
     ──► git commit -m "..."           (checkpoint)
```

Useful extras:

```sh
go doc fmt.Errorf                 # offline docs for a symbol
go doc -all kling.dev/kreminders/internal/config
go test ./internal/config -run TestLoad -v
go build -o /tmp/kreminders ./cmd/kreminders && /tmp/kreminders version
go vet ./...                      # ignores tests? no, includes them
```

`go test` caches results. When a test result makes no sense, add `-count=1` to
force a rerun before believing anything.

## Reading compiler errors

Go's errors are usually literal. The ones you'll meet first:

| Message | Usual cause |
|---|---|
| `undefined: foo` | typo, wrong package, or `foo` is unexported in another package |
| `declared and not used` | local variable/import with no use; delete it or use `_` |
| `imported and not used` | same for imports |
| `no required module provides package X` | missing `go get`, or a wrong import path |
| `cannot use x (variable of type A) as B` | wrong type; convert explicitly, don't hope |
| `assignment mismatch: 2 variables but 1 value` | function returns two values; you're assigning one |
| `no new variables on left side of :=` | `:=` needs at least one new name; use `=` |
| `missing return` | a branch doesn't return in a function that promises a value |
| `multiple-value X() in single-value context` | you passed an `(T, error)` result where one value fits |
| `unused parameter` is *not* an error | unused **parameters** are fine; unused locals are not |

If an error mentions a file and line you didn't write, it's probably an
interface you're failing to satisfy — read the `cannot use ... as ...` part
backwards.

## Testing, briefly

- Tests live in `*_test.go` files next to the code, in the same package
  (`package config`) so they can see unexported things. Use
  `package config_test` when you want to test only the public API.
- `t.Run("name", func(t *testing.T) {...})` gives named subtests and lets one
  table drive many cases.
- `t.Setenv("KREM_X", "y")` sets an env var for one test and restores it after.
- `t.TempDir()` gives a directory that's deleted afterwards — use it instead of
  `/tmp/whatever`.
- `httptest.NewRequest` / `httptest.NewRecorder` test handlers without opening a
  port; `httptest.NewServer` when you need a real listener.
- A test that sleeps is usually a sign you haven't found the seam yet.

## Working habits for this project

- One slice per commit. A slice is "smaller than a ticket"
  (see [`01a`](01a-hello-binary.md)–[`01e`](01e-logging-and-shutdown.md)).
- Before committing: `gofmt -l .` clean, `go vet ./...` clean, `go test ./...`
  green. If a check fails, fix or delete the experiment — don't commit around it.
- Keep `NOTES.md` (gitignored or committed, your choice) with surprises: "nil
  pointer in X", "forgot to close Y". Future you will use it in ticket 06.
- When stuck for more than ~30 minutes: write down (a) what you expected,
  (b) what happened, (c) the smallest example that shows it. Half the time that
  answers it; the other half is a perfect question to bring back here.
- Ask for review with a `git show` or a diff plus what you tried. "Review my
  code" is enough context; I'll compare it to the ticket's acceptance list.

## What not to worry about yet

Generics, reflection, pointers-to-pointers, `unsafe`, build tags, cgo,
benchmarks, advanced `context` patterns. None of them are needed before
ticket 07, and most never.
