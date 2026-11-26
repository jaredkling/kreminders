# Ticket 01d — HTTP server and `/healthz`

**Depends on:** 01c
**Effort:** 1–2 hours
**Part of:** [01 — Bootstrap](01-bootstrap.md)

## Goal

`kreminders serve` loads and validates configuration, then listens on
`127.0.0.1:5736` (or whatever `KREM_LISTEN` says) and answers:

```sh
curl -s localhost:5736/healthz | jq
# { "ok": true, "version": "0.0.1-dev", "time": "2026-11-26T09:12:00-06:00" }
```

Wrong methods and unknown paths are rejected sensibly. The handler is tested
without opening a port. Shutdown is still Ctrl-C and still abrupt — that's
01e.

## Why this slice

This is the smallest HTTP server worth having, and it teaches the two-layer test
habit you'll use for every handler in ticket 08: test the handler with
`httptest`, test the wiring by hand. Separation here saves hours later.

## What you'll learn

- `http.Handler`, `http.HandlerFunc`, and `*http.ServeMux`
- Method-aware route patterns (Go 1.22+) and what happens without them
- Writing JSON with the right headers
- `httptest.NewRequest` / `httptest.NewRecorder`
- `http.Server` with timeouts vs `http.ListenAndServe`
- Binding explicitly, and testing with `127.0.0.1:0`

## Steps

1. Write the handler first and its test: GET returns 200,
   `Content-Type: application/json`, and decodable JSON with the three fields.
   A second case asserts a non-GET method is not 200.
2. Add the `serve` subcommand: load config (printing the validation error to
   stderr and exiting non-zero if it fails), build a mux, register the handler,
   construct an `http.Server` with timeouts, listen on the configured address,
   serve.
3. Test the real binding once: listen on `127.0.0.1:0`, ask the listener for
   its address, make a request, close. This catches "the handler is fine but the
   server was never wired" bugs.
4. Manual checks: `ss -ltnp | grep 5736`, `curl -i`, wrong method with
   `curl -X POST -i`, unknown path. Then `KREM_LISTEN=127.0.0.1:5736` (already
   in use) to see the bind error.
5. Commit.

## Acceptance criteria

- [ ] `go run ./cmd/kreminders serve` with a valid `KREM_BASE_URL` logs the
      address and stays running.
- [ ] `curl -i localhost:5736/healthz` → 200, JSON content type, `ok: true`.
- [ ] `curl -i -X POST localhost:5736/healthz` → not 200, and if you return 405,
      an `Allow` header is present.
- [ ] `curl -i localhost:5736/nope` → 404.
- [ ] `ss -ltnp | grep 5736` shows `127.0.0.1`, never `0.0.0.0` or `[::]`.
- [ ] Starting a second instance on the same port prints a clear bind error and
      exits non-zero; it does not panic or hang.
- [ ] With `KREM_BASE_URL` unset, `serve` exits non-zero before binding, with
      the config error printed once.
- [ ] `go test ./internal/... ./cmd/... -race` covers the handler and the
      ephemeral-port binding.
- [ ] Committed as one slice.

## Questions to answer

- Why construct an `http.Server` instead of calling `http.ListenAndServe`? Name
  two things you gain before ticket 07 needs them.
- What does the mux pattern `GET /healthz` do that `"/healthz"` doesn't? Which
  behaviour do you actually want for `POST`?
- `httptest.NewRecorder` doesn't test timeouts or binding. What is the smallest
  test that does?
- Where does `version` come from in the health response, and why not a new
  constant?
- The design's `/healthz` eventually reports the database and the scheduler
  heartbeat. What should it report *today*, and how will ticket 07 extend it
  without changing the response shape?
- If `KREM_BASE_URL` has a trailing slash, 01c rejects it. Why should `serve`
  never "fix" it silently?

## Pitfalls

- `http.DefaultServeMux` (the global one): convenient now, impossible to test in
  isolation later.
- Calling `w.Write` before `w.WriteHeader(405)` so the status is always 200.
- Forgetting `Content-Type` and letting `curl` guess.
- Encoding the error response with `%v` of the error — that's how internal paths
  end up in public responses.
- Binding to `":5736"` "because the docs say 5736": that's all interfaces.
- Starting the listener before migrations exist in ticket 02. The order is
  migrate → listen; write it that way now even with no migrations.
- Panicking on a bind error instead of returning it; panics in `main` are not
  more honest.

## When you're stuck

If `curl` gets a connection refused, check the address in the log line against
`ss -ltnp` — the most common cause is binding `127.0.0.1` and curling
`[::1]`/`localhost` which resolves to IPv6 first. Try
`curl -4 http://127.0.0.1:5736/healthz`. If the handler test passes but the
server returns 404, the mux in the test and the mux in `serve` are not the same
object.
