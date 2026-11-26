# kreminders

Household reminders that nag until the thing is actually done.

A recurring or one-off task (contacts on the 12th, a new toothbrush head, a
dentist appointment) becomes a Gotify notification on your phone. Tapping it
opens a small web page with **Done / Snooze / Skip**. Different tasks nag at
different rates, deadlines make the nagging louder rather than stopping, and
everything can be managed from an AI agent over MCP.

One Go binary, one SQLite file, one systemd unit. No containers.

## Status

Design is complete; implementation is in progress, tracked as tickets:

| Document | Contents |
|---|---|
| [`docs/design.md`](docs/design.md) | architecture, domain model, scheduling, nag policy, confirmation, decisions |
| [`docs/schema.md`](docs/schema.md) | SQLite DDL, invariants, worked examples |
| [`docs/mcp.md`](docs/mcp.md) | MCP endpoint and tool reference |
| [`docs/ops.md`](docs/ops.md) | NixOS unit, secrets, proxy, backups, runbook |
| [`docs/tickets/`](docs/tickets/README.md) | step-by-step implementation plan |

The design docs are the specification. If the code proves them wrong, the doc
changes in the same commit.

## Layout

```
cmd/kreminders/     entry point: subcommand dispatch only
internal/           config, domain, store, plan, gotify, scheduler, web, mcp
docs/               design, schema, MCP, ops, tickets
```

## Development

```sh
go test ./... -race
go vet ./...
gofmt -l .
```

Running locally needs no Gotify server: with `KREM_GOTIFY_URL` unset the sender
logs instead of sending. See [`docs/design.md`](docs/design.md) §10 for the
environment variables.

## Deployment

Native NixOS service on `tito`, reachable at
`https://kreminders.nb.kling.dev` over Netbird. See
[`docs/ops.md`](docs/ops.md).

## License

MIT — see [`LICENSE`](LICENSE).
