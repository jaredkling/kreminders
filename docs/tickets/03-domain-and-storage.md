# Ticket 03 — Domain and storage

**Depends on:** 02
**Effort:** one or two evenings
**Read first:** [`../schema.md`](../schema.md) §2 (DDL), §3 (invariants), §5 (worked examples), [`../design.md`](../design.md) §4 (domain model)

## Goal

You can create, read, update, list and delete tasks, and create and read
occurrences, through Go code backed by SQLite. The four example tasks from
`schema.md` §5 round-trip exactly. Invalid input is rejected with a message that
names the field. Not-found and duplicate conditions are distinguishable by
callers without string matching.

## Why now

Tickets 04 and 05 are pure logic over these types; ticket 08 and 09 are HTTP and
MCP over these operations. If the types and errors are right, everything after
this is mostly wiring.

## What you'll learn

| Concept | Where to look |
|---|---|
| Structs, methods, pointers vs values | [Tour of Go: methods](https://go.dev/tour/methods/1), Effective Go |
| Sentinel errors and `errors.Is` | `go doc errors.Is`, [Go blog: working with errors](https://go.dev/blog/go1.13-errors) |
| `fmt.Errorf` with `%w` | `go doc fmt.Errorf` |
| `sql.NullInt64` and scanning NULLs | `go doc database/sql.NullInt64` |
| JSON tags and custom marshalling | `go doc encoding/json`, `go doc encoding/json.Marshaler` |
| Time arithmetic and epoch conversion | `go doc time.Time.UnixMilli`, `go doc time.UnixMilli` |
| Regexp validation | `go doc regexp.MustCompile` |
| `RETURNING` in SQLite (optional) | [SQLite RETURNING docs](https://sqlite.org/lang_returning.html) |
| Table-driven tests | [Go subtests](https://go.dev/wiki/TableDrivenTests) |

## Scope

**In**

- `internal/domain`:
  - `Task`, `Occurrence`, `Notification` types.
  - `Plan`/`Phase` types with parsing, plus rendering back to the stored JSON.
  - Validation: slug format and length, title non-empty, kind ↔ schedule
    fields, timezone loadable, priority range, phase ordering, `every` minimum,
    grace non-negative.
  - Helper(s) for derived values (`deadline_at`, "is this overdue", "days
    left/late") — decide which belong here.
  - Error values for not-found and conflict.
- `internal/store`:
  - `CreateTask`, `GetTaskByID`, `GetTaskBySlug`, `ListTasks`,
    `UpdateTask`, `DeleteTask`.
  - `InsertOccurrence`, `GetOccurrence`, `FindOpenOccurrence(taskID)`,
    `ListOpenOccurrences`.
  - Conversion helpers between DB rows and domain types. Keep them in one place.
- A `seed` path for tests, not for production.

**Out**

- Schedule math and nag calculation (ticket 04).
- Resolution rules — `done`/`skip`/`missed` transitions (ticket 05/07). Insert
  and read only.
- Delivery, tokens, HTTP, MCP.

## Acceptance criteria

- [ ] `CreateTask` with each of the four `schema.md` §5 examples succeeds, and
      reading back gives identical values — including `next_due_at` and
      `phases`.
- [ ] `GetTaskBySlug("nope")` returns an error detectable with `errors.Is`
      against a not-found sentinel.
- [ ] Creating two tasks with the same slug returns a *conflict* error, and the
      test proves it isn't the not-found error.
- [ ] Updating only `notes` leaves every other field and `created_at` intact and
      bumps `updated_at`.
- [ ] `deadline_at` round-trips as `NULL` for an informational task and as a
      value for an acknowledged one.
- [ ] `phases` survives a round trip without reordering or losing `every: null`.
- [ ] Invalid inputs are rejected *before* SQL runs, with errors naming the
      field: bad slug, empty title, `kind = once` without `once_at`, loadable
      timezone required, priority 11, `every` of 5 minutes.
- [ ] All store tests use `t.TempDir()` and the migration from ticket 02.
- [ ] No SQL string appears outside `internal/store`.

## Suggested approach

1. Define the types first, in terms of what callers need, not what the columns
   look like. Decide whether `due_at` is a `time.Time` or an `int64` in the
   domain and write down your reason in a comment. (The DB stores milliseconds;
   something has to convert, and only one layer should.)
2. Write validation as its own tested step: functions that take a task and
   return an error, with no DB involved. Try to make "validate a newly created
   task" and "validate an update" share as much as possible.
3. Write the store queries one at a time against a test database. Start with
   create-and-read, then list, update, delete. Use `RETURNING` if you like it;
   otherwise `LastInsertId` is fine — know why you chose.
4. Introduce sentinel errors. Map driver errors (unique constraint) to your
   conflict error in exactly one place, using `errors.Is`/`errors.As` on the
   driver error type rather than matching strings. Check the driver docs for how
   it identifies constraint failures.
5. Add the occurrence operations. Notice that the "open occurrence" query is
   the same one the scheduler will use; write it as you'll want it (ticket 05).
6. Round-trip the four example tasks as one table-driven test. This test is your
   regression net for the rest of the project.

## Questions to answer

- Should `deadline_at` be stored on insert, or derived on read from `due_at` and
  `grace_ms`? The schema stores it — why, and where do you compute it so both
  the insert and the notification title agree?
- Where do default plans (design.md §6) live: `domain`, HTTP/MCP handlers, or a
  service layer? What happens when MCP and the web UI disagree about defaults?
- Is a domain type allowed to have a `Validate()` method that mutates (e.g.
  fills defaults)? What's the cost of a validator with side effects?
- Why is `*sql.DB` passed around rather than stored in a global?
- How will you represent "this occurrence has no deadline" so that no caller
  ever dereferences a nil pointer?
- What makes `errors.Is` different from `==` for your sentinels? Find the place
  where you wrap a sentinel and confirm `errors.Is` still matches.

## Pitfalls

- Scanning into `*int64` and getting `converting NULL to int64 is unsupported`.
- Building SQL with `fmt.Sprintf` for values. (For `user_version` it was
  unavoidable; for data it never is. Use placeholders.)
- JSON marshalling of `every: null`: a `time.Duration` with `omitempty` will
  disappear instead of serialising as `null`. Decide on a representation and
  test it.
- Validating with `regexp.MatchString` on every call and compiling the pattern
  each time; compile once at package level.
- Forgetting `updated_at` on updates, or using the DB's `strftime` while the
  rest of the system uses Go's clock.
- Making `ListTasks` scan in a different order than the dashboard expects, then
  "fixing" it in the template.
- Returning the raw driver error to callers, so HTTP/MCP have to interpret
  driver text.

## Testing checklist

- [ ] Validation table: one case per rule, including the boundary (slug length
      64 OK / 65 rejected, priority 0 and 10 OK, 11 rejected).
- [ ] Round-trip table: the four example tasks, asserting every field including
      the epoch values from `schema.md`.
- [ ] Not-found vs conflict, both via `errors.Is`.
- [ ] Update partiality and `updated_at` behaviour.
- [ ] Occurrence: insert pending, read back, `deadline_at` NULL and non-NULL,
      unique `(task_id, due_at)` violation surfaces as conflict.
- [ ] Cascade: delete a task, occurrences disappear (from ticket 02, keep it).
- [ ] `go test ./internal/... -race`.

## When you're stuck

Write the SQL by hand in `sqlite3` first, using the worked examples in
`schema.md` §5. Once a query gives the right rows in the shell, port it to Go —
that separates SQL problems from Go problems, which is the most common way to
lose an evening here. If scanning feels repetitive, look at how
`database/sql.Rows.Scan` takes pointers and consider a small `scanTask` helper
rather than a reflection-based mapper.
