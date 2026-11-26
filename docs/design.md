# kreminders — Design

| | |
|---|---|
| Status | Design settled, pre-implementation |
| Last updated | 2026-11-25 |
| Module | `kling.dev/kreminders` |
| Runtime | single Go binary, SQLite, Gotify |

## 1. Purpose

Household reminders that nag until the thing is actually done, and that you can
manage from an AI agent instead of a settings screen.

### Goals

- Recurring tasks (`monthly on the 12th`, `every 3 days`) and one-off dated
  events (`dentist Thursday 09:00`).
- Per-task nag plans: different cadences, and escalation once a deadline passes.
- Deadline that means "now I'm serious", not "stop caring".
- One-tap confirmation from the Gotify notification, landing on a small web
  page with Done / Snooze / Skip.
- Full CRUD and "what's open?" access over MCP, from agents on the LAN.
- One binary, one process, no containers, no external scheduler.
- Enough history to answer "when did I last change my contacts?" and "am I
  always late with the toothbrush?"

### Non-goals (v1)

- Multiple users. There is exactly one person; tasks are not assigned to anyone.
- Calendar sync (CalDAV/iCal), invitees, free/busy.
- A mobile app. Gotify is the delivery channel and the browser is the UI.
- Email, SMS, or any second notification channel.
- Time tracking, streaks, gamification.
- HA / multi-instance. One host, one process, one SQLite file.

## 2. Glossary

| Term | Meaning |
|---|---|
| **Task** | A definition: title, schedule, notification plan. Has a stable `slug`. |
| **Occurrence** | One cycle of a task: `"contacts, 12 Nov 2026"`. Carries status and nag state. |
| **Notification** | One attempt to deliver one nag to Gotify; audited, keeps the Gotify message id. |
| **Anchor** (`due_at`) | The instant a cycle becomes actionable. The first nag time is `due_at` unless the plan starts earlier (`after` < 0). |
| **Lead** | `-min(phase.after)`: how long before the anchor a cycle is materialised (0 for normal plans, 7 days for a `-1w` notice). |
| **Deadline** (`deadline_at`) | `due_at + grace`. After this, later plan phases apply (higher priority). |
| **Plan / phase** | Ordered notification rules relative to the anchor: `{after, every, priority}`. |
| **Quiet hours** | Window in which *future repeating* nags are pushed to the window start. |
| **Confirm token** | HMAC-signed, occurrence-scoped, expiring capability in a `/c/...` URL. |

## 3. Architecture

One process, three jobs: schedule, deliver, answer.

```
                                  ┌──────────────────────────────────────┐
   Gotify  ◄── POST /message ─────┤ kreminders serve   (127.0.0.1:5736)  │
notifications.kling.dev           │                                      │
      │                           │  ┌────────────┐   ┌───────────────┐  │
      │ click.url                 │  │  scheduler │   │  HTTP handlers│  │
      ▼                           │  │  30s tick  │   │  /c/{token}   │  │
   phone ── HTTPS ──► reverse ───►│  └─────┬──────┘   │  POST done    │  │
                      proxy       │        │          │  snooze, skip │  │
             kreminders.nb.       │        │          │  /  /healthz  │  │
               kling.dev          │        │          │  /mcp         │  │
                                  │        ▼          └───────┬───────┘  │
                                  │   ┌────────────────┐      │          │
                                  │   │ SQLite (WAL)   │◄─────┘          │
                                  │   │ /var/lib/...   │                 │
                                  │   └────────────────┘                 │
                                  └──────────────────────────────────────┘
                                                ▲
                                                │ HTTPS, bearer token
                                  agent on the LAN (MCP streamable HTTP)
```

- **Listener**: `127.0.0.1:5736` only. TLS is terminated by the existing reverse
  proxy on the host, which forwards `kreminders.nb.kling.dev` here.
- **MCP** shares the listener at `/mcp`; there is no second port and no stdio
  subcommand.
- **Absolute URLs** come exclusively from `KREM_BASE_URL`. Request headers
  (`Host`, `X-Forwarded-*`) are never used to build links.
- **Process lifecycle**: `migrate` → load config → open DB (WAL) → start
  listener → start ticker → wait for SIGTERM → stop ticker → `http.Shutdown` →
  close DB.
- **Single writer**: the scheduler and all HTTP handlers share one
  `*sql.DB`; there is no second process, so no cross-process locking is needed.
  The ticker is a single goroutine and ticks never overlap.

## 4. Domain model

```
task ─1:N─ occurrence ─1:N─ notification
  ▲             │
  │             └── status: pending | done | skipped | missed
  │                            (only `pending` can nag)
  └── kind: recurring | once   plan: JSON array of phases
```

### Lifecycle

```
   schedule fires                nag series (plan)                 done (web/MCP)
        │                              │                              │
        ▼                              ▼                              ▼
   ┌─────────┐   repeated nags    ┌─────────┐   confirm       ┌────────┐
   │ pending │ ─────────────────► │ pending │ ──────────────► │  done  │
   └─────────┘   (status fixed)   └─────────┘                 └────────┘
        │                              │
        │ skip (web/MCP)               │ next cycle materializes
        ▼                              ▼
   ┌─────────┐                    ┌────────┐
   │ skipped │                    │ missed │   (no confirmation for that cycle)
   └─────────┘                    └────────┘
```

Resolution (`done`/`skipped`/`missed`) sets `resolved_at`, clears
`next_nag_at`, and triggers deletion of that occurrence's outstanding Gotify
messages. Resolved occurrences are immutable except for `note`.

The deadline is **not** a state. It only changes which phase of the plan is
active, and therefore the priority. Nagging continues indefinitely.

## 5. Scheduling

### Kinds

| `kind` | Field | Examples |
|---|---|---|
| `recurring` | `cron_expr` — 5-field cron or `@every <dur>` | `0 8 12 * *`, `0 19 * * 1`, `@every 72h` |
| `once` | `once_at` — RFC3339 with offset, stored as UTC ms | `2027-04-15T09:00:00-05:00` |

Parsing uses `robfig/cron`'s `ParseStandard`, which accepts both forms:
cron expressions give calendar-correct behaviour across DST, while `@every`
gives true intervals (24h means 24h, even across a DST change). A per-task
`timezone` overrides the global `KREM_TZ` (`America/Chicago`).

Known and accepted: `0 8 31 * *` silently skips short months; there is no
"last day of month" syntax in v1.

### Materialization

The task row holds a cursor, `next_due_at`, which is the **anchor** of the next
cycle. A cycle is materialised `lead = -min(phase.after)` before that anchor, so
advance notices (`after: "-1w"`) fire on time. `lead` is derived from the plan,
never stored.

```
lead := -min(p.after for p in phases)       // 0 for a normal plan, 168h for the dentist

for each enabled task with next_due_at != NULL:
    if next_due_at - lead > now: continue
    t    := next_due_at
    next := nil
    if kind = recurring:
        loop:
            n := cron.Next(t)
            if n = nil or n > now: next = n; break
            t = n                           // skip cycles missed during downtime
    if no occurrence exists for (task, t):
        mark any other pending occurrence of the task as missed
        insert occurrence:
            due_at      = t
            deadline_at = t + grace
            next_nag_at = initNag(phases, t, now)
    task.next_due_at = next                 // NULL for `once` or exhausted
```

`initNag` is the earliest future series instant, with two corrections:

- if every instant is already past (downtime or a task created after its
  anchor): ack tasks nag immediately, informational tasks whose
  `anchor + resolve_window` has also passed are inserted already resolved;
- a future *repeating* instant inside quiet hours is clamped (§6); one-shot
  instants and the anchor are never moved.

Properties that fall out of this:

- **Restart-safe**: the cursor is persisted; nothing about the schedule lives in
  memory.
- **Advance notices work**: because materialisation is driven by the earliest
  plan instant rather than the anchor, a `-1w` phase has an occurrence to attach
  to. A one-off created today for next Friday exists in the database today, so
  the dashboard and `list_tasks` can show it.
- **No history spam**: after a long outage a monthly task creates one cycle, not
  seven. The previous pending occurrence, if any, is marked `missed`.
- **Duplicates are impossible**: `UNIQUE (task_id, due_at)` plus the existence
  check.
- **Disabling** sets `next_due_at = NULL`, stops future cycles, and resolves any
  open occurrence as `skipped` with note `task disabled`.
- **Editing the schedule** recomputes `next_due_at` from now. An already-open
  occurrence is left alone until it is resolved or a new cycle lands on it.

### Deadline

`deadline_at = due_at + grace_ms`, both stored. `grace_ms` defaults to 0, in
which case `deadline_at = due_at` and the plan's post-deadline phases apply
from the start (used by tasks that are urgent immediately).

## 6. Notification plans

A plan is an ordered JSON array stored in `task.phases`:

```json
[
  {"after":"0",    "every":"24h", "priority":5},
  {"after":"72h",  "every":"12h", "priority":8}
]
```

| Field | Meaning |
|---|---|
| `after` | Offset from `due_at`. May be negative (advance notices). Go duration syntax. |
| `every` | Repeat interval. `null` means the phase fires exactly once. |
| `priority` | Gotify priority 0–10, default 5. |

The union of all phases is a **static series of nag instants**:

```
series = ∪ phase p:  due_at + p.after + k·p.every   for k = 0,1,2,…  (deduplicated)
                                                      (k > 0 only when every != null)
```

`next_nag_at` is the earliest series instant *after* the last successful send —
or, at materialisation, the result of `initNag`; or, after a snooze, the chosen
instant. Because it is derived rather than tracked as a count, downtime,
snoozes, and edits never desynchronise anything.

### Defaults

| Task shape | Default plan |
|---|---|
| ack, grace > 0 | `[{0, 24h, 5}, {grace, 12h, 8}]` |
| ack, grace = 0 | `[{0, 12h, 8}]` |
| informational, recurring | `[{0, null, 5}]` |
| informational, once | `[{-24h, null, 5}]` |

### Quiet hours

Applied **only** to repeating series instants that are still in the future:

| Situation | Timing |
|---|---|
| Anchor (`due_at`) | exact |
| One-shot phase (`every: null`) | exact |
| Repeating phase (`every: N`) computed in the future | if inside 21:30–08:00, move to next window open (08:00) |
| Series instant already in the past (catch-up after downtime) | send immediately |
| Snooze preset | the chosen time |
| `task.respect_quiet_hours = 0` | no clamping ever |

Clamping is computed in the task's timezone, so it tracks DST. The stored
`next_nag_at` is always the clamped instant; nothing is re-evaluated at send
time.

### Snooze

The web UI offers `evening` (today 20:00, or tomorrow if past), `tomorrow`
(08:00 next day), `three_days` (date +3 at 08:00). MCP also accepts an RFC3339
instant. A snooze sets `next_nag_at` directly; phases keep advancing on
`elapsed-since-due_at`, so snoozing past a deadline returns at the escalated
priority. After the next send, `next_nag_at` is recomputed from the series,
so the skipped instants are gone rather than queued.

### Message text

Title: `{title} — {state}` where state is one of `due today`, `{n} days left`,
`{n} days overdue`, `day {nag_count+1}` (no deadline), `in {n} days`,
`tomorrow`, `today`.

Body:

```
{notes}

Scheduled Thu 12 Nov, 08:00
Deadline Sun 15 Nov, 08:00 (in 2 days)
Last done 12 Oct
```

Only non-empty lines are rendered. `Last done` is the `resolved_at` of the
previous `done` occurrence for the same task.

## 7. Delivery (Gotify)

`POST {KREM_GOTIFY_URL}/message` with header `X-Gotify-Key: <app token>`:

```json
{
  "title": "Contacts — 2 days left",
  "message": "Change contacts.\n\nScheduled Thu 12 Nov, 08:00\nDeadline Sun 15 Nov, 08:00 (in 2 days)\nLast done 12 Oct",
  "priority": 5,
  "extras": {
    "client::display": {"contentType": "text/plain"},
    "client::notification": {
      "click": {"url": "https://kreminders.nb.kling.dev/c/42.1795000000.8ZtQ…"}
    }
  }
}
```

Gotify has no action buttons, so the `click.url` is the only tap target and each
task gets its own notification. `priority` maps to Android notification
channels, which is where the escalation is actually perceptible:

| Range | Use |
|---|---|
| 0–3 | informational |
| 4–6 | normal nagging |
| 7–10 | overdue |

### Delivery outcomes

The response body's `{"id":N}` is stored on the `notification` row.

| Outcome | Behaviour |
|---|---|
| 2xx | record message id, `nag_count++`, `last_nag_at = now`, recompute `next_nag_at` |
| 4xx | record error, back off `1m, 5m, 15m, 1h` (`delivery_failures`), keep `last_nag_at` unchanged |
| 5xx / timeout | same as 4xx |
| Gotify not configured (`KREM_GOTIFY_URL=""`) | log-only sender; records `ok=1, gotify_message_id=NULL` |

Failures are never silent: the dashboard shows them and `/healthz` reports the
last failed delivery. After six consecutive failures the plan still retries
hourly; a phone notification is by definition the thing that can't report its
own failure.

### Cleanup on resolution

When an occurrence resolves, the server calls `DELETE /message/{id}` with the
**client token** for every notification of that occurrence with a non-null
message id. Best-effort: failures are logged, never retried, never block the
resolution. This is what makes confirming feel complete — the nag disappears
from the phone instead of lingering with a dead link.

### Duplicate suppression

Delivery is at-least-once. If the process dies between the Gotify 2xx and the
DB commit, one duplicate nag can appear. Accepted: a duplicate reminder is
much cheaper than a missed one. After a successful send the occurrence is
re-read; if it was resolved in the meantime (a Done click racing the nag), the
message is deleted immediately.

## 8. Confirmation

### Token

```
/c/{occurrence_id}.{expiry_unix}.{sig}
sig = base64url( HMAC-SHA256(signing_key, "token:v1:{occurrence_id}:{expiry}")[:16] )
```

- Generated per nag, and on demand from the dashboard or MCP.
- `expiry = max(due_at, deadline_at) + 30 days`.
- Scope is exactly one occurrence: an old notification can never complete a
  later cycle.
- Stateless; nothing to store or clean up. Rotating `KREM_SIGNING_KEY`
  invalidates outstanding links, which is low-impact because every nag carries
  a fresh one.
- A leaked token allows exactly one occurrence to be completed early — the same
  power the notification itself grants. There is no read access behind it.

### Routes

| Method | Path | Behaviour |
|---|---|---|
| `GET` | `/c/{token}` | Renders the occurrence. Never mutates. |
| `POST` | `/c/{token}/done` | Resolve `done`. Idempotent. |
| `POST` | `/c/{token}/snooze` | Body `until=evening\|tomorrow\|three_days`. |
| `POST` | `/c/{token}/skip` | Resolve `skipped`. |
| `GET` | `/` | Open items, delivery health, recent history. |
| `GET` | `/healthz` | 503 if DB ping fails or last tick > 5 min. |
| `POST` | `/mcp` | MCP streamable HTTP (bearer auth). |

All mutations are POST (link previews, browser prefetch, and Gotify client
prewarming all issue GETs). After a POST the server redirects `303` back to
`GET /c/{token}`, which renders the resolved state. No cookies, no sessions, no
JS; plain HTML forms so the Android custom tab works.

### Page states

| Condition | Response |
|---|---|
| valid, pending | 200 — title, notes, scheduled, deadline (countdown), last done, 3 snooze buttons |
| valid, resolved | 200 — "Done on Sun 15 Nov, 20:14 (2 days late)", plus the next cycle's date |
| valid, expired, pending | 410 — "This link expired. Use the newest notification." + dashboard link |
| bad signature / unknown id | 404 — generic "unknown link" (no oracle for valid-but-unsigned) |

Response headers on every `/c/*` route: `Cache-Control: no-store`,
`Referrer-Policy: no-referrer`, `X-Content-Type-Options: nosniff`,
`Content-Security-Policy: default-src 'none'; style-src 'unsafe-inline';
form-action 'self'; base-uri 'none'; frame-ancestors 'none'`.

The reverse proxy must forward POST bodies and methods unmodified and must not
cache `/c/*` (see `ops.md`).

## 9. MCP

- Transport: MCP streamable HTTP (`mcp.NewStreamableHTTPHandler`) mounted at
  `/mcp` on the main listener. `JSONResponse: true`, no SSE requirement for
  clients that don't want it, stateless mode off (session ids are harmless).
- Auth: static bearer token (`KREM_MCP_TOKEN`), compared in constant time via
  the SDK's `RequireBearerToken`. Unauthenticated requests get 401.
- No stdio subcommand: agents run on the LAN and talk to the daemon, and the
  daemon already owns the DB. This keeps one writer and one code path.
- Tools are thin wrappers over the same service layer the web handlers use;
  there is no second implementation of any rule. Full reference in `mcp.md`.

Agent-facing additions that the web UI doesn't need:

- `complete_task` / `skip_task` / `snooze_task` accept either `slug` (meaning
  "the currently open cycle") or an explicit `occurrence_id`.
- `confirm_url` returns a signed link without sending a notification, so the
  assistant can paste a tappable link into chat.
- `send_test_notification` validates Gotify credentials end to end.
- `history` answers "when did I last change my contacts?" — the question that
  motivated the whole system.

## 10. Configuration

Environment only, with `_FILE` variants for every secret (contents read at
startup, trailing whitespace trimmed). `_FILE` wins if both are set.

| Variable | Default | Notes |
|---|---|---|
| `KREM_BASE_URL` | — (required) | `https://kreminders.nb.kling.dev` |
| `KREM_LISTEN` | `127.0.0.1:5736` | loopback behind the proxy |
| `KREM_DB` | `/var/lib/kreminders/kreminders.db` | `StateDirectory` in the unit |
| `KREM_TZ` | `America/Chicago` | default task timezone |
| `KREM_DEFAULT_TIME` | `08:00` | time used when a date is given without one (`YYYY-MM-DD` for `once_at`) |
| `KREM_QUIET_HOURS` | `21:30-08:00` | `off` disables globally |
| `KREM_GOTIFY_URL` | — | empty string ⇒ log-only sender |
| `KREM_GOTIFY_APP_TOKEN(_FILE)` | — | `POST /message` |
| `KREM_GOTIFY_CLIENT_TOKEN(_FILE)` | — | delete-own messages; empty ⇒ no cleanup |
| `KREM_SIGNING_KEY(_FILE)` | — (required) | 32 bytes, hex |
| `KREM_MCP_TOKEN(_FILE)` | — | empty ⇒ `/mcp` disabled (404) |
| `KREM_LOG_LEVEL` | `info` | `debug`, `info`, `warn`, `error` |

Refuses to start if the DB is unreachable, `KREM_BASE_URL` is unset or has a
trailing slash, or `KREM_SIGNING_KEY` is not 32 bytes. An unset MCP token is a
warning, not a failure (the confirm page still works).

### Subcommands

| Command | Purpose |
|---|---|
| `kreminders serve` | default; scheduler + HTTP |
| `kreminders migrate` | apply migrations and exit (also run at serve startup) |
| `kreminders test-notify` | send one test notification, print the Gotify message id |
| `kreminders version` | version + build info |

## 11. Failure modes

| Failure | Effect | Handling |
|---|---|---|
| Gotify down / 401 | nags not delivered | backoff, dashboard + `/healthz` report it, hourly retry |
| Gotify client token missing | notifications not cleaned up | non-fatal, logged once at startup |
| SQLite `SQLITE_BUSY` | write contention | `busy_timeout=5000`, single writer, `BEGIN IMMEDIATE` for multi-statement writes |
| Disk full | writes fail | `/healthz` 503; no data loss beyond the failed write |
| Process crash mid-send | possible duplicate nag | accepted (at-least-once) |
| Restart after downtime | nags due while down | one immediate nag per occurrence, no burst |
| Month-end cron | `31` skipped in short months | accepted, documented |
| DST transition | wall-clock drift for `@every`, none for cron | covered by tests |
| Netbird down on phone | confirm link unreachable | server-side unaffected; dashboard works when back |
| Proxy down | links + MCP down | app keeps nagging; `/healthz` reachable locally |
| Signing key rotated | outstanding links 410 | next nag issues a fresh link |
| Token leaked | one occurrence can be completed | bounded by scope + 30-day expiry |
| Token in notification history | readable by anyone with the unlocked phone | accepted: capability is equivalent to the notification |

## 12. Decision log

| Decision | Chosen | Alternatives rejected, and why |
|---|---|---|
| Storage | SQLite (`modernc.org/sqlite`, WAL) | Postgres: a service, role, secret and backup story for ~10 writes/day and one writer. Revisit only if multi-host writing appears. |
| Timestamps | UTC epoch ms in DB, cron in task TZ | `timestamptz`/local strings: DST bugs, ambiguous comparisons. |
| Schedule model | cron + `@every`, or `once` at an instant | RRULE (heavy dependency for needs we don't have); home-grown DSL (`monthly:12`) is less expressive than cron for `nth weekday`. |
| Reminder state | `occurrence` rows | `last_completed_at` on the task: no history, no skip-vs-miss, no per-cycle note or token. |
| Nag policy | static series from phases | Stored "nag count"/"next nag at" as the only state: desynchronises on snooze, edit, downtime. In-memory cron jobs: lost on restart, no catch-up. |
| Post-deadline | keep nagging, higher priority | One-shot escalation, or auto-miss: silently stops caring, which defeats the purpose. |
| Quiet hours | clamp future repeats only | Clamping everything: the dentist reminder at 07:00 or a rare off-hours notice would be lost. No quiet hours at all: 03:00 nags. |
| Delivery | Gotify click URL | ntfy: real action buttons would remove a tap, but Gotify is already deployed and the tap is not the bottleneck. |
| Confirmation | page + POST | GET-completes: prefetch and mis-taps complete tasks; an undo window is extra state for less safety. |
| Tokens | HMAC, stateless, per-occurrence, expiring | DB-stored tokens: cleanup job for no benefit; per-nag rows: same link power, more writes. |
| MCP transport | streamable HTTP only | stdio: duplicate code path, second writer, no benefit for LAN agents. |
| Port | `127.0.0.1:5736` | 8080 is often taken; loopback means collisions are cosmetic anyway. |
| Process model | one binary, one unit | Separate scheduler/web/MCP binaries: three deployments for one user. |

## 13. Future work

Roughly in order of likely value: multi-user and per-person Gotify apps;
`week-of-month` and `nth-weekday` helpers; snooze-until-arbitrary-date in the
web UI; a weekly digest notification; litestream for continuous backup; an
`ntfy` sender behind the same interface; a "vacation mode" that shifts deadlines
by N days; CSV import of old paper reminders.
