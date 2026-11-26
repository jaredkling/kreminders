# Ticket 01c — Configuration

**Depends on:** 01b
**Effort:** 1–2 hours, in four small commits
**Part of:** [01 — Bootstrap](01-bootstrap.md)

## Goal

An `internal/config` package that loads every variable from
[`../design.md`](../design.md) §10, applies defaults, reads `*_FILE` variants,
validates eagerly and returns an error that names the field. Nothing is wired to
`serve` yet — the test suite is the user interface for this slice.

## Why this slice

Configuration is the first place where a wrong design choice is expensive later:
every package in the project will receive a config value, and every deployment
mistake will look like a Go bug if the errors are vague. Do it in pieces, with a
test per piece.

Do **not** start with the full table. Four commits:

| Commit | Scope |
|---|---|
| 1 | `BaseURL` (required, validated) and `Listen` (default `127.0.0.1:5736`) |
| 2 | `DB`, `TZ`, `DefaultTime`, `QuietHours`, `LogLevel`, `GotifyURL` (defaults where the design gives them) |
| 3 | Secrets: `SigningKey` (required, 32 bytes hex), `GotifyAppToken`, `GotifyClientToken`, `MCPToken` (optional), plus the `_FILE` variants |
| 4 | Validation pass: one error listing every problem, not just the first |

Each commit builds, has tests, and changes behaviour a human can explain in one
sentence.

## What you'll learn

- Structs as plain data, methods on value vs pointer receivers
- `os.Getenv` vs `os.LookupEnv` (unset vs empty — they differ)
- Reading files and trimming what editors add
- Parsing timezone, `HH:MM-HH:MM` windows, and durations
- Returning errors from constructors; wrapping with context
- `errors.Join` for reporting several problems at once
- Table tests with `t.Setenv` (and why they can't run in parallel)

## Steps

1. Define a `Config` struct with only what commit 1 needs. Write `Load() (Config,
   error)`. A test sets nothing, expects a specific error; a test sets a valid
   URL, expects the default listen address.
2. Add commit 2's fields one or two at a time, each with a test. Encourage
   yourself to write the failing test first; it keeps the increments honest.
3. For `_FILE`: write one helper that returns "value from file if set, else the
   env value". Test precedence, a missing file, an empty file, and a trailing
   newline. Then use it for each secret. Notice the helper doesn't know which
   secret it's reading.
4. Validation last: decode the signing key and check the *decoded* length;
   `time.LoadLocation` the timezone; parse quiet hours into a start/end pair of
   minutes; reject a base URL with a trailing slash; accept only known log
   levels. Use `errors.Join` and make the message look like something you'd want
   to read at 2am.
5. Optional but useful: a `config` subcommand that prints the effective
   configuration with secrets redacted. It makes commit 3 visible without
   starting a server.
6. `go test ./internal/config -race -count=1` after every commit.

## Acceptance criteria

- [ ] `KREM_BASE_URL` unset → error naming it. `KREM_BASE_URL=x/` → error saying
      it must not end in `/`.
- [ ] Unset optional values produce the documented defaults
      (`127.0.0.1:5736`, `America/Chicago`, `08:00`, `21:30-08:00`, `info`).
- [ ] A signing key file containing 64 hex chars plus a newline loads; 10 bytes
      does not; a non-hex value does not.
- [ ] `_FILE` beats the direct variable when both are set.
- [ ] A secret's value never appears in an error message, and (if you added
      `config`) never in its output.
- [ ] Multiple simultaneous problems produce one error mentioning all of them.
- [ ] `go test ./internal/config -v` shows a named case per rule.
- [ ] No package besides `config` reads `os.Getenv`.
- [ ] Four commits, each green.

## Questions to answer

- Should `Load` read the environment itself, or take a lookup function so tests
  don't mutate the process? What does `t.Setenv` do about parallel tests, and why?
- Where do defaults live: at the point of reading, on the struct after parsing,
  or as constants used in both? Which choice makes the docs and the code easiest
  to keep in sync?
- Is an empty `KREM_MCP_TOKEN` the same as an unset one? The design says the
  endpoint is disabled — make both cases behave identically and say so in a test.
- Quiet hours as parsed minutes or as `time.Duration` since midnight? Which is
  easier for ticket 04 to apply, and which is easier to validate?
- What should happen if `KREM_QUIET_HOURS=off`? What about `22:00-22:00`?
- If `KREM_DB` points at a directory that doesn't exist, is that a config error
  or ticket 02's problem?

## Pitfalls

- Reading the file but not trimming `\n`, then comparing a 33-character string
  to 64 hex characters and getting a confusing message.
- Treating `os.Getenv` returning `""` as "unset", so an explicit empty
  `KREM_GOTIFY_URL=` doesn't work.
- A config struct whose `String()`/`%+v` prints secrets. Either custom-format it
  or never print it.
- Validating timezone names by hand ("looks like Area/City") instead of asking
  the time package.
- Building the "quiet hours" parser with `strings.Split` and then indexing
  without checking the length — write the malformed-input test first.
- Doing all four commits at once and then not knowing which change broke the
  tests when one fails.

## When you're stuck

Write the test that describes the *error message you want*, then implement
until it passes. If `t.Setenv` interacts badly with a parallel test, that's Go
telling you something real: env-based config is process-global, so keep those
tests sequential. If `time.LoadLocation` fails for `America/Chicago`, your Go
install is missing zone data — that's the same issue ticket 04 has to solve for
the deployed binary, so note it.
