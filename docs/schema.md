# kreminders — Schema

| | |
|---|---|
| Engine | SQLite 3 (WAL), driver `modernc.org/sqlite` (pure Go) |
| File | `/var/lib/kreminders/kreminders.db` |
| Version marker | `PRAGMA user_version` |

## 1. Conventions

- **Time**: every instant is `INTEGER` **UTC milliseconds** since the Unix
  epoch. No `TEXT` timestamps, no `DATETIME`, no local time in the database.
  Timezone only exists as a *task attribute* used when computing schedule
  instants; the result is stored absolute. This makes comparisons trivial and
  DST a pure computation problem in Go.
- **Durations**: `INTEGER` milliseconds (`*_ms`).
- **Booleans**: `INTEGER` 0/1 with `CHECK`.
- **JSON**: `TEXT` holding a JSON document, validated by the application on
  write (SQLite's `json_valid` is used as a cheap backstop).
- **Concurrency**: WAL, one writer process, `busy_timeout=5000`. Every
  multi-statement write runs inside `BEGIN IMMEDIATE`.
- **Deletes**: `ON DELETE CASCADE` from task → occurrence → notification.
  Everything is archived by making rows `missed`/`skipped`, not by deleting.

Connection DSN:

```
file:/var/lib/kreminders/kreminders.db?_pragma=journal_mode(WAL)&_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)&_pragma=synchronous(NORMAL)
```

## 2. DDL

```sql
PRAGMA user_version = 1;

CREATE TABLE task (
  id                  INTEGER PRIMARY KEY,
  slug                TEXT    NOT NULL UNIQUE,
  title               TEXT    NOT NULL,
  notes               TEXT    NOT NULL DEFAULT '',
  kind                TEXT    NOT NULL CHECK (kind IN ('recurring','once')),
  cron_expr           TEXT,
  once_at             INTEGER,
  timezone            TEXT,
  needs_ack           INTEGER NOT NULL DEFAULT 1 CHECK (needs_ack IN (0,1)),
  grace_ms            INTEGER NOT NULL DEFAULT 0 CHECK (grace_ms >= 0),
  resolve_window_ms   INTEGER NOT NULL DEFAULT 43200000
                              CHECK (resolve_window_ms >= 0),          -- 12h
  phases              TEXT    NOT NULL CHECK (json_valid(phases)),
  respect_quiet_hours INTEGER NOT NULL DEFAULT 1
                              CHECK (respect_quiet_hours IN (0,1)),
  enabled             INTEGER NOT NULL DEFAULT 1 CHECK (enabled IN (0,1)),
  next_due_at         INTEGER,                                       -- cursor
  created_at          INTEGER NOT NULL,
  updated_at          INTEGER NOT NULL,
  CHECK ((kind = 'recurring' AND cron_expr IS NOT NULL AND once_at IS NULL)
      OR (kind = 'once'      AND once_at  IS NOT NULL AND cron_expr IS NULL))
);

CREATE INDEX task_due ON task (next_due_at)
  WHERE enabled = 1 AND next_due_at IS NOT NULL;

CREATE TABLE occurrence (
  id                INTEGER PRIMARY KEY,
  task_id           INTEGER NOT NULL REFERENCES task(id) ON DELETE CASCADE,
  due_at            INTEGER NOT NULL,
  deadline_at       INTEGER,                       -- NULL = informational
  status            TEXT    NOT NULL DEFAULT 'pending'
                            CHECK (status IN ('pending','done','skipped','missed')),
  phase             INTEGER NOT NULL DEFAULT 0,    -- denormalised, for display
  next_nag_at       INTEGER,
  nag_count         INTEGER NOT NULL DEFAULT 0,
  last_nag_at       INTEGER,
  delivery_failures INTEGER NOT NULL DEFAULT 0,
  resolved_at       INTEGER,
  resolution        TEXT CHECK (resolution IS NULL
                            OR resolution IN ('done','skipped','missed')),
  note              TEXT,
  UNIQUE (task_id, due_at),
  CHECK (deadline_at IS NULL OR deadline_at >= due_at),
  CHECK (status = 'pending'
         OR (resolved_at IS NOT NULL AND next_nag_at IS NULL AND resolution IS NOT NULL)),
  CHECK (status <> 'pending' OR resolution IS NULL)
);

CREATE INDEX occurrence_open ON occurrence (next_nag_at) WHERE status = 'pending';
CREATE INDEX occurrence_task ON occurrence (task_id, due_at DESC);

CREATE TABLE notification (
  id                INTEGER PRIMARY KEY,
  occurrence_id     INTEGER NOT NULL REFERENCES occurrence(id) ON DELETE CASCADE,
  sent_at           INTEGER NOT NULL,
  priority          INTEGER NOT NULL CHECK (priority BETWEEN 0 AND 10),
  gotify_message_id INTEGER,
  ok                INTEGER NOT NULL CHECK (ok IN (0,1)),
  error             TEXT
);

CREATE INDEX notification_occ  ON notification (occurrence_id);
CREATE INDEX notification_live ON notification (occurrence_id)
  WHERE gotify_message_id IS NOT NULL;
```

### Column notes

| Column | Notes |
|---|---|
| `task.slug` | `^[a-z0-9][a-z0-9-]{0,63}$`, the MCP handle. Never reused after delete (checked at create time against history in the same transaction as best-effort). |
| `task.notes` | Free text shown in the notification body and on the confirm page. |
| `task.timezone` | `NULL` = use `KREM_TZ`. IANA name, validated with `time.LoadLocation`. |
| `task.grace_ms` | `deadline_at = due_at + grace_ms`. |
| `task.resolve_window_ms` | Informational tasks only: auto-resolve at `max(due_at, last plan instant) + resolve_window_ms`. Unused for `needs_ack = 1`. |
| `task.phases` | Ordered JSON array; see `design.md` §6. Validated: non-empty, sorted by `after` (after parsing), `every` ≥ 1h when set, priority 0–10. |
| `task.next_due_at` | Anchor of the next cycle. The cycle is materialised once `next_due_at − lead ≤ now`, where `lead = −min(phase.after)`. `NULL` = disabled, exhausted (`once`), or nothing further to schedule. |
| `occurrence.phase` | Index of the phase that produced the most recent nag. Display only — never used to compute the next nag. |
| `occurrence.next_nag_at` | The authoritative "when do we nag next", or `NULL` once resolved. |
| `occurrence.delivery_failures` | Consecutive failed sends; drives backoff, reset on success. |
| `occurrence.resolution` | Why it closed: `done` (confirmed), `skipped` (explicit), `missed` (a new cycle arrived first). |
| `notification.gotify_message_id` | From the Gotify response. `NULL` in log-only mode (no cleanup possible). |

## 3. Invariants

1. At most one `pending` occurrence per task. Enforced in the materialiser
   transaction, not by a constraint (the rollover marks the old one `missed`
   first).
2. `status = 'pending'` ⟺ `next_nag_at` may be non-null; all other statuses
   have `next_nag_at IS NULL` (CHECK).
3. `(task_id, due_at)` is unique, so a materialiser retry can never duplicate a
   cycle.
4. `notification` rows are append-only audit; they are never edited.
5. `deadline_at` is `NULL` exactly for occurrences of `needs_ack = 0` tasks.
6. `phase`/`nag_count` are cosmetic. Deleting and recomputing them yields the
   same future behaviour as long as `last_nag_at` and `due_at` are intact.

## 4. Migrations

- Numbered files embedded in the binary: `migrations/0001_init.sql`,
  `0002_....sql`, applied in filename order inside a single transaction.
- `PRAGMA user_version` holds the last applied number. SQLite DDL is
  transactional, so a failed migration rolls back cleanly.
- `kreminders migrate` applies and exits; `kreminders serve` applies at startup
  and refuses to start if the file's `user_version` is *newer* than the binary
  (downgrade protection).
- Migrations are forward-only. A destructive change requires a new table plus
  copy, never an `ALTER ... DROP` of a column holding data.
- No `DROP TABLE` in a migration without a preceding backup step in `ops.md`.

## 5. Worked examples

### Tasks

```sql
INSERT INTO task (slug, title, notes, kind, cron_expr, once_at, timezone,
                  needs_ack, grace_ms, phases, next_due_at,
                  created_at, updated_at)
VALUES
('contacts', 'Contacts', 'Change contacts. New pair is in the bathroom drawer.',
 'recurring', '0 8 12 * *', NULL, NULL, 1, 259200000,
 '[{"after":"0","every":"24h","priority":5},
   {"after":"72h","every":"12h","priority":8}]',
 1794492000000, 1794492000000, 1794492000000),

('toothbrush', 'Toothbrush head', 'New heads are under the sink.',
 'recurring', '0 8 12 * *', NULL, NULL, 1, 691200000,
 '[{"after":"0","every":"72h","priority":3},
   {"after":"192h","every":"24h","priority":6}]',
 1794492000000, 1794492000000, 1794492000000),

('birthday-lena', 'Lena''s birthday', 'Card + present. She likes plants.',
 'recurring', '0 9 12 4 *', NULL, NULL, 0, 0,
 '[{"after":"0","every":null,"priority":3}]',
 1807538400000, 1807538400000, 1807538400000),

('dentist', 'Dentist appointment', 'Dr. Ruiz, 4th floor. Bring insurance card.',
 'once', NULL, 1807797600000, NULL, 0, 0,
 '[{"after":"-168h","every":null,"priority":5},
   {"after":"-24h","every":null,"priority":5},
   {"after":"0","every":null,"priority":6}]',
 1807797600000, 1807538400000, 1807538400000);
```

Resulting local times (`America/Chicago`):

| Task | `next_due_at` | Local |
|---|---|---|
| contacts | 1794492000000 | Thu 12 Nov 2026, 08:00 CST |
| toothbrush | 1794492000000 | Thu 12 Nov 2026, 08:00 CST |
| birthday-lena | 1807538400000 | Mon 12 Apr 2027, 09:00 CDT |
| dentist | 1807797600000 | Thu 15 Apr 2027, 09:00 CDT |

### Occurrences for one month

`contacts` (grace 72h ⇒ deadline Sun 15 Nov 08:00):

| # | `next_nag_at` (ms) | Local | Plan phase | Priority |
|---|---|---|---|---|
| 1 | 1794492000000 | Thu 12 Nov 08:00 | 0 (`after 0`, `24h`) | 5 |
| 2 | 1794578400000 | Fri 13 Nov 08:00 | 0 | 5 |
| 3 | 1794664800000 | Sat 14 Nov 08:00 | 0 | 5 |
| 4 | 1794751200000 | Sun 15 Nov 08:00 — **deadline** | 1 (`after 72h`, `12h`) | 8 |
| 5 | 1794794400000 | Sun 15 Nov 20:00 | 1 | 8 |
| 6 | 1794837600000 | Mon 16 Nov 08:00 | 1 | 8 |
| … | every 12h | | 1 | 8 |

`toothbrush` (grace 192h ⇒ deadline Fri 20 Nov 08:00):

| # | `next_nag_at` (ms) | Local | Plan phase | Priority |
|---|---|---|---|---|
| 1 | 1794492000000 | Thu 12 Nov 08:00 | 0 (`after 0`, `72h`) | 3 |
| 2 | 1794751200000 | Sun 15 Nov 08:00 | 0 | 3 |
| 3 | 1795006800000 | Wed 18 Nov 08:00 | 0 | 3 |
| 4 | 1795183200000 | Fri 20 Nov 08:00 — **deadline** | 1 (`after 192h`, `24h`) | 6 |
| 5 | 1795269600000 | Sat 21 Nov 08:00 | 1 | 6 |
| … | daily | | 1 | 6 |

A completed `contacts` cycle after nag #2:

```sql
-- occurrence row, after POST /c/.../done
id                = 1
task_id           = 1
due_at            = 1794492000000
deadline_at       = 1794751200000
status            = 'done'
phase             = 0
next_nag_at       = NULL
nag_count         = 2
last_nag_at       = 1794578400000
delivery_failures = 0
resolved_at       = 1794583200000   -- Fri 13 Nov 09:20 CST
resolution        = 'done'
note              = 'Left eye pair, from the drawer'
```

### Informational auto-resolution

`dentist` is `needs_ack = 0` with a `-168h` first phase, so its occurrence is
materialised a week before the event (Thu 8 Apr 2027 09:00) even though
`next_due_at` is the event instant. It fires at −168h, −24h and 0, then resolves
automatically:

```
resolved_at = max(due_at, last plan instant) + resolve_window_ms
            = 1807797600000 + 43200000      -- Thu 15 Apr 21:00 CDT
status      = 'done'
resolution  = 'done'
note        = 'auto-resolved (informational)'
```

## 6. Query cookbook

The scheduler's two queries. Both are index-backed.

```sql
-- 1. Tasks to inspect for materialisation. The per-row lead check
--    (next_due_at - lead <= now) happens in Go, so the plan JSON is parsed once.
SELECT id, slug, kind, cron_expr, once_at, timezone, needs_ack,
       grace_ms, phases, resolve_window_ms, respect_quiet_hours, next_due_at
FROM task
WHERE enabled = 1 AND next_due_at IS NOT NULL
ORDER BY next_due_at;

-- 2. Occurrences due to nag
SELECT o.id, o.task_id, o.due_at, o.deadline_at, o.phase, o.nag_count,
       o.last_nag_at, o.delivery_failures,
       t.slug, t.title, t.notes, t.needs_ack, t.timezone, t.phases,
       t.respect_quiet_hours
FROM occurrence o
JOIN task t ON t.id = o.task_id
WHERE o.status = 'pending' AND o.next_nag_at <= ?
ORDER BY o.next_nag_at
LIMIT 50;
```

Dashboard and MCP reads:

```sql
-- open items, most urgent first
SELECT t.slug, t.title, o.id, o.due_at, o.deadline_at, o.next_nag_at, o.nag_count
FROM occurrence o JOIN task t ON t.id = o.task_id
WHERE o.status = 'pending'
ORDER BY COALESCE(o.deadline_at, o.due_at), o.due_at;

-- last completion per task (the question that started all this)
SELECT o.task_id, MAX(o.resolved_at)
FROM occurrence o
WHERE o.status = 'done'
GROUP BY o.task_id;

-- newest nags still on the phone for an occurrence (cleanup sweep)
SELECT id, gotify_message_id
FROM notification
WHERE occurrence_id = ? AND gotify_message_id IS NOT NULL AND ok = 1;

-- delivery health (served by /healthz and the dashboard)
SELECT COUNT(*) FROM notification
WHERE ok = 0 AND sent_at >= ?;   -- last 24h
```

## 7. Backup and restore

Nightly `VACUUM INTO` produces a consistent single-file snapshot without
stopping the service (safe with WAL; it reads a transactionally consistent
view):

```sh
sqlite3 /var/lib/kreminders/kreminders.db \
  "VACUUM INTO '/var/lib/kreminders/backup/kreminders-$(date +%F).db'"
```

- Keep 14 daily snapshots; the systemd unit is in `ops.md`.
- Verify a snapshot with `PRAGMA integrity_check;` (expect `ok`).
- Restore: stop the service, move the snapshot to `kreminders.db`, delete any
  stale `-wal`/`-shm` files next to it, start the service, run
  `sqlite3 ... "PRAGMA integrity_check; PRAGMA user_version;"`.
- Do **not** copy the live `.db` file while the service is running.
