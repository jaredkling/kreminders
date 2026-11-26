# Ticket 01 — Bootstrap

**Depends on:** nothing
**Effort:** one evening
**Read first:** [`../design.md`](../design.md) §3 (architecture), §10 (configuration), §10 subcommands

## Goal

`kreminders` builds and runs. `kreminders version` prints build information.
`kreminders serve` loads and validates its entire configuration, starts an HTTP
server on `127.0.0.1:5736`, answers `GET /healthz`, and shuts down cleanly on
Ctrl-C. Nothing is stored, nothing is sent.

## Why this first

Every later ticket adds a package that needs configuration and a running
process. Getting the entry point, logging, and shutdown right now means you
never have to redo them. It also teaches the Go idioms you'll use in every file
after this one.

## What you'll learn

| Concept | Where to look |
|---|---|
| Modules, packages, `internal/` | `go help modules`, [Organizing a Go module](https://go.dev/doc/modules/layout) |
| Short variable declarations, multiple returns, `error` | [A Tour of Go](https://go.dev/tour/), Effective Go |
| `os.LookupEnv` vs `os.Getenv`, `os.ReadFile` | `go doc os.LookupEnv` |
| Structured logging | `go doc log/slog`, [slog guide](https://go.dev/blog/slog) |
| HTTP server and handler | `go doc net/http.Server`, `go doc net/http.ServeMux` |
| Graceful shutdown and signals | `go doc os/signal.NotifyContext`, `go doc net/http.Server.Shutdown` |
| Table-driven tests | [Go testing tutorial](https://go.dev/doc/tutorial/add-a-test) |

## Scope

**In**

- A `cmd/kreminders/main.go` that dispatches subcommands and nothing else.
- `internal/config`: load every variable in `design.md` §10, including the
  `_FILE` variants, with defaults, and validate them eagerly.
- `version` and `serve` subcommands. (`migrate` and `test-notify` come later.)
- `slog` JSON or text logging with a `KREM_LOG_LEVEL` knob.
- `GET /healthz` returning JSON with at least `ok`, `version`, `time`.
- Graceful shutdown with a bounded timeout.
- A placeholder for every other route that will exist later (`/`, `/c/`,
  `/mcp`) — 404 or 501 is fine, just decide and be consistent.

**Out**

- Any database, scheduler, or Gotify code.
- Templates or styling.
- Nix packaging (ticket 10).

## Acceptance criteria

```sh
go run ./cmd/kreminders version        # prints version, commit (may be "unknown"), go version
go run ./cmd/kreminders serve          # fails with a clear message: KREM_BASE_URL unset
```

- [ ] With `KREM_BASE_URL=https://example.test`, the process starts, logs one
      line naming the listen address, and `curl -s localhost:5736/healthz | jq`
      returns `ok: true`.
- [ ] `ss -ltnp | grep 5736` shows `127.0.0.1` only — not `0.0.0.0` or `[::]`.
- [ ] Ctrl-C produces a log line and exit status 0 within a second.
- [ ] `KREM_BASE_URL=https://example.test/` (trailing slash) is rejected with a
      message that says why.
- [ ] `KREM_SIGNING_KEY_FILE` pointing at a file containing 64 hex characters is
      accepted; a file with 10 bytes is rejected. A trailing newline in the file
      must be tolerated (editors add one).
- [ ] Unset `KREM_MCP_TOKEN` produces a warning, not a failure.
- [ ] Unknown `KREM_LOG_LEVEL` values are rejected at startup.
- [ ] `gofmt -l .` prints nothing, `go vet ./...` passes, `go test ./...` passes.

## Suggested approach

1. Create the package skeleton from the layout in the README. Write the smallest
   possible `main` that only parses `os.Args[1]` and prints usage for anything
   unknown. Decide now how you feel about `flag` vs a plain `switch` — both are
   fine; pick one and be consistent.
2. Write `config.Load()` returning a struct (or pointer) and an `error`. Keep
   validation *inside* loading so an invalid process never starts. Look up how
   `fmt.Errorf` wrapping with `%w` lets you report several problems at once; a
   `[]error` joined at the end reads much better than failing on the first.
3. Add the `_FILE` handling as a helper that answers "give me the value of X,
   preferring the file". Notice how both branches look the same to callers.
4. Rewrite `main` to log the config *without secrets* on startup — decide which
   fields are safe to print.
5. Add the HTTP server. Give it a mux, at least one handler, and set the
   timeouts you think matter. Register handlers in one place so later tickets
   only add lines there.
6. Wire shutdown. Think about what needs to stop in what order once you add a
   ticker in ticket 07, and leave the shape ready for it.
7. Write tests for config (table-driven, `t.Setenv`, `t.TempDir` for the files)
   and for `/healthz` (`net/http/httptest`). You should not need to bind a port
   to test a handler.

## Questions to answer

- Where should the version string live so that `-ldflags -X` can set it later
  without touching code? Try it once in a scratch build.
- Why does every secret have a `_FILE` variant? (Re-read `ops.md` §4 if you
  forgot — this is an agenix constraint.)
- Is `config` allowed to know about `syscall` signals or `net/http`? Where is the
  seam between "loading configuration" and "running a server"?
- If `KREM_BASE_URL` is required, why is it not checked at the moment it's used
  to build a link? What goes wrong with late validation?
- What does `ServeMux` do with `GET /healthz` versus `POST /healthz`? Which
  pattern syntax are you relying on?

## Pitfalls

- Reading secret files but not trimming whitespace, then comparing 33 bytes.
- Logging the config struct with `%+v` and leaking the Gotify tokens into
  `journalctl`.
- Calling `os.Exit` deep inside a package: the deferred cleanup never runs, and
  tests can't call that code.
- Starting the listener without `ReadHeaderTimeout` or a request size limit;
  you'll add real limits in ticket 08, but the server config belongs here.
- Treating "unset MCP token" as fatal and discovering later that the confirm
  page (ticket 08) is supposed to work without MCP.
- Putting `/healthz` behind any future auth middleware. It's a loopback probe.

## Testing checklist

- [ ] Config: defaults, explicit values, `_FILE` precedence, missing required,
      bad hex/byte length, invalid timezone, invalid log level, invalid quiet
      hours, base URL with trailing slash.
- [ ] Handler: 200 and the expected JSON keys; wrong method is rejected.
- [ ] Shutdown: a test that starts the server on `127.0.0.1:0`, cancels the
      context, and asserts the call returns. Use the listener's actual address,
      not a fixed port.
- [ ] `go test ./... -race` (there's no concurrency yet, but build the habit).

## When you're stuck

Start smaller than the ticket: make `version` work first, commit it, then add
`serve` with a hard-coded address, commit, then move configuration into
`internal/config` one variable at a time, keeping `go test` green. If signals
are fighting you, check the difference between `signal.Notify` and
`signal.NotifyContext` in the docs — one of them removes most of the code.
