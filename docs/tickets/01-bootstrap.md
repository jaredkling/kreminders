# Ticket 01 — Bootstrap

**Depends on:** [00 — Go primer](00-go-primer.md)
**Effort:** roughly five sessions, one per slice
**Slices:** [01a](01a-hello-binary.md) · [01b](01b-subcommands-and-tests.md) · [01c](01c-configuration.md) · [01d](01d-http-and-healthz.md) · [01e](01e-logging-and-shutdown.md)

## What this ticket delivers, as a whole

By the end, `kreminders` builds and runs. `version` prints build information.
`serve` loads and validates its entire configuration, answers `GET /healthz` on
`127.0.0.1:5736`, logs what it's doing, and shuts down cleanly. Nothing is
stored and nothing is sent — that starts in ticket 02.

## Why it's split

This is your first Go program, and the original single ticket asked you to learn
packages, argument handling, environment parsing, HTTP handlers, `httptest`,
structured logging and signals at the same time. Each slice below introduces one
idea group and ends in something you can run, test and commit.

| Slice | You'll have | New ideas | Rough time |
|---|---|---|---|
| [01a](01a-hello-binary.md) | A binary that prints a version and usage | packages, `main`, `os.Args`, exit codes, `go run`/`go build` | 30–45 min |
| [01b](01b-subcommands-and-tests.md) | Real subcommands, build info, first test | functions returning values, `io.Writer`, table tests, `-ldflags` | ~1 hour |
| [01c](01c-configuration.md) | A validated config package (4 commits) | structs, env lookup, file reads, parsing, `errors.Join`, `t.Setenv` | 1–2 hours |
| [01d](01d-http-and-healthz.md) | `serve` with `/healthz` on loopback | `net/http`, mux patterns, JSON, `httptest`, server timeouts | 1–2 hours |
| [01e](01e-logging-and-shutdown.md) | Structured logs, clean SIGINT/SIGTERM, `run() error` | `slog`, contexts, `signal.NotifyContext`, graceful shutdown | 1–2 hours |

## Working rules

- **One slice per branch commit-set.** Stay on `01-bootstrap`; commit at the end
  of each slice (01c suggests four commits — smaller is better there).
- **Green before commit:** `gofmt -l .` silent, `go vet ./...` clean,
  `go test ./... -race -count=1` passing.
- **If a slice takes more than one sitting, stop and tell me.** A slice that
  doesn't fit in a session usually means a missing concept, not missing effort,
  and it's cheaper to clear it up than to grind.
- **Ask for review with the diff** and one line of what you tried. I'll compare
  it to the acceptance lists and point at gaps, not write it for you.
- **Update the docs if they're wrong.** If a config default or a route turns out
  to be silly in practice, change `design.md` in the same commit and say why.

## Combined acceptance

Everything in the slices' checkboxes, plus these end-to-end properties:

- [ ] Fresh clone + `go run ./cmd/kreminders version` works with no setup.
- [ ] `KREM_BASE_URL=https://example.test go run ./cmd/kreminders serve` →
      `curl -s localhost:5736/healthz | jq` returns `ok: true`.
- [ ] `ss -ltnp | grep 5736` shows loopback only.
- [ ] Ctrl-C exits 0 within a second and logs it.
- [ ] Unset `KREM_BASE_URL` exits 1 with one readable line and no listener.
- [ ] No secret appears in logs; `grep -ri` for your test token across captured
      output finds nothing.
- [ ] `go test ./... -race -count=1`, `go vet ./...`, `gofmt -l .` all clean.

## Deliberately not here

- Any SQLite, scheduling, Gotify or template code. Tempting to "just start
  ticket 02 too" — resist; a clean 01 makes 02's failures unambiguous.
- CI. Add the workflow at the very end of 01e, when `go vet ./...` has packages
  to vet. Commands:
  `gofmt -l .`, `go vet ./...`, `go test ./... -race -count=1`,
  `go build ./...`.
- `/healthz` reporting the database and scheduler heartbeat; ticket 07 extends
  the same handler.
