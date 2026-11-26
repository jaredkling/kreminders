# Ticket 01e — Logging and graceful shutdown

**Depends on:** 01d
**Effort:** 1–2 hours
**Part of:** [01 — Bootstrap](01-bootstrap.md)

## Goal

The service is pleasant to operate: structured logs at a level you can turn
down, one startup line that says what it's doing without revealing secrets, a
single human-readable error if configuration is broken, and Ctrl-C/SIGTERM that
finishes cleanly in well under a second and exits 0.

```sh
KREM_LOG_LEVEL=debug go run ./cmd/kreminders serve
# time=... level=INFO msg="starting" listen=127.0.0.1:5736 base_url=https://... gotify=log-only mcp=disabled
^C
# time=... level=INFO msg="shutting down"
# time=... level=INFO msg="stopped"
```

## Why this slice

This is where `os.Exit` becomes a design decision instead of a habit. The shape
you choose now — a `run() error` at the bottom, exit codes in `main`, a context
that everything stops on — is exactly the shape ticket 07 needs for its ticker.
Getting it right once is cheaper than retrofitting it.

## What you'll learn

- `log/slog`: handlers, levels, key/value attributes
- `context.Context` cancellation and `signal.NotifyContext`
- `http.Server.Shutdown` and what "graceful" actually bounds
- Returning errors from `run()` and converting them to exit codes in `main`
- Deferred cleanup ordering

## Steps

1. Configure a `slog` logger from `KREM_LOG_LEVEL` and use it for the startup
   line. Decide text vs JSON now, and note why (dev readability vs journald
   parsing). Keep the level invalid case an error — 01c already validates it.
2. Replace `serve`'s inline body with the `run() error` shape: `main` calls
   `run`, prints any error once (to stderr, not the logger, before the logger
   exists), exits 1. Server lifecycle moves inside `run`.
3. Create a cancellable context from signals. Start the server, wait for either
   "server failed" or "signal received", then shut down with a timeout and the
   same context for cleanup.
4. Log the effective startup configuration with secrets redacted (or simply
   omitted). Decide what an operator actually needs to know on line one.
5. Test: invalid config exits non-zero without binding; a signal causes a clean
   exit 0 (start the server in a goroutine, send the signal to your own
   process, or better: structure the shutdown so a test can trigger the same
   path without signals — use your judgement and document it in a comment).
6. Manual: Ctrl-C, `kill -TERM <pid>`, and `KREM_BASE_URL=` (empty) → exit 1.
   Check `echo $?` each time.
7. Commit.

## Acceptance criteria

- [ ] Ctrl-C and `kill -TERM` both exit 0 in under a second, logging start and
      stop.
- [ ] `KREM_BASE_URL=` (empty) prints one clear error to stderr and exits 1;
      nothing is bound, nothing is logged twice.
- [ ] `KREM_LOG_LEVEL=error` suppresses the info lines; `debug` shows more than
      `info`.
- [ ] `journalctl`-style output contains no token, key, DB path contents or
      config struct dump. Prove it: compute a hash of your test token and grep
      the captured logs for both the value and the hash test output.
- [ ] The server does not return from `run` while requests are in flight; a
      request slower than the shutdown timeout is cut off, and the exit is
      still 0 with an explanatory log line.
- [ ] Tests: config failure path, and one shutdown path that doesn't rely on
      actually signalling the test process if you can avoid it.
- [ ] `go test ./... -race -count=1`, `go vet`, `gofmt` clean.
- [ ] Committed as one slice. Ticket 01 is done.

## Questions to answer

- Why should `os.Exit` appear only in `main` (or nowhere, if `main` returns)?
  What breaks if a helper exits?
- What timeout will you pick for shutdown, and what in tickets 02–07 will make
  that number mean something? (Ticket 07 adds a ticker; ticket 02 adds a DB
  handle.)
- In what order do the server and the future database close? Which one must be
  closed last, and why?
- Should the logger be passed explicitly to handlers in ticket 08, or installed
  as the default? What did the primer say about globals and tests?
- What does "graceful" guarantee when a request is in the middle of a Gotify
  call in ticket 07? Is that acceptable?
- If the process receives a second Ctrl-C while shutting down, what should
  happen? Decide, even if you don't implement it.

## Pitfalls

- `os.Exit` before the deferred `Shutdown`/`Close` runs — the classic version of
  this bug looks like "the data is occasionally corrupt after a deploy".
- Calling `Shutdown` when the listener failed to start, then waiting for a
  server that never served.
- `context.WithTimeout` followed by `defer cancel()` inside a loop or inside
  `Shutdown`'s scope, cancelling the timeout you meant to use.
- Using a `context.Context` as a struct field instead of passing it as the
  first parameter.
- Logging the config struct with `%+v`, or logging each env var "for debugging"
  (this is how tokens end up in issue reports).
- Registering signal handlers twice (once in `main`, once in `run`), so the
  second signal is swallowed.
- Using `log.Fatal` in a library package; nobody can test or reuse that code.

## When you're stuck

If Ctrl-C hangs, check whether you're waiting on `Shutdown` with a context that
has no deadline, or waiting on the server goroutine without closing its error
channel. If the exit code is 130 or 143, the signal killed the process before
your handler ran — the handler isn't registered yet, or it's registered after
the blocking call.
