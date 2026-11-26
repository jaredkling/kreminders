# Ticket 07 — Scheduler loop

**Depends on:** 05, 06
**Effort:** two sessions
**Read first:** [`../design.md`](../design.md) §3 (lifecycle), §5–§7 (materialise, series, delivery outcomes, duplicates), §8 (confirmation, for the link seam), §11 (failure modes)

## Goal

`serve` now does the whole job in the background: every 30 seconds it
materialises what's due, sends the nags that the plan says are due, records what
happened, schedules the next attempt after failures, and clears notifications
for cycles resolved behind its back. A heartbeat keeps `/healthz` honest.
Shutdown is clean.

Click URLs come from a small seam you fill in during ticket 08; for now a stub
that returns `KREM_BASE_URL + "/c/pending"` is enough to see a notification
arrive.

## Why now

This is the first concurrent code in the project. Doing it after the pure logic
(04), the state changes (05) and the side effect (06) means the loop itself is
boring — which is exactly what you want from concurrency.

## What you'll learn

| Concept | Where to look |
|---|---|
| Goroutines and `select` | [Tour: concurrency](https://go.dev/tour/concurrency/1), [Go blog: advanced concurrency](https://go.dev/blog/advanced-go-concurrency-patterns) |
| `time.Ticker` semantics | `go doc time.Ticker` |
| Context cancellation in loops | `go doc context.WithCancel` |
| Shutting down background work | `go doc sync.WaitGroup` |
| Safe shared state | `go doc sync/atomic`, `go doc sync.Mutex` |
| Race detection | `go test -race`, `go doc runtime` (`GOMAXPROCS`) |
| Testable concurrency | channels as triggers; `httptest`-style fakes |

## Scope

**In**

- A scheduler type: `Run(ctx)` plus a single-tick operation that can be called
  directly from tests.
- Tick body in the order the design mandates:
  1. `Materialise(now)` (one transaction, ticket 05),
  2. read due occurrences (`status = 'pending' AND next_nag_at <= now`, limited),
  3. for each: build the message (plan + ticket 06 sender),
  4. send **outside** any transaction, with a context deadline,
  5. record the outcome and recompute `next_nag_at`/`nag_count`/`last_nag_at`
     or apply backoff,
  6. for resolved-while-sending occurrences, delete the just-sent message.
- Failure handling: `delivery_failures`, backoff from ticket 06, reset on
  success, never advance `last_nag_at` on failure.
- Heartbeat: last successful tick time, exposed to `/healthz` (ticket 01's
  handler gets richer).
- Graceful stop: in-flight tick finishes or times out; no new ticks.
- Logging: one line per delivery attempt at info, failures at warn with the
  occurrence id and Gotify status.

**Out**

- Real confirm tokens and pages (ticket 08) — use the stub seam.
- MCP (09).
- A second process or distributed locking. The design says one writer, one
  ticker; the tests should make that assumption explicit rather than accidental.

## Acceptance criteria

With a temp DB, a fake sender and a manual tick trigger:

- [ ] Create a task whose first nag is now; one tick sends exactly one message,
      the fake records one payload, and the occurrence has `nag_count = 1`,
      `last_nag_at = now`, and `next_nag_at` = the next series instant.
- [ ] Two ticks in a row at the same instant send one message in total.
- [ ] A failing sender: `notification` gets an `ok = 0` row with the error,
      `delivery_failures = 1`, `next_nag_at = now + 1m`, and `nag_count` stays 0.
      The next successful attempt resets `delivery_failures` and increments
      `nag_count`.
- [ ] Six consecutive failures: the delay follows 1m, 5m, 15m, 1h, 1h, 1h and
      nothing is sent in between.
- [ ] Resolve an occurrence between step 3 and step 5 (fake sender can do this):
      the send still happens, then the message is deleted, and the occurrence
      stays resolved. Log it.
- [ ] Two pending occurrences in different tasks are both delivered in one tick,
      in `next_nag_at` order.
- [ ] More than the per-tick limit are due: only the limit is sent, the rest on
      the next tick, with no reordering into the past.
- [ ] `/healthz` reports `db: ok` and a fresh `last_tick`; with a stale
      heartbeat injected, it returns 503.
- [ ] `Run(ctx)` with a canceled context returns promptly; a tick in progress
      is allowed to finish (or its context is cancelled) — decide and document
      which, then test it.
- [ ] A slow sender does not cause overlapping ticks: a second tick waits.
- [ ] `go test ./... -race -count=1` is clean, including the concurrent cases.
- [ ] Manual: with a real Gotify token, a real nag arrives and the log shows an
      occurrence id and a message id.

## Suggested approach

1. Separate the loop from the work: one goroutine owns timing and cancellation,
   one method does "one tick". The method must not care how it was triggered.
2. Choose a trigger that tests can drive without sleeping. Possibilities: an
   exported `TickOnce(ctx)` used by the loop and the tests, or a `chan` of
   triggers with a default ticker feeding it. Look at both and pick the one with
   fewer surprises on shutdown.
3. Decide the transaction boundaries before writing SQL. Design §7 requires
   sending *outside* the write transaction; figure out the smallest pair of
   writes you need (claim, then record) and whether you need a claim at all in a
   single-process design. Write the reason in a comment.
4. Wire the heartbeat where `healthz` can read it without a package-level
   global. (Options: pass a small interface to the web package in ticket 08, or
   keep a struct with a mutex. There is no single right answer; there is a wrong
   answer: two places that disagree.)
5. Implement failure accounting using the `delivery_failures` column — the state
   survives restarts, which a memory-only counter would not.
6. Test with the fake sender first; only then try the real one. If the real one
   is involved in tests, guard it behind a build tag or an env var so
   `go test ./...` never touches the network.

## Questions to answer

- What happens to the message that is being sent when `Shutdown` arrives? Which
  of these is acceptable: send it, cancel it, or leave a row that says "sent,
  id unknown"? What does `design.md` §11 promise about duplicates?
- Why can't the `notification` insert and the Gotify call be in the same
  transaction? What breaks if they are?
- If `next_nag_at` is computed from `last_nag_at` and the clock moves backwards
  (NTP correction), what happens? Does it matter more or less than a duplicate?
- How does the scheduler know an occurrence was resolved during the send,
  without holding a lock across the HTTP call?
- Where does the tick period come from, and why is 30s acceptable given a
  minimum `every` of 1h?
- What should happen if `Materialise` fails (DB locked)? Does the tick abort,
  skip delivery, or deliver from existing state?

## Pitfalls

- Starting the ticker before migrations/listener are ready, or before the first
  tick the server reports healthy.
- `defer ticker.Stop()` inside `Run` looks right but check that it's actually
  reached on every return path.
- A `select` with `case <-ticker.C:` and no `case <-ctx.Done():` that only
  notices shutdown on the next tick.
- Sending one message then aborting the tick on the first error, so all later
  occurrences are delayed by a whole interval.
- Updating `next_nag_at` before the send succeeds: a failed nag then waits for
  the series to catch up instead of retrying in a minute.
- Writing `notification` rows with `ok = 1` in log-only mode and then trying to
  delete message id 0.
- Reading the heartbeat without synchronisation and getting caught by `-race`
  (and by reality).
- A test that waits 30 seconds. If a test needs real time, the design of the
  trigger is wrong.

## Testing checklist

- [ ] Happy path, single and multiple occurrences.
- [ ] Idempotent double tick.
- [ ] Failure, backoff sequence, recovery.
- [ ] Resolve-during-send cleanup.
- [ ] Stale heartbeat → 503.
- [ ] Shutdown during a slow send (fake sender blocks on a channel you control).
- [ ] Non-overlap with a slow sender.
- [ ] `go test ./... -race -count=1` and `go test ./internal/scheduler -run Ticker -v`.
- [ ] Manual run overnight with a real task and quiet hours; check the journal
      has exactly one tick per interval and one send at the expected time.

## When you're stuck

If the loop misbehaves, add a test that calls the single-tick operation directly
with a fixed `now`. If that passes but the loop fails, the bug is in timing or
cancellation, not in the work — log the tick start/end with the trigger source.
When the race detector complains, fix the access, don't silence it with a
bigger lock; usually the fix is "pass the value instead of sharing it".
