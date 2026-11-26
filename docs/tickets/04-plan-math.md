# Ticket 04 — Plan math

**Depends on:** 03
**Effort:** two or three sessions — this is the heart of the project
**Read first:** [`../design.md`](../design.md) §5 (scheduling), §6 (notification plans, quiet hours, snooze, message text), §7 (priority table); [`../schema.md`](../schema.md) §5

## Goal

`internal/plan` answers, purely, with no I/O and no `time.Now()` inside:

- what a task's next anchor is, from its cron/`@every` schedule;
- how long before an anchor its cycle must be materialised (`lead`);
- every nag instant in a plan, and which phase is active at a given moment;
- when the next nag is, given the last successful one;
- where a future nag belongs after quiet hours;
- when a snooze preset lands;
- the notification title and body for an occurrence.

Every row in the two tables in `schema.md` §5 is reproduced by a test.

## Why now

This is the part where a wrong line silently loses a dentist appointment six
months from now. It's also the most fun part: pure functions are trivially
testable, and you can drive the clock by hand. Get it right here and the
scheduler in ticket 07 becomes a loop.

## What you'll learn

| Concept | Where to look |
|---|---|
| Cron parsing and `Schedule.Next` | `go doc github.com/robfig/cron/v3` (read `ParseStandard`, `Schedule`, `ConstantDelaySchedule`) |
| `time.Location` and wall-clock vs instant | `go doc time.LoadLocation`, [Time zones in Go](https://pkg.go.dev/time#pkg-overview) |
| Duration parsing | `go doc time.ParseDuration` — then decide how `3d` fits |
| Sorting and deduplicating | `go doc slices.Sort`, `go doc slices.Compact` |
| Table tests with named cases | `go test -run TestSeries -v` |
| Property-style checks | `go doc testing/quick` or a loop over random inputs |
| Fuzzing (optional) | `go doc testing.F`, `go test -fuzz` |

## Scope

**In**

- Schedule: parse and validate cron / `@every`; compute the next anchor after a
  given instant, in the task's timezone.
- `lead(plan)`.
- `series(plan, due)` → sorted, deduplicated `[]Instant{At, Phase}`.
- `nextNag(plan, due, after)` → earliest series instant strictly after `after`.
- `phaseAt(plan, due, at)` → active phase index (used for the denormalised
  `occurrence.phase` and logging).
- `initNag(plan, due, now, opts)` → per `design.md` §5 "Materialization" and
  §6 quiet hours: earliest future instant, past-everything fallback, and
  informational auto-resolution signalling (returning *what* the caller should
  do, e.g. "send now", "resolve now", "wait until T").
- `clampQuiet(instant, window, loc)` for repeating phases only.
- Snooze presets: `evening`, `tomorrow`, `three_days`.
- Title/body rendering given a task, an occurrence, and the nearest relevant
  history (e.g. last completion time).
- Duration parsing that accepts `d` and `w` in addition to Go's units, used by
  the MCP layer later.

**Out**

- Reading or writing the DB.
- Sending anything. Nothing in this package imports `net/http` or the store.

## Acceptance criteria

- [ ] A test reproduces the contacts table in `schema.md` §5 exactly: seven nag
      instants with phase indices and priorities.
- [ ] A test reproduces the toothbrush table exactly, including the duplicated
      21 Nov 08:00 instant collapsing to one nag.
- [ ] `lead(dentist phases)` is 168h; `lead(contacts phases)` is 0.
- [ ] `initNag` for a plan starting at `-168h`, evaluated 7 days before the
      anchor, returns the `-168h` instant.
- [ ] `initNag` for the same plan, evaluated *after* the event, reports
      "resolve now" for an informational task and "send now" for an
      acknowledged task.
- [ ] Quiet hours: an instant at 22:00 clamps to next 08:00; 08:00 stays;
      21:29 stays; 21:30 clamps; a one-shot phase at 03:00 does not clamp; an
      acknowledged task with `respect_quiet_hours = false` does not clamp.
- [ ] DST, America/Chicago: a `0 8 12 * *` anchor on 12 Nov 2026 and 12 Mar 2027
      is 08:00 local on both, and the UTC offsets differ. An `@every 24h` series
      stays 24h apart in elapsed time.
- [ ] A time that doesn't exist (02:30 on the spring-forward day) has a defined,
      tested behaviour.
- [ ] Series are strictly increasing, deduplicated, and always terminate.
- [ ] Snooze presets: at 19:00, `evening` = today 20:00; at 21:00, `evening` =
      tomorrow 20:00; `tomorrow` = next day 08:00; `three_days` = +3 days 08:00.
- [ ] Title/body golden tests match `design.md` §6 for: due today, 2 days left,
      2 days overdue, no-deadline day count, informational "in 6 days".

## Suggested approach

1. Write the schedule wrapper first: parse once into something you can call
   `Next(t)` on. Validate at parse time, not at use time. Check the docs for
   how the library represents `@every` — the type differs from a cron schedule.
2. Build `series` bottom-up: for one phase, generate instants from `due + after`
   in steps of `every`; stop when you have enough. Decide how you bound the work
   (a caller-supplied horizon, or "give me the next N"). Do not generate
   forever.
3. Compose phases, sort, dedupe, and put the phase index on each instant.
4. Implement the three "next" queries on top of `series` and test them against
   the schema tables before moving on.
5. Add quiet hours as a function over a single instant. Apply it in exactly one
   place (inside `initNag`/`nextNag`? or as a caller step?) and write down why.
6. Add snooze presets, which are small but fiddly around day boundaries.
7. Add rendering last; it's string assembly, and having real occurrence data in
   the tests makes it obvious when the wording is off.
8. Add a property test: for random sane plans, `series` is sorted, unique,
   strictly increasing, and `nextNag(series[i])` equals `series[i+1]` (when it
   exists).

## Questions to answer

- `0 8 12 * *` means "08:00 local on the 12th". `@every 24h` means "24 hours
  later". Which is which in the library's types, and where in your code does the
  difference become a decision?
- What is the difference between `t.Add(24*time.Hour)` and
  `time.Date(...day+1...)` in a zone with DST? Which one does each schedule kind
  need?
- Where does `time.Now()` enter the system? Make a list, and decide which
  functions take `now` as a parameter instead.
- `initNag` has three possible outcomes (send, wait, resolve). How do you
  express that in Go without sentinel booleans — an enum-ish type, an interface,
  a struct of optional fields? What does the caller do most easily?
- What should happen if two phases produce the same instant but different
  priorities? Who wins, and do you document it?
- If a plan's phases are unsorted in JSON, is that a validation error or do you
  sort? What did `design.md` promise?
- How large can `series` get for `@every 1h` with no end? What stops a caller
  from asking for a hundred years of it?

## Pitfalls

- `time.Time` values with a monotonic reading don't compare equal with `==`.
  Use `.Equal` in tests, and strip monotonic clocks at the boundary
  (`.Round(0)` or constructing from `UnixMilli`).
- `time.Date` silently normalises invalid wall-clock times (02:30 in a spring
  gap). Whatever it does, write a test that pins the behaviour you want.
- Quiet hours that cross midnight: 21:30–08:00 is not "before 21:30 and after
  08:00" reasoning that works naively; draw the number line before coding.
- Building the series with `for { t = t.Add(every) }` and an `every` that parsed
  to zero.
- Applying the quiet-hours clamp to the anchor or to one-shot phases — the
  design explicitly forbids it, and the dentist reminder is the reason.
- Generating the series in UTC and then converting to local for display: the
  *computation* is local; the *storage* is UTC. Doing it in the wrong order
  moves the dentist by an hour twice a year.
- Title wording drifting from `design.md`; golden tests should be read as spec.

## Testing checklist

- [ ] Golden series: contacts, toothbrush (with the dedupe), one-off dentist.
- [ ] `nextNag` at every interesting point: before the first instant, exactly on
      an instant, between two, after all.
- [ ] Phase/priority boundaries at the deadline.
- [ ] Quiet hours matrix: 21:29, 21:30, 22:00, 07:59, 08:00, with the window
      disabled, and with the task opting out.
- [ ] DST: 1 Nov 2026 (fall back) and 14 Mar 2027 (spring forward) in
      `America/Chicago`; assert both local times and durations.
- [ ] `@every 72h` across a DST transition: elapsed time stays 72h.
- [ ] Snooze presets around 20:00 and around midnight.
- [ ] Property test for sortedness/uniqueness/monotonicity.
- [ ] `go test ./internal/plan -race -count=1`.

## When you're stuck

If the series tests fail, print the series with `%v` and a fixed reference date
before comparing; most bugs here are an off-by-one in "at or after" versus
"strictly after". Write down in a comment which one each function guarantees.
For DST, test with `TZ=America/Chicago go test ./internal/plan -run DST -v` — and
import `time/tzdata` somewhere (the `cmd` package is a good place) so the binary
has zone data even on a minimal system; that's also what makes the Nix build
reproducible in ticket 10.
