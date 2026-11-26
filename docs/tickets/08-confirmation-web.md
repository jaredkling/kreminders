# Ticket 08 — Confirmation web

**Depends on:** 07
**Effort:** two sessions
**Read first:** [`../design.md`](../design.md) §8 (tokens, routes, page states), §6 (snooze presets, message text), §3 (listener), [`../ops.md`](../ops.md) §3 (proxy expectations)

## Goal

A notification's `click.url` opens a page you can act on from your phone: Done,
Snooze, Skip. Tokens are signed, scoped, expiring, and never needed in a cookie.
The dashboard at `/` lists open items with the same links. The scheduler's link
stub is replaced by the real thing.

## Why now

Everything upstream works and needs only a link. This ticket is where the
"family" part becomes real: one tap, and the nag disappears because ticket 07
already cleans up on resolution.

## What you'll learn

| Concept | Where to look |
|---|---|
| HMAC and constant-time comparison | `go doc crypto/hmac`, `go doc hmac.Equal` |
| URL-safe encoding | `go doc encoding/base64.RawURLEncoding` |
| `html/template` escaping | `go doc html/template`, [template docs](https://pkg.go.dev/html/template) |
| Serving templates from the binary | `go doc embed` (reuse ticket 02's habit) |
| Routing by method and path values | `go doc net/http.ServeMux` (Go 1.22 patterns) |
| Form parsing and `303` redirects | `go doc net/http.Request.ParseForm`, `go doc net/http.Redirect` |
| Middleware as `func(http.Handler) http.Handler` | `go doc net/http.Handler` |
| Security headers and CSP | [MDN: CSP](https://developer.mozilla.org/en-US/docs/Web/HTTP/CSP) |

## Scope

**In**

- Token package or functions: mint and verify, per design §8
  (`occurrence_id`, expiry, truncated HMAC, base64url), with tests.
- Handlers:
  - `GET /c/{token}` — render; never mutate.
  - `POST /c/{token}/done`, `/snooze`, `/skip` — mutate via ticket 05's service,
    then `303` back to `GET /c/{token}`.
  - `GET /` — open items with generated links, delivery health from
    `notification` rows, maybe recent history.
- Page states: pending, resolved (show when and how late), expired (410),
  unknown/tampered (404, indistinguishable).
- Headers from design §8 on `/c/*`: `Cache-Control: no-store`,
  `Referrer-Policy: no-referrer`, `nosniff`, and the CSP.
- Request body limits (`http.MaxBytesReader`) on POSTs.
- Templates embedded, parsed once at startup, rendered with typed data (no
  `map[string]any` if you can avoid it).
- An access-log middleware that logs method, route pattern, status and duration
  — but **not** the raw path for `/c/*`, because the path contains the token.
- Replace the scheduler's link stub with the real token builder, and remove the
  stub.

**Out**

- MCP (09).
- Styling beyond a readable mobile layout; no JS required.
- Authentication/login (the capability token is the auth).

## Acceptance criteria

Round-trip tests with `httptest`, plus manual checks from the phone:

- [ ] A minted token verifies, and each of these fails with the states in
      design §8: one character changed, expiry in the past, unknown id, empty
      signature, id with a leading `+`/space, extra segment.
- [ ] `GET /c/{valid}` returns 200 containing the task title and the scheduled
      and deadline times **in the task's timezone**, plus three snooze options.
- [ ] `GET` never changes the database (assert the occurrence row is untouched).
- [ ] `POST /c/{valid}/done` resolves `done`, stores the note if posted, and
      returns 303 to the same `GET`; a second POST shows the resolved page and
      does not move `resolved_at`.
- [ ] Snooze options set `next_nag_at` to the times ticket 04 defined; skip
      resolves `skipped`.
- [ ] Resolved page shows "Done on …", the lateness relative to the deadline,
      and the next cycle date; no form is rendered.
- [ ] Expired token → 410 with a link to `/`; tampered token → 404 with no
      detail about why. Both pages have the headers.
- [ ] `/` lists pending occurrences most urgent first, each with a working
      link, and shows the last 24h of failed deliveries if any.
- [ ] Every `/c/*` response (including 404/410) has the required headers;
      a test asserts them on all four cases.
- [ ] Logs from a page view do not contain the token: grep your test log output
      for it.
- [ ] The real notification sent by ticket 07 opens this page and the Done
      button works from the phone over Netbird.
- [ ] `go test ./internal/web -race`, `go vet`, `gofmt`.

## Suggested approach

1. Implement mint/verify first, with no HTTP. This is small and everything else
   depends on it. Write the tests for tampering and expiry before the handlers.
2. Design the template data types. Compute everything the template needs
   (preformatted times, "2 days late", button list) in Go and pass plain values.
   Templates should not do arithmetic or timezone work.
3. Parse templates once when the handler is constructed; test the rendering
   through `httptest` rather than by calling `Execute` directly.
4. Route with method-aware patterns if you're on Go 1.22+; otherwise check the
   method yourself and return 405 with an `Allow` header. Either is fine — but
   a `POST`-only route must never be reachable by `GET`.
5. Post/Redirect/Get: after a mutation, redirect rather than render, so a
   refresh can't repeat the action. Figure out which status code is right when
   the browser landed here from a form POST.
6. Build the dashboard last; it reuses the token builder and the existing
   "open occurrences" query, and is your manual-testing surface.
7. Swap the scheduler's stub for the real builder, delete the stub, and run the
   ticket 07 tests again — they should still pass with the URL shape changed.

## Questions to answer

- Why does the signature cover the occurrence id *and* the expiry, and why is the
  expiry part of the URL instead of only a database column?
- How can you compare the signature without leaking timing information? What
  does `hmac.Equal` do that `bytes.Equal` doesn't?
- What happens if the token's fields are joined ambiguously (e.g. ids `1` and
  `23` versus `12` and `3`)? How does your format prevent that?
- Why is there no CSRF token? Under what change to the design would you need
  one?
- Is the dashboard (`/`) safe to leave unauthenticated behind the Netbird
  perimeter? What's on it, and what isn't?
- How does a 404 for an unknown id avoid becoming an oracle for "which
  occurrences exist"?
- Where does `html/template` save you from injection, and what would you have to
  do to break it? (Then don't.)

## Pitfalls

- Building the HMAC input with `fmt.Sprintf("%d.%d", ...)` where a malformed
  input can be re-parsed differently than it was signed; parse strictly into
  integers and re-render the canonical form before verifying.
- Expiry stored in milliseconds in one place and seconds in another; the token
  example in the design uses seconds.
- Confirming via GET because "it's just a link" — that's precisely the bug the
  design avoids (link previews, prefetch).
- Rendering with `template.HTML` on anything that comes from `notes`.
- Setting security headers in one handler and forgetting the error paths.
- Logging `r.URL.String()`, which includes the token.
- Templates parsed per request (slow, and you can't catch parse errors until
  production).
- `http.Redirect` with 307 or 308 for the post-action redirect; the browser may
  repeat the POST.
- Dashboard links minted with a different expiry than notification links,
  making "expired" surprising.
- Forgetting that snooze presets are computed in the *task* timezone, not the
  server's.

## Testing checklist

- [ ] Token table: valid, tampered char, wrong id, expired, malformed segments,
      empty, very long.
- [ ] Handler table: pending → done → second done; pending → snooze (each
      preset) → assert `next_nag_at`; pending → skip; resolved → GET; expired →
      410; tampered → 404.
- [ ] Headers on 200/303/404/410.
- [ ] Dashboard: ordering, link presence, empty state.
- [ ] Access log: token absent, status and route present.
- [ ] Full manual pass with `curl -i` and then on the phone.
- [ ] `go test ./... -race`, including the scheduler tests with the real builder.

## When you're stuck

If the page renders but the buttons do nothing, check two things in order: does
the form's `action` include the token (view source), and does the POST reach your
handler at all (access log)? If the token verifies in tests but not in the
browser, compare byte-for-byte what was minted and what arrived — clients
sometimes percent-encode or truncate long URLs in previews. If the phone can't
load the page at all, it's the proxy or Netbird, not Go: `curl` from another mesh
machine first.
