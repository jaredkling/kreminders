# kreminders — MCP

| | |
|---|---|
| Endpoint | `https://kreminders.nb.kling.dev/mcp` |
| Transport | MCP streamable HTTP (POST, JSON-RPC 2.0) |
| Auth | `Authorization: Bearer $KREMINDERS_MCP_TOKEN` |
| Sessions | session ids supported; each request is independent for our purposes |
| Tools | 13, all thin wrappers over the same service layer as the web UI |

## 1. Connecting

### opencode

```jsonc
// opencode.json
{
  "$schema": "https://opencode.ai/config.json",
  "mcp": {
    "kreminders": {
      "type": "remote",
      "url": "https://kreminders.nb.kling.dev/mcp",
      "headers": { "Authorization": "Bearer {env:KREMINDERS_MCP_TOKEN}" },
      "enabled": true
    }
  }
}
```

`{env:VAR}` is substituted from the environment at load time. If your client
doesn't support substitution, paste the token into a file with `0600`
permissions and reference it (`{file:...}`) rather than hard-coding it in a
shared config.

### Claude Desktop

Add a **Connector** with the URL above and the `Authorization` header. Clients
that only speak stdio need an `mcp-remote`-style bridge pointed at the same URL.

### curl sanity check

```sh
curl -sS https://kreminders.nb.kling.dev/mcp \
  -H "Authorization: Bearer $KREMINDERS_MCP_TOKEN" \
  -H 'Content-Type: application/json' \
  -H 'Accept: application/json, text/event-stream' \
  -d '{"jsonrpc":"2.0","id":1,"method":"initialize","params":{
        "protocolVersion":"2025-11-25","capabilities":{},
        "clientInfo":{"name":"curl","version":"0"}}}'
```

A `401` means the bearer token is wrong; `404` means the server was started
without `KREM_MCP_TOKEN` (the endpoint is disabled rather than open).

## 2. Conventions

- **Identity**: every task has a `slug` (`lowercase-with-dashes`) that is stable
  across schedule edits. Tools accept `slug` or numeric `id`; mutations also
  accept `occurrence_id` to target a specific cycle.
- **Timestamps** in responses are RFC 3339 with the task's UTC offset
  (`2026-11-12T08:00:00-06:00`). Inputs accept RFC 3339 with an offset, or a
  bare `YYYY-MM-DD` (interpreted as 08:00 in the task timezone).
- **Durations** accept `ms`, `s`, `m`, `h`, `d`, `w` — `72h`, `3d`, `1w`.
  Internally everything is milliseconds.
- **Schedules** accept 5-field cron (`0 8 12 * *`, `0 19 * * 1`) or
  `@every 72h`. `@monthly`-style descriptors are not supported; use cron.
- **Reads are safe**, mutations are idempotent where it matters (see §5).
- **Errors** are returned as MCP tool results with `isError: true` and a short
  human-readable message (`contacts is already done (resolved 2 days ago)`).
  Protocol-level errors are reserved for malformed arguments.

## 3. Tools

### `list_tasks`

List definitions with their next cycle and open state.

| Arg | Type | Default | |
|---|---|---|---|
| `include_disabled` | bool | `false` | include tasks with `enabled = 0` |
| `needs_ack` | bool \| null | null | filter actionable vs informational |

```jsonc
{
  "tasks": [{
    "id": 1, "slug": "contacts", "title": "Contacts",
    "kind": "recurring", "schedule": "0 8 12 * *",
    "timezone": "America/Chicago",
    "needs_ack": true,
    "grace": "72h",
    "enabled": true,
    "next_due_at": "2026-12-12T08:00:00-06:00",
    "last_done_at": "2026-11-13T09:20:00-06:00",
    "open": null
  }]
}
```

### `get_task`

| Arg | Type | |
|---|---|---|
| `slug` | string | one of `slug`/`id` required |
| `id` | int | |
| `history_limit` | int (`10`) | occurrences to include |

Returns the task, its parsed `phases`, and the most recent occurrences
(newest first) with status, resolution, note, and timing.

### `create_task`

| Arg | Type | Default | Notes |
|---|---|---|---|
| `title` | string | required | |
| `slug` | string | derived from title | validated, unique |
| `kind` | `"recurring"`\|`"once"` | `"recurring"` | |
| `schedule` | string | — | required for `recurring`; cron or `@every` |
| `once_at` | string | — | required for `once`; RFC 3339 or `YYYY-MM-DD` |
| `timezone` | string | `KREM_TZ` | IANA name |
| `needs_ack` | bool | `true` | |
| `grace` | duration | `"0"` | deadline = due + grace |
| `phases` | array | derived (§4 of `design.md`) | `{after, every, priority}` |
| `notes` | string | `""` | shown in the notification and on the page |
| `respect_quiet_hours` | bool | `true` | |
| `resolve_window` | duration | `"12h"` | informational tasks only |

Refuses `needs_ack: false` combined with phases that repeat more than daily
(meaningless noise) — a warning in the response, not an error.

```jsonc
// create_task
{ "title": "Dentist appointment", "kind": "once",
  "once_at": "2027-04-15T09:00-05:00",
  "needs_ack": false,
  "notes": "Dr. Ruiz, 4th floor. Bring insurance card.",
  "phases": [{"after":"-1w","every":null,"priority":5},
             {"after":"-24h","every":null,"priority":5},
             {"after":"0","every":null,"priority":6}] }

// response
{ "id": 4, "slug": "dentist-appointment", "next_due_at": "2027-04-15T09:00:00-05:00",
  "phases": [ ... ], "confirm_url": null }
```

### `update_task`

Partial update: send only fields to change (same names as `create_task`, plus
`enabled`). Changing `schedule`/`once_at` recomputes `next_due_at` from now; an
already-open occurrence is **not** discarded. Changing `phases` takes effect at
the next send (the current `next_nag_at` is recomputed immediately). Changing
`grace` only affects future occurrences.

### `set_task_enabled`

| Arg | Type | |
|---|---|---|
| `slug`/`id` | | required |
| `enabled` | bool | required |

Disabling stops future cycles and resolves an open occurrence as
`skipped` with note `task disabled`; re-enabling recomputes the next cycle.

### `delete_task`

| Arg | Type | |
|---|---|---|
| `slug`/`id` | | required |
| `confirm` | bool | must be `true` |

Hard delete, cascades history. Prefer `set_task_enabled` for "I'm done with
this"; deletion is for mistakes.

### `list_open`

No arguments (optional `overdue_only`). Pending occurrences across all tasks,
sorted by deadline (then due date). Only materialised cycles appear, so an
advance notice becomes visible once its `lead` window opens (a `-1w` phase is
listed a week ahead); anything further out is visible via `list_tasks`.

```jsonc
{
  "open": [{
    "occurrence_id": 2, "slug": "contacts", "title": "Contacts",
    "due_at": "2026-11-12T08:00:00-06:00",
    "deadline_at": "2026-11-15T08:00:00-06:00",
    "days_left": -1,           // negative = days late
    "overdue": true,
    "next_nag_at": "2026-11-16T08:00:00-06:00",
    "nag_count": 4,
    "confirm_url": "https://kreminders.nb.kling.dev/c/2.…"
  }]
}
```

### `complete_task`

| Arg | Type | Default | |
|---|---|---|---|
| `slug` | string | | current open cycle |
| `id` | int | | |
| `occurrence_id` | int | | explicit cycle (e.g. from `history`) |
| `note` | string | `""` | stored on the occurrence |
| `completed_at` | string | now | backdating is allowed, must be ≥ `due_at` − 30d |

```jsonc
{ "slug": "contacts", "note": "Left eye pair" }
// →
{ "occurrence_id": 2, "status": "done",
  "resolved_at": "2026-11-13T09:20:00-06:00",
  "was_already_resolved": false,
  "next_due_at": "2026-12-12T08:00:00-06:00" }
```

Idempotent: completing an already-`done` occurrence returns
`was_already_resolved: true` and the original resolution, without an error.
Completing a `missed` cycle is an error (`that cycle was missed on 12 Dec;
use history or target occurrence_id`).

### `snooze_task`

| Arg | Type | |
|---|---|---|
| `slug`/`id`/`occurrence_id` | | required |
| `until` | string | `evening`, `tomorrow`, `three_days`, or RFC 3339 |

Sets `next_nag_at`; phases keep advancing, so snoozing past a deadline returns
at the escalated priority. Snoozing a non-pending occurrence is an error.

### `skip_task`

| Arg | Type | |
|---|---|---|
| `slug`/`id`/`occurrence_id` | | required |
| `note` | string | optional |

Resolves `skipped` (this cycle only; the next cycle is unaffected).

### `confirm_url`

| Arg | Type | |
|---|---|---|
| `slug`/`id`/`occurrence_id` | | required |

Returns a signed link without sending a notification. Use it when the user asks
for something tappable in chat:

```jsonc
{ "slug": "toothbrush" }
// → { "occurrence_id": 3, "url": "https://kreminders.nb.kling.dev/c/3.…",
//      "expires_at": "2026-12-20T08:00:00-06:00" }
```

### `history`

| Arg | Type | Default | |
|---|---|---|---|
| `slug` | string | all tasks | |
| `limit` | int | `20` | max 200 |
| `since` | string | — | RFC 3339 lower bound on `due_at` |

```jsonc
{
  "history": [{
    "occurrence_id": 2, "slug": "contacts",
    "due_at": "2026-11-12T08:00:00-06:00",
    "status": "done", "resolution": "done",
    "resolved_at": "2026-11-13T09:20:00-06:00",
    "days_late": 0,
    "note": "Left eye pair"
  }, {
    "occurrence_id": 1, "slug": "toothbrush",
    "due_at": "2026-11-12T08:00:00-06:00",
    "status": "done", "resolution": "done",
    "resolved_at": "2026-11-20T21:05:00-06:00",
    "days_late": 0, "note": null
  }]
}
```

`days_late` is relative to `deadline_at` when present, otherwise to `due_at`.

### `send_test_notification`

No arguments. Sends one message ("kreminders test") at priority 3 through the
configured Gotify credentials and returns `{"ok": true, "message_id": 123}` or
the Gotify error verbatim. Use after changing tokens, not as a health check.

## 4. Example dialogues

**"I changed my contacts"**

```
list_open                      → contacts pending, occurrence_id 2
complete_task(slug="contacts", note="Left eye pair")
```
Assistant reports when it was due, whether it was on time, and the next cycle
date (`12 Dec`).

**"Remind me about the dentist on April 15th at 9, and a week before"**

```
create_task(title="Dentist appointment", kind="once",
            once_at="2027-04-15T09:00-05:00", needs_ack=false,
            phases=[{"after":"-1w","every":null,"priority":5},
                    {"after":"-24h","every":null,"priority":5},
                    {"after":"0","every":null,"priority":6}])
```

**"What's overdue?"**

```
list_open(overdue_only=true)
```
Returns contacts 1 day late and the toothbrush at day 8 of 8; the assistant
suggests doing the contacts today.

**"When did I last change my contacts?"**

```
history(slug="contacts", limit=3)
```
→ `12 Nov → 9 Oct → 14 Sep`, and it can compute the average lateness.

**"I'm travelling, push the toothbrush to next week"**

```
snooze_task(slug="toothbrush", until="2026-11-27T08:00:00-06:00")
```

The three presets (`evening`, `tomorrow`, `three_days`) cover the web page;
agents pass an explicit instant when you name one.

## 5. Error and idempotency matrix

| Case | Result |
|---|---|
| Unknown slug | error: `task "foo" not found` |
| No open occurrence when completing by slug | error listing today's open tasks |
| Complete an already-done cycle | success, `was_already_resolved: true` |
| Complete a missed/skipped cycle | error, hints at `history` |
| Snooze/skip a resolved cycle | error |
| Duplicate `create_task` with an existing slug | error (`slug already in use`) |
| Invalid cron/phases/timezone | error naming the offending field |
| Two concurrent completes | one wins; the other reports `was_already_resolved: true` |
| Gotify down during `send_test_notification` | `ok: false` with the HTTP status and body |

## 6. Security notes

- The bearer token grants full read/write over every task and history entry.
  It is stored in each agent's config, so treat those configs as secrets
  (`0600`, not in a git repo).
- Rotating `KREM_MCP_TOKEN` requires restarting the service and updating every
  client; nothing else depends on it.
- The endpoint is only reachable through Netbird (plus the LAN, if the proxy
  listens there). It must never be exposed publicly without TLS and a token.
- `confirm_url` hands out a capability link. It is scoped to one occurrence and
  expires, but an agent that logs its tool output has effectively logged a
  usable link. Avoid dumping URLs into shared transcripts.
