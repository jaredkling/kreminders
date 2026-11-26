# Ticket 10 — Deploy

**Depends on:** 09
**Effort:** one weekend session, mostly YAML/Nix
**Read first:** [`../ops.md`](../ops.md) (all of it), [`../design.md`](../design.md) §10–§11

## Goal

`kreminders` runs as a hardened systemd service on `tito`, starts at boot,
survives reboots, reads its secrets from agenix (or sops-nix), is reachable at
`https://kreminders.nb.kling.dev` through the existing reverse proxy, has a
nightly verified backup, and passes the project-level acceptance run in the
tickets README.

## Why now

Everything you tested ran from `go run` with a temp database. This ticket is
where the thing becomes yours: the real DB path, the real tokens, the real
phone.

## What you'll learn

| Concept | Where to look |
|---|---|
| NixOS modules and option merging | [NixOS manual: modules](https://nixos.org/manual/nixos/stable/#sec-writing-modules) |
| `buildGoModule` and `vendorHash` | [nixpkgs Go section](https://nixos.org/manual/nixpkgs/stable/#sec-language-go) |
| systemd hardening options | `man systemd.exec`, `systemd-analyze security` |
| `StateDirectory` semantics | `man systemd.exec` (`StateDirectory=`) |
| agenix / sops-nix basics | the projects' READMEs, `ops.md` §4 |
| Reverse proxy streaming | `man nginx` (`proxy_buffering`), Caddy's `reverse_proxy` docs |
| SQLite online backup | `sqlite3 ... "VACUUM INTO ..."`, `PRAGMA integrity_check` |

## Scope

**In**

- A `modules/kreminders.nix` (or inline in your host config) following
  `ops.md` §2: package, system user, unit, `StateDirectory`, hardening,
  restart policy, environment from secret file paths.
- Secrets created in your existing agenix/sops store and referenced as `_FILE`
  paths. Verify no token appears in `systemctl show kreminders` or the journal.
- The reverse-proxy block from `ops.md` §3, including no buffering on `/mcp`
  and no caching.
- The backup service + timer from `ops.md` §6, with retention and an integrity
  check.
- A restore drill performed at least once.
- First-run checklist from `ops.md` §5 executed in order.

**Out**

- Containerisation (explicitly not wanted).
- Monitoring beyond `/healthz` and the journal.
- Off-host backup replication (note it as a follow-up if you care).

## Acceptance criteria

- [ ] `nix build .#kreminders` (or your flake's equivalent) succeeds with a real
      `vendorHash`; confirm it's not `lib.fakeHash`.
- [ ] `systemctl status kreminders` shows active, and `journalctl -u kreminders`
      shows startup without secrets and without errors.
- [ ] After `nixos-rebuild switch` the service is up; after a reboot, it comes
      back with the same data (occurrence count identical, `last_tick` fresh).
- [ ] `ss -ltnp | grep 5736` shows only `127.0.0.1` and only the service user.
- [ ] `systemd-analyze security kreminders` — note the exposure score in your
      notes and be able to explain the remaining findings rather than just
      silencing them.
- [ ] `systemctl show kreminders -p Environment` contains paths, not tokens.
      `journalctl -u kreminders | grep -i` for each token value finds nothing.
- [ ] `curl -s https://kreminders.nb.kling.dev/healthz` from another mesh
      machine returns ok; MCP tool calls through the proxy work (no timeouts,
      no buffering artefacts).
- [ ] `sudo systemctl start kreminders-test-notify` delivers to the phone, and
      tapping the dashboard link loads over HTTPS.
- [ ] The nightly backup ran: a file exists, is newer than 25h, and
      `PRAGMA integrity_check` returns `ok` on it.
- [ ] Restore drill: copy a snapshot to a scratch path, point a
      one-off `KREM_DB` at it, start a second instance on a different port, and
      confirm the data is intact — without stopping production.
- [ ] The project-level acceptance run in the tickets README passes, including
      the one-tap Done and the notification disappearing.
- [ ] A `pkill -9` of the service leaves the DB consistent on restart (WAL
      recovers), and no more than one duplicate nag is observed.

## Suggested approach

1. Package first, no module: get `buildGoModule` to build your binary and run
   it by hand with explicit env vars, using a throwaway DB under `/tmp`. Fix the
   `vendorHash` loop now, when the only variable is the build.
2. Convert to a unit with `StateDirectory`, run it, then add hardening one
   option at a time, restarting after each. If you add ten options at once and
   it fails, you won't know which one.
3. Add secrets last in this sequence: create files, confirm they exist with the
   right owner/mode, point the env vars at them, restart, check the journal.
   Hash the token values (`sha256sum`) so you can prove "not in logs" without
   putting the values in your shell history.
4. Do the proxy next. Test with `curl -i` before touching the phone; check the
   `/mcp` path with an `initialize` call specifically, because buffering bugs
   show up there and nowhere else.
5. Add the backup timer, wait for one run (or trigger it manually), verify the
   snapshot, then do the restore drill. A backup you haven't restored is a
   rumour.
6. Finally, execute the first-run checklist and the acceptance run.

## Questions to answer

- Why does the unit take `KREM_*_FILE` paths instead of values in
  `Environment=`? What would `systemctl show` reveal in the other design?
- `StateDirectory` vs `ReadWritePaths` vs just `ProtectSystem=true`: which
  combination actually keeps the service from writing anywhere else, and what
  breaks first when you get it wrong?
- Where does timezone data come from inside the sandbox? (You imported
  `time/tzdata` in ticket 04 — verify it's really used, e.g. by removing
  `TZDIR` from the environment.)
- If the DB path moves out of `StateDirectory`, which unit options break?
- Does your `Restart=always` hide a crash loop? How would you notice (journal
  timestamps, `systemctl show -p NRestarts`)?
- What is the rollback plan if a migration is wrong? `ops.md` says restore a
  backup — is the last snapshot recent enough, and does the older binary accept
  it?

## Pitfalls

- `lib.fakeHash` left in the module: the build fails with the real hash
  printed; paste it, then remember to update it when dependencies change.
- Writing the env in the module but forgetting that `age.secrets` needs
  `owner = "kreminders"` and `mode = "0400"`, then debugging a permission error
  as a parse error.
- `%` in a systemd `script` being interpreted as a specifier (the backup date
  format is the classic casualty — `ops.md` uses `%%F`).
- A proxy with buffering on `/mcp` that works for `tools/list` and hangs on a
  slow tool call.
- Binding the app to `0.0.0.0` "just to test", and forgetting.
- `ProtectHome=true` plus a DB path someone decided to put under `/home`.
- A backup timer without `Persistent=true` that never runs because the laptop
  was asleep at 03:30.
- Restoring the main DB from a snapshot without removing `-wal`/`-shm`; SQLite
  will happily mix them.
- Putting a token into a shell command that lands in `.bash_history`; use files
  and `$(cat ...)` only when necessary, and prefer `systemctl start`.

## Testing checklist

- [ ] Fresh install on an empty `StateDirectory`: DB created, migrations
      applied, version 1.
- [ ] Reboot: service healthy, data intact, heartbeat fresh.
- [ ] Kill -9: recovers, no corruption (`PRAGMA integrity_check`), duplicates
      bounded as documented.
- [ ] Secrets: correct owner/mode; not in env; not in logs; rotation followed
      once end-to-end (rotate the app token, restart, `test-notify`).
- [ ] Proxy: HTTPS page, MCP call, `no-store` header visible via `curl -i`.
- [ ] Backup: file created on schedule, integrity ok, restore drill succeeded.
- [ ] Acceptance run from the tickets README.
- [ ] Update your `NOTES.md`: what you'd tell yourself at the start of this
      ticket.

## When you're stuck

If the unit starts and immediately exits, run it manually with the same
environment (a one-off `systemd-run` unit with `ExecStart`/`Environment` copied
from the real one, or a `bash -c` as the service user) and read the real error. If a file is "permission denied" rather than
"not found", check both the file mode *and* every parent directory. If the
service is healthy but the phone can't reach it, prove each hop separately:
loopback `curl`, then the proxy on the host, then the mesh name from another
machine, then the phone. The bug is always the hop you skipped.
