# Implementation tickets

Ten tickets that take `kreminders` from an empty `main.go` to the design in
[`../design.md`](../design.md). They are ordered so every ticket ends in a
state you can run, test, and be proud of. None of them contain the code — the
point is to work it out. Where a ticket says "look up X", that is the expected
way to make progress.

## How to use these

1. Read [`../design.md`](../design.md) once, all of it. Skim
   [`../schema.md`](../schema.md) and keep it open while you code — it is the
   reference, not the tutorial.
2. Do the tickets **in order**. Each has "Depends on" at the top; the only
   exception is ticket 06 (Gotify), which can be done any time after 04 if you
   want a break from the scheduler.
3. Branch per ticket (`git switch -c 04-plan-math`), and commit whenever an
   acceptance box gets ticked. Small commits, boring messages.
4. Run the three checks before calling a ticket done:

   ```sh
   gofmt -l .        # must print nothing
   go vet ./...
   go test ./... -race -count=1
   ```

5. If the implementation proves a doc wrong, fix the doc in the same commit.
   The docs are the spec; the code is the proof.
6. Keep a scratch `NOTES.md` (gitignored or not, your call) with "what confused
   me / what I'd do differently". On a first Go project this is worth more than
   the code.

## Environment

| Tool | Check | Notes |
|---|---|---|
| Go | `go version` | 1.26 already installed |
| sqlite3 | `sqlite3 --version` | `nix shell nixpkgs#sqlite` if missing |
| curl, jq | `curl --version`, `jq --version` | for manual endpoint checks |
| nix | `nix --version` | only needed for ticket 10 |

Useful commands you'll reach for constantly:

```sh
go run ./cmd/kreminders version
go run ./cmd/kreminders serve
go test ./internal/plan -run TestQuiet -v
go test ./... -race
go doc time.Time.UnixMilli
go doc net/http.Server
go get github.com/robfig/cron/v3
```

## Order and dependencies

| # | Ticket | Depends on | Go focus |
|---|---|---|---|
| 01 | [Bootstrap](01-bootstrap.md) | — | packages, modules, `log/slog`, `net/http`, signals, env config |
| 02 | [SQLite and migrations](02-sqlite-and-migrations.md) | 01 | `database/sql`, `embed`, transactions, `t.TempDir` |
| 03 | [Domain and storage](03-domain-and-storage.md) | 02 | structs, errors, `sql.Null*`, JSON in columns |
| 04 | [Plan math](04-plan-math.md) | 03 | pure functions, `time` and DST, table-driven tests |
| 05 | [Materialiser](05-materialiser.md) | 04 | `sql.Tx`, integration tests, injected clock |
| 06 | [Gotify](06-gotify.md) | 03, 04 | interfaces, `http.Client`, `httptest`, retries |
| 07 | [Scheduler loop](07-scheduler-loop.md) | 05, 06 | goroutines, tickers, `context`, the race detector |
| 08 | [Confirmation web](08-confirmation-web.md) | 07 | `html/template`, HMAC, middleware, redirects |
| 09 | [MCP server](09-mcp-server.md) | 08 | MCP SDK, generics, bearer auth |
| 10 | [Deploy](10-deploy.md) | 09 | NixOS modules, systemd hardening, secrets |

Rough effort: 01–03 are evenings; 04 and 05 are the intellectual core (two or
three sessions each); 06–08 are steady; 09 is a new library; 10 is plumbing with
a satisfying payoff.

## Suggested package layout

```
cmd/kreminders/main.go       subcommand dispatch only; no logic
internal/config/             env + *_FILE loading, validation
internal/domain/             Task, Occurrence, Plan types; validation; no SQL
internal/store/              SQLite connection, migrations, queries
internal/plan/               pure schedule/nag/series math (no I/O)
internal/gotify/             Sender interface + HTTP implementation
internal/scheduler/          the tick loop: materialise -> deliver
internal/web/                confirm pages, dashboard, tokens, templates
internal/mcp/                tool registration
internal/store/migrations/   embedded .sql files
```

Why this shape: `internal/` stops anything outside the module importing your
code; `cmd/` keeps `main` trivial; every package exists because something
changes independently (SQL vs time math vs HTTP). If you find yourself wanting a
`utils` package, that's usually two packages or none.

Suggested convention: each package exposes what the next ticket needs and
nothing more. You will refactor; that's normal and cheap in Go.

## Project-level definition of done

All ten tickets ticked, plus this acceptance run against the real deployment:

- [ ] The four tasks from [`../schema.md`](../schema.md) §5 exist, created via
      MCP with natural-language instructions to your agent.
- [ ] A nag for `contacts` arrives on the phone at 08:00 America/Chicago.
- [ ] Tapping it opens HTTPS, Done resolves it, and the notification disappears
      from the phone.
- [ ] `list_open` shows the toothbrush with the correct days-left.
- [ ] Stopping the service for a day and starting it again sends exactly one
      catch-up nag per open occurrence.
- [ ] The nightly backup file passes `PRAGMA integrity_check`.
