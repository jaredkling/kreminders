# Ticket 02 — SQLite and migrations

**Depends on:** 01e
**Effort:** one evening
**Read first:** [`../schema.md`](../schema.md) §1–§4, [`../design.md`](../design.md) §3

## Goal

The binary can open the SQLite database described in `schema.md`, create it on
first run, apply embedded migrations exactly once, and refuse to start against a
database written by a newer build. `kreminders migrate` exists; `serve` runs the
same step at startup.

## Why now

Every remaining ticket reads or writes this schema. Doing the DDL as a
migration, rather than pasting it into a scratch file, forces you to make the
schema reproducible on any machine — which is the whole deployment story in
ticket 10.

## What you'll learn

| Concept | Where to look |
|---|---|
| `database/sql` basics, `sql.DB` is a pool | `go doc database/sql`, [database/sql tutorial](https://go.dev/doc/tutorial/database-access) |
| Driver registration with a blank import | `go doc database/sql.Register` |
| `modernc.org/sqlite` DSN and `_pragma` | `go doc modernc.org/sqlite` (read the package docs' DSN section) |
| Embedding files in a binary | `go doc embed`, [embed docs](https://pkg.go.dev/embed) |
| Transactions and `Exec` | `go doc database/sql.Tx` |
| `PRAGMA user_version` | [SQLite pragma docs](https://sqlite.org/pragma.html#pragma_user_version) |
| Temp dirs in tests | `go doc testing.T.TempDir` |

## Scope

**In**

- `internal/store` (or similar) with: open the DB from `KREM_DB`, create parent
  directories, set the pragmas from `schema.md` §1, apply migrations, expose
  `Close`.
- `migrations/0001_init.sql` containing exactly the DDL from `schema.md` §2,
  embedded with `go:embed`, plus the `user_version` bookkeeping. Put it where
  `go:embed` can see it from the package that uses it (the README layout
  suggests `internal/store/migrations/`).
- `kreminders migrate` subcommand.
- Downgrade protection in `serve` and `migrate`.
- Adapters in `config` for the DB path if you decided it belongs there.

**Out**

- Any query beyond what the tests need (`SELECT 1`, pragma checks).
- Backup automation (ticket 10).

## Acceptance criteria

```sh
KREM_DB=/tmp/kreminders-test.db go run ./cmd/kreminders migrate
sqlite3 /tmp/kreminders-test.db '.tables'
sqlite3 /tmp/kreminders-test.db 'PRAGMA user_version;'
```

- [ ] First `migrate` creates the file, the three tables, the indexes and checks
      from `schema.md` §2, and sets `user_version = 1`.
- [ ] A second `migrate` exits 0 and changes nothing (compare `PRAGMA
      user_version` and a `SELECT count(*) FROM sqlite_master` before/after).
- [ ] `sqlite3 file 'PRAGMA journal_mode;'` prints `wal` after the service has
      opened the DB, and `PRAGMA foreign_keys` returns 1 on a fresh connection.
- [ ] Deleting the DB file and running `serve` recreates it (same migration path
      as `migrate`).
- [ ] With `PRAGMA user_version = 99` set by hand, both `serve` and `migrate`
      refuse with a message that mentions restoring a backup.
- [ ] `sqlite3 file '.schema task'` matches the DDL in `schema.md` (apart from
      whitespace) — diff it properly, don't eyeball it.
- [ ] Tests create their own temp database and clean up; no test depends on
      `/var/lib/...` or a fixed path.

## Suggested approach

1. Put the `.sql` file somewhere `go:embed` can reach. Read the embed docs
   carefully about paths — the compiler will complain in a specific way if you
   try to reach outside the package directory.
2. Build the DSN from the config path. Look at how the driver expects pragma
   parameters to be written, and confirm each pragma took effect (some are
   per-connection, some are file-level; the distinction matters for the tests).
3. Implement `migrate` as: read current version → find embedded files with a
   number above it → in a transaction per file (or one for all — decide and
   justify) → update `user_version`. Handle "database is newer than binary".
4. Wire `migrate` into `serve` before the listener starts. Make sure a failed
   migration leaves the process not listening.
5. Write the tests listed below. The "run twice" test is the one that catches
   almost every mistake in this ticket.

## Questions to answer

- Why does the design use `PRAGMA user_version` instead of a `schema_migrations`
  table? Under what condition would you switch?
- Which pragmas are stored in the database file and which are per connection?
  What does that imply for `sql.DB`'s pool and for your tests?
- Is it safe to run the whole `0001_init.sql` in a single `Exec`? Prove it with a
  test rather than assuming; if your driver or SQL text disagrees, decide how
  you'll split statements without breaking on a `;` inside a string.
- Where should "create the parent directory" live, and what should happen if the
  process lacks permission?
- How would you know, at runtime, that migration 1 has been applied exactly
  once on every machine?

## Pitfalls

- Forgetting `foreign_keys`, then finding that half the `CHECK`s work and the
  cascade doesn't.
- Relying on the pragma that isn't set by the DSN.
- An open `*sql.DB` keeping `t.TempDir()` files alive after the test ends; add
  `t.Cleanup` when you open.
- Comparing `user_version` as a string, or forgetting it's per-database and not
  per-table.
- Running `migrate` and `serve` concurrently in a test by accident.
- Expecting `PRAGMA user_version = 1` to be parameterisable — it isn't; that's
  one place where string building is unavoidable. Convince yourself the string
  can't be attacker-controlled.

## Testing checklist

- [ ] Fresh DB: version 0 → migrate → 1, tables exist.
- [ ] Idempotency: migrate twice, assert no error and no change.
- [ ] Newer DB: seed `user_version = 99`, assert the specific error.
- [ ] Pragmas: open the DB through your package, then query `journal_mode` and
      `foreign_keys` through the same `*sql.DB`.
- [ ] Cascade: insert a task + occurrence, delete the task, assert the
      occurrence is gone (proof that `ON DELETE CASCADE` is active).
- [ ] Schema fidelity: a test that compares `sqlite_master` DDL against a
      checked-in golden string is allowed here; it catches accidental edits.

## When you're stuck

If `go:embed` complains, print the embedded file list with `fs.Glob` in a tiny
test — seeing what the binary actually contains is usually faster than reading
the docs twice. If a pragma "doesn't work", open a `*sql.DB`, run the pragma,
and immediately query it on the same connection; if it's per-connection you'll
see the difference when you query through a second one (`db.SetMaxOpenConns(1)`
makes the experiment unambiguous).

