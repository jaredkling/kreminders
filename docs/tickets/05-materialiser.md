# Ticket 05 — Materialiser and resolution

**Depends on:** 04
**Effort:** one or two sessions
**Read first:** [`../design.md`](../design.md) §5 (materialisation, deadline), §6 (plans), §8 (state transitions); [`../schema.md`](../schema.md) §3 (invariants)

## Goal

A service layer that, given "now":

1. materialises the next cycle of each task when its lead window opens,
2. rolls over a still-pending cycle to `missed` when a newer one supersedes it,
3. advances the cursor correctly — including skipping several cycles after
   downtime and recording nothing but the latest,
4. inserts informational cycles already resolved when their event is in the
   past,
5. resolves cycles as `done` or `skipped`, and snoozes them.

The result is durable state the scheduler (ticket 07) can deliver from and the
web/MCP (tickets 08, 09) can mutate. No notifications are sent here.

## Why now

This is where the design's invariants either hold or quietly break. Doing it
before any HTTP or Gotify code means you can test it with a fake clock and a
temp database, in milliseconds, with no network.

## What you'll learn

| Concept | Where to look |
|---|---|
| Transactions with `BeginTx`, `Commit`, `Rollback` | `go doc database/sql.Tx` |
| Context propagation | `go doc context`, [Go blog: context](https://go.dev/blog/context) |
| Returning prepared results instead of sending inside a tx | `go doc database/sql.Tx.Query` |
| File organisation for a service layer | [Organizing a Go module](https://go.dev/doc/modules/layout) |
| Unique constraint handling | the driver's error type; `errors.As` |
| Deterministic time in tests | inject `now time.Time` or a small clock interface |
| Scenario-style tests | `t.Run` with a shared fixture builder |

## Scope

**In**

- A `Materialise(ctx, now)` operation that applies design §5 for every enabled
  task and returns what changed (created occurrences, rolled-over ones).
- `Resolve(ctx, occurrenceID, status, note, at)` for `done`, `skipped`.
- `Snooze(ctx, occurrenceID, until)`.
- `SetEnabled`, and `UpdateTask` recomputing `next_due_at` (design §5 last two
  bullets).
- Whatever the scheduler and web UI need to read next (e.g. "open occurrences
  ordered by urgency" already exists from ticket 03).

**Out**

- Sending notifications, deleting Gotify messages.
- Tokens, HTML, MCP.
- Concurrency (single caller; the ticker in ticket 07 guarantees it).

## Acceptance criteria

Using a fixed clock and a temp DB:

- [ ] Monthly contacts, tick at 12 Nov 07:59 → no occurrence. Tick at 08:00 →
      one occurrence with `nag_count = 0`, `next_nag_at = 08:00`, deadline the
      15th, and `task.next_due_at` moved to 12 Dec.
- [ ] Dentist with a `-168h` first phase: created 1 Jan, tick at 8 Apr 09:00 →
      occurrence exists with `next_nag_at` = 8 Apr 09:00, even though
      `next_due_at` is 15 Apr.
- [ ] Downtime: cursor at 12 Sep, tick at 20 Nov → exactly one new occurrence
      for 12 Nov; the September/October pending occurrence (if present) is
      `missed` with `resolved_at`; no row exists for 12 Oct.
- [ ] A pending October occurrence that was still open is `missed` **before**
      the November occurrence is inserted (unique constraint and invariant 1 in
      `schema.md` hold afterwards).
- [ ] After a `once` task materialises, `next_due_at` is `NULL` and the row is
      still readable.
- [ ] A `once` task whose event is already past resolves immediately when
      informational (`status = done`, note explains auto-resolution); an
      acknowledged one is created pending and nags now.
- [ ] Disabled task: no occurrence ever materialises; disabling an open one
      resolves it `skipped` with a note saying why.
- [ ] Calling `Materialise` twice in a row at the same instant creates nothing
      the second time and changes no timestamps.
- [ ] `Resolve(done)` sets `resolution`, `resolved_at`, clears `next_nag_at`,
      stores the note; a second `Resolve(done)` reports "already resolved" and
      does not overwrite `resolved_at`.
- [ ] `Resolve` on a `missed` cycle is an error that the caller can distinguish
      from not-found.
- [ ] `Snooze` on a resolved cycle is an error; on a pending one it sets
      `next_nag_at` and leaves `nag_count`/`last_nag_at` alone.
- [ ] Editing `schedule` recomputes the cursor from now and leaves an open
      occurrence untouched (design §5).
- [ ] Everything runs inside `go test ./... -race` with no real sleeps.

## Suggested approach

1. Decide where this lives. A `service` package that depends on both `store`
   and `plan` keeps `store` dumb and `main` thin; alternatively methods on a
   `Store` type. Pick one and write down the rule you'll use for the next ticket.
2. Write `Materialise` as: load candidate tasks (all enabled with a cursor) →
   for each, ask `plan` "is this due to materialise, and what is the latest
   anchor" → in one transaction: mark superseded pending `missed`, insert, update
   the cursor. Keep the plan math out of SQL and the SQL out of the math.
3. Be explicit about the order of writes inside the transaction and why. Write a
   comment that a reader can check against `schema.md` invariant 1.
4. Add resolution as small functions with a single source of truth for which
   status transitions are legal. Consider a table or a `switch` — then test the
   illegal ones, not just the legal ones.
5. Build the scenario test harness early: a helper that creates a task, runs a
   sequence of `(clock, action)` steps, and asserts the resulting occurrence
   rows. Most of this ticket's value is in that harness.

## Questions to answer

- Why does the design materialise *only the latest* missed cycle rather than one
  row per missed month? What would change if you recorded all of them?
- What exactly does `Materialise` return? Ticket 07 needs to know what to nag
  about; tickets 08/09 need to know what changed. Design the return value before
  writing the code, not after.
- If `Resolve` is called for an occurrence that a concurrent tick is about to
  deliver, who wins? (You'll finish this thought in ticket 07.)
- Should the "informational, event in the past" case be resolved inside
  `Materialise` or delegated to `Resolve`? What does that choice make easier to
  test?
- When a task's phases change, is the open occurrence's `next_nag_at`
  recomputed immediately? `mcp.md` says yes; `design.md` §5 doesn't say. Decide,
  implement it in one place, and make both docs say the same thing.
- How will you prove `next_due_at` is right for `@every 72h` after a long
  downtime, without generating a thousand instants?

## Pitfalls

- Inserting the new occurrence before marking the old `missed`: one of the
  invariants is violated for a moment, and your later uniqueness handling
  becomes confused.
- Using `INSERT OR REPLACE`, which would silently delete the old cycle's
  history.
- Advancing the cursor with `cron.Next(next_due_at)` instead of looping until
  "after now", then materialising a cycle from last month on the next tick.
- Mixing `time.Time` and epoch milliseconds across the service boundary and
  losing the location (both print fine, one shifts by an hour at DST).
- Doing the informational auto-resolve only in the tick path, so an event
  created after the fact never resolves.
- Forgetting `defer tx.Rollback()` — harmless but noisy in logs if you log it as
  an error after `Commit`.
- Writing tests that sleep for real time; every assertion here can be
  instantaneous.
- Letting `Resolve` accept arbitrary statuses from callers (MCP will eventually
  pass user input; the service is the last line of defence).

## Testing checklist

- [ ] The full acceptance list above, one `t.Run` per bullet.
- [ ] A "no-op tick" test: run `Materialise` on a task whose lead window hasn't
      opened and assert `updated_at`, cursor, and row counts are unchanged.
- [ ] Invariant assertions after every scenario helper run: at most one pending
      occurrence per task; resolved rows have `resolved_at` and no
      `next_nag_at`; `deadline_at` NULL exactly for informational tasks.
- [ ] Cursor edge cases: 31st of the month, leap day, `@every` after downtime.
- [ ] Error paths: unknown occurrence id, double resolve, resolve a `missed`
      cycle, snooze a resolved cycle.
- [ ] `go test ./... -race -count=1`.

## When you're stuck

When a scenario fails, dump the whole `occurrence` table for the task
(`sqlite3 -header -column`) instead of asserting field by field in code; seeing
every row usually reveals the ordering problem immediately. If you're unsure
whether the bug is in `plan` or in `Materialise`, evaluate the plan functions
directly with the same timestamps in a scratch test — ticket 04's tests should
already tell you which side is wrong.
