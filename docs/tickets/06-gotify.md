# Ticket 06 — Gotify delivery

**Depends on:** 03 (and 04 for message text)
**Effort:** one evening
**Read first:** [`../design.md`](../design.md) §7 (delivery, priorities, cleanup, duplicates), [`../mcp.md`](../mcp.md) `send_test_notification`, [`../schema.md`](../schema.md) `notification` table

## Goal

The binary can deliver a nag to Gotify and delete it again. There is a fake
sender for tests and a log-only sender for local development. Failures are
classified, bounded and recordable. `kreminders test-notify` works against the
real server.

## Why now

It's the only outbound integration, and it is independent of the scheduler. Once
it exists, ticket 07 has something to call, and you can get a real notification
on your phone before the web page exists (the link will 404 — that's expected
and motivating).

## What you'll learn

| Concept | Where to look |
|---|---|
| Interfaces in Go and where to declare them | [Effective Go: interfaces](https://go.dev/doc/effective_go#interfaces), [Go blog: errors are values](https://go.dev/blog/errors-are-values) |
| HTTP client, timeouts and cancellation | `go doc net/http.Client`, `go doc net/http.NewRequestWithContext` |
| JSON request and response bodies | `go doc encoding/json` |
| `httptest.NewServer` for integration-style tests | `go doc net/http/httptest` |
| Typed errors with `errors.As` | `go doc errors.As` |
| Bounded reads | `go doc io.LimitReader` |
| Joined URLs | `go doc net/url.JoinPath` |

## Scope

**In**

- Payload construction from a task/occurrence: title, body, priority, plain-text
  content type, and the absolute click URL from `KREM_BASE_URL`.
- A sender that `POST`s to `{KREM_GOTIFY_URL}/message`, authenticated with the
  app token, reading the returned `{"id": N}`.
- A delete operation using the client token (`DELETE /message/{id}`), treating
  "already gone" sanely.
- A log-only sender selected when `KREM_GOTIFY_URL` is empty.
- A failure type that distinguishes "rejected by Gotify" (4xx), "server/temporary"
  (5xx, timeout), and "configuration" (missing token).
- A pure backoff helper: 1m, 5m, 15m, then 1h cap, with tests.
- `kreminders test-notify`.
- A test double the scheduler can use in ticket 07.

**Out**

- Deciding *when* to retry (that's ticket 07); this ticket only computes delays
  and records outcomes.
- Storing notification rows (ticket 07 owns the write, next to the send).
- Token generation (`../mcp.md` `confirm_url` placeholder is fine).

## Acceptance criteria

- [ ] With a test HTTP server standing in for Gotify, the request contains:
      path `/message`, the app token header, `Content-Type: application/json`,
      a non-empty title/message, the right priority, and a click URL starting
      with `KREM_BASE_URL`.
- [ ] The click URL is never built from request headers (grep for `Host`,
      `X-Forwarded` in `internal/gotify` and `internal/web`).
- [ ] A 200 response with `{"id":42}` returns 42 and no error.
- [ ] A 401 returns a typed error whose message includes the status but **not**
      the token; a test asserts the token string does not appear in
      `fmt.Sprintf("%v", err)`.
- [ ] A 500 and a client timeout are both classified as retryable.
- [ ] The HTTP client has a timeout, and every request carries a context.
- [ ] `DELETE /message/{id}` sends the client token; a 404 is treated as success
      (the nag is gone either way).
- [ ] `KREM_GOTIFY_URL=""` makes `serve` log a warning at startup and use the
      log-only sender; `test-notify` says it did nothing network-related.
- [ ] `test-notify` against the real Gotify produces a notification on the phone
      (manual, one line in your notes).
- [ ] Backoff: attempts 1–4 yield 1m, 5m, 15m, 1h; attempt 9 is still 1h; the
      function has no clock inside.
- [ ] `go test ./internal/gotify -race` passes without network access.

## Suggested approach

1. Write the payload type by transcribing the JSON example in `design.md` §7.
   Then write a small function that produces it from a task + occurrence +
   click URL. Keep string formatting of the message in `plan` (ticket 04) and
   leave this package to assemble the envelope.
2. Write the HTTP client with a constructor that takes the base URL and token —
   not a package-level singleton. Check how `url.JoinPath` behaves with a path
   in the base URL.
3. Test against `httptest.NewServer`: have the handler decode the body into your
   type and record it in a channel or variable you assert on. Also assert the
   header, and return a canned response.
4. Define your error types. Make sure callers can answer "should I retry?" from
   the error alone, without inspecting strings.
5. Add the log-only sender and make the *selection* a single function so
   `main`/`serve` doesn't need to know about modes. Watch out for interfaces
   holding a typed nil.
6. Add `test-notify` wiring in `cmd`.
7. Write the fake sender for ticket 07: it should record calls and be able to
   fail on demand, but stay in `_test.go` unless the scheduler tests need it
   across packages (then a tiny `internal/gotify/fake` package is fine).

## Questions to answer

- Should the interface (`Send`, `Delete`) live in `gotify` or in the package that
  consumes it? Read the Go proverb about accepting interfaces and returning
  structs, then decide where the seam should be for *this* project.
- What exactly should the sender return on success? Just the id, or the whole
  response? What does ticket 07 need to write into `notification`?
- Where do retry counts live — the sender, the occurrence row
  (`delivery_failures`), or both? What happens after a restart in each case?
- Why is `DELETE` failure not allowed to block a resolution? Look at the
  resolution path in design §7 "Cleanup on resolution".
- Gotify 3 shows tokens once, and the client token can read all messages. What
  is your policy for logging request errors that include a response body?
- If `KREM_GOTIFY_CLIENT_TOKEN` is unset, is that a startup error, a warning, or
  silently ignored? The design says the feature is optional — make sure the
  code path still delivers.

## Pitfalls

- A struct pointer returned as an interface is non-nil even when the pointer is
  nil; a "log-only" sender that was never initialised will still pass `!= nil`
  checks and panic when called.
- `json.NewEncoder(r.Body).Encode(payload)` in the *test* server, then asserting
  on the request after the handler returns — copy what you need.
- Forgetting to close response bodies, exhausting connections under the ticker.
- Putting the token in the URL or in an error string that gets logged.
- Reading unbounded error bodies into logs.
- Building the click URL with `path.Join` on strings, which mangles `https://`.
- Treating any 2xx as "id present"; Gotify may return a body without `id` in
  error-ish futures — decide what "missing id" means.
- Retrying inside the sender, making the scheduler's accounting wrong.

## Testing checklist

- [ ] Payload golden test (JSON), including `priority` and the `click.url`.
- [ ] Header, method, path assertions.
- [ ] Status matrix: 200 with id, 204/no id, 400, 401, 429, 500, timeout.
- [ ] Error classification table (`retryable`, `configuration`, etc.).
- [ ] Backoff table including the cap and attempt 0.
- [ ] Delete: 204 success, 404 success, 500 retryable.
- [ ] Log-only mode: returns a sentinel id (decide: 0 or nil) and never dials.
- [ ] No token leakage: assert on the error string and on captured logs.
- [ ] `go test ./... -race`.

## When you're stuck

If Gotify rejects your request, reproduce it with `curl` from `design.md` §7
until the body is exactly right, then compare with what your Go code sends:
point your client at a local `nc -l 5736` (or a tiny `httptest` handler that
dumps the body) and diff the two. If the notification arrives but has no click
action, check `contentType` and the `extras` nesting — clients are strict about
the namespace names.
