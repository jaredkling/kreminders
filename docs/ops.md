# kreminders — Operations

| | |
|---|---|
| Host | NixOS (`tito`), native systemd unit, no containers |
| Binary | `/nix/store/…-kreminders/bin/kreminders` |
| Service user | `kreminders` (system user) |
| Listener | `127.0.0.1:5736` |
| Public URL | `https://kreminders.nb.kling.dev` (existing reverse proxy terminates TLS) |
| State | `/var/lib/kreminders/` (`StateDirectory`) |

## 1. Layout

```
/var/lib/kreminders/           0750 kreminders:kreminders
├── kreminders.db              SQLite (WAL + -wal/-shm siblings)
└── backup/
    ├── kreminders-2026-11-24.db
    └── kreminders-2026-11-25.db
```

Nothing else is written. The binary is stateless and read-only outside
`StateDirectory`; secrets are read once at startup from files.

## 2. Package and unit

```nix
# modules/kreminders.nix
{ config, lib, pkgs, ... }:
let
  kreminders = pkgs.buildGoModule {
    pname = "kreminders";
    version = "0.1.0";
    src = ./..;                 # repo root
    vendorHash = lib.fakeHash;  # replace with the hash from the first failed build
  };
in {
  users.groups.kreminders = { };
  users.users.kreminders = {
    isSystemUser = true;
    group = "kreminders";
    home = "/var/lib/kreminders";
  };

  systemd.services.kreminders = {
    description = "kreminders — household reminders";
    wantedBy = [ "multi-user.target" ];
    wants = [ "network-online.target" ];
    after = [ "network-online.target" ];

    environment = {
      KREM_BASE_URL = "https://kreminders.nb.kling.dev";
      KREM_GOTIFY_URL = "https://notifications.kling.dev";
      KREM_TZ = "America/Chicago";
      KREM_DEFAULT_TIME = "08:00";
      KREM_QUIET_HOURS = "21:30-08:00";
      KREM_LOG_LEVEL = "info";
      # secrets as paths, never values (see §4)
      KREM_GOTIFY_APP_TOKEN_FILE = config.age.secrets.kreminders_gotify_app.path;
      KREM_GOTIFY_CLIENT_TOKEN_FILE = config.age.secrets.kreminders_gotify_client.path;
      KREM_SIGNING_KEY_FILE = config.age.secrets.kreminders_signing.path;
      KREM_MCP_TOKEN_FILE = config.age.secrets.kreminders_mcp.path;
    };

    serviceConfig = {
      ExecStart = "${kreminders}/bin/kreminders serve";
      User = "kreminders";
      Group = "kreminders";
      StateDirectory = "kreminders";
      StateDirectoryMode = "0750";
      UMask = "0077";
      Restart = "always";
      RestartSec = 2;
      TimeoutStopSec = 20;

      # hardening
      NoNewPrivileges = true;
      PrivateTmp = true;
      PrivateDevices = true;
      ProtectSystem = "strict";
      ProtectHome = true;
      ProtectClock = true;
      ProtectKernelTunables = true;
      ProtectKernelModules = true;
      ProtectKernelLogs = true;
      ProtectControlGroups = true;
      ProtectProc = "invisible";
      ProcSubset = "pid";
      RestrictAddressFamilies = [ "AF_INET" "AF_INET6" "AF_UNIX" ];
      RestrictNamespaces = true;
      RestrictRealtime = true;
      RestrictSUIDSGID = true;
      LockPersonality = true;
      MemoryDenyWriteExecute = true;   # fine for Go, no JIT
      SystemCallArchitectures = "native";
    };
  };
}
```

`KREM_LISTEN` and `KREM_DB` are intentionally omitted: the binary defaults are
`127.0.0.1:5736` and `${StateDirectory}/kreminders.db`. Override only if the
proxy hostname or port changes.

An optional smoke-test unit that reuses the same environment and secrets:

```nix
  systemd.services.kreminders-test-notify = {
    environment = config.systemd.services.kreminders.environment;
    serviceConfig = {
      Type = "oneshot";
      User = "kreminders";
      Group = "kreminders";
      StateDirectory = "kreminders";
    };
  };
```

Upgrade path: `nixos-rebuild switch` restarts the unit; migrations run at
startup before the listener opens. A downgrade to an older binary is
refused (higher `user_version`) — restore a backup instead.

## 3. Reverse proxy

The app does no TLS and must not be cached. Requirements: forward POST bodies
and methods unchanged, don't buffer `/mcp` (streamable HTTP uses chunked/SSE
responses), and keep timeouts generous.

```nginx
# nginx — inside the existing TLS server for kreminders.nb.kling.dev
client_max_body_size 64k;

location /mcp {
  proxy_pass http://127.0.0.1:5736;
  proxy_http_version 1.1;
  proxy_set_header Connection "";
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
  proxy_buffering off;        # required for streamable HTTP
  proxy_read_timeout 1h;
  proxy_send_timeout 1h;
}

location / {
  proxy_pass http://127.0.0.1:5736;
  proxy_http_version 1.1;
  proxy_set_header Connection "";
  proxy_set_header Host $host;
  proxy_set_header X-Forwarded-Proto $scheme;
}
```

```caddy
# Caddy alternative
kreminders.nb.kling.dev {
  reverse_proxy 127.0.0.1:5736
}
```

Caddy doesn't buffer proxied streams by default. The app already sends
`Cache-Control: no-store` on every response, so the only caching risk is a
proxy configured to cache aggressively — don't.

The app binds loopback only; the proxy is the only thing on the mesh that can
reach it. If you ever want LAN access without Netbird, point the proxy at the
LAN interface instead, but do not bind the app to `0.0.0.0` directly.

## 4. Secrets

Four secrets, no database credentials (SQLite), no TLS keys (proxy owns the
certificates).

| Env `_FILE` var | Contents | Source |
|---|---|---|
| `KREM_GOTIFY_APP_TOKEN_FILE` | Gotify application token | Gotify UI → **Apps** → create `kreminders` |
| `KREM_GOTIFY_CLIENT_TOKEN_FILE` | Gotify client token for *your* user | Gotify UI → **Clients** → create `kreminders` |
| `KREM_SIGNING_KEY_FILE` | 32 random bytes, hex (64 chars) | `openssl rand -hex 32` |
| `KREM_MCP_TOKEN_FILE` | 32 random bytes, hex (64 chars) | `openssl rand -hex 32` |

Notes:

- Gotify 3 shows tokens exactly once, on creation or rotation.
- The **client token is the privileged one**: it can read and delete every
  message belonging to that user. It must belong to the same user that owns the
  `kreminders` application, otherwise cleanup will 403.
- Leaving `KREM_GOTIFY_CLIENT_TOKEN_FILE` unset disables cleanup; nothing else
  breaks.
- `KREM_MCP_TOKEN_FILE` unset disables `/mcp` (404). This is a valid way to run
  without MCP.
- The MCP token also has to be configured in each agent; see `mcp.md`.

### agenix

```sh
umask 077
mkdir -p secrets
agenix -e secrets/kreminders_gotify_app.age     # paste the Gotify app token
agenix -e secrets/kreminders_gotify_client.age  # paste the Gotify client token
agenix -e secrets/kreminders_signing.age        # paste: openssl rand -hex 32
agenix -e secrets/kreminders_mcp.age            # paste: openssl rand -hex 32
```

```nix
age.secrets = {
  kreminders_gotify_app    = { file = ../secrets/kreminders_gotify_app.age;    owner = "kreminders"; mode = "0400"; };
  kreminders_gotify_client = { file = ../secrets/kreminders_gotify_client.age; owner = "kreminders"; mode = "0400"; };
  kreminders_signing       = { file = ../secrets/kreminders_signing.age;       owner = "kreminders"; mode = "0400"; };
  kreminders_mcp           = { file = ../secrets/kreminders_mcp.age;           owner = "kreminders"; mode = "0400"; };
};
```

### sops-nix

```yaml
# secrets/kreminders.yaml  (encrypted with sops)
gotify_app_token: "..."
gotify_client_token: "..."
signing_key: "..."
mcp_token: "..."
```

```nix
sops.secrets = {
  kreminders_gotify_app    = { key = "gotify_app_token";    owner = "kreminders"; mode = "0400"; };
  kreminders_gotify_client = { key = "gotify_client_token"; owner = "kreminders"; mode = "0400"; };
  kreminders_signing       = { key = "signing_key";         owner = "kreminders"; mode = "0400"; };
  kreminders_mcp           = { key = "mcp_token";           owner = "kreminders"; mode = "0400"; };
};
```

Both styles work because the binary consumes paths (`*_FILE`), not values. If
you prefer one `EnvironmentFile`, `sops.templates` can generate it; the result
is identical.

## 5. First-run checklist

1. In Gotify: create application `kreminders` (Apps) and client `kreminders`
   (Clients). Copy both tokens.
2. `openssl rand -hex 32` twice → signing key, MCP token.
3. Encrypt the four values (§4) and reference them from the unit.
4. Deploy (`nixos-rebuild switch`) and check the unit:
   `systemctl status kreminders`, `journalctl -u kreminders -n 30`.
5. Confirm it is listening on loopback and nothing else has the port:
   `ss -ltnp | grep 5736` → only `kreminders`.
6. `curl -sS http://127.0.0.1:5736/healthz` → `{"ok":true,...}`.
7. Send a test notification: `sudo systemctl start kreminders-test-notify`
   (§2) → notification on the phone.
8. Tap it: the dashboard must load over HTTPS from the phone.
9. Add the MCP endpoint to your agent with the bearer token and call
   `list_tasks` (should be empty) then create the first real task.
10. Hit `https://kreminders.nb.kling.dev/` from the phone over Netbird.

## 6. Backups

```nix
systemd.services.kreminders-backup = {
  description = "kreminders SQLite snapshot";
  serviceConfig = {
    Type = "oneshot";
    User = "kreminders";
    Group = "kreminders";
    ReadWritePaths = [ "/var/lib/kreminders" ];
    StateDirectory = "kreminders";
  };
  script = ''
    set -eu
    mkdir -p /var/lib/kreminders/backup
    dst="/var/lib/kreminders/backup/kreminders-$(date +%%F).db"
    ${pkgs.sqlite}/bin/sqlite3 /var/lib/kreminders/kreminders.db \
      "VACUUM INTO '$dst'"
    ${pkgs.sqlite}/bin/sqlite3 "$dst" "PRAGMA integrity_check;" | grep -qx ok
    find /var/lib/kreminders/backup -name 'kreminders-*.db' -mtime +14 -delete
  '';
};

systemd.timers.kreminders-backup = {
  wantedBy = [ "timers.target" ];
  timerConfig.OnCalendar = "*-*-* 03:30:00";
  timerConfig.Persistent = true;
};
```

`%%F` is escaped because systemd treats `%` as a specifier. `VACUUM INTO` is
consistent with a live service; never `cp` the database. Copy snapshots off the
host if you care about disk failure.

**Restore**

```sh
systemctl stop kreminders
cd /var/lib/kreminders
install -o kreminders -g kreminders -m 0600 \
  backup/kreminders-2026-11-24.db kreminders.db
rm -f kreminders.db-wal kreminders.db-shm
sqlite3 kreminders.db "PRAGMA integrity_check; PRAGMA user_version;"
systemctl start kreminders
```

If the restored file's `user_version` is older than the deployed binary, the
next start migrates it forward automatically.

## 7. Monitoring

- `GET /healthz` (loopback) → `200 {"ok":true,"db":"ok","last_tick":"…","version":"…"}`;
  `503` if the DB is unreachable or the scheduler hasn't ticked in 5 minutes.
- Logs: `journalctl -u kreminders -f`. Failures include the occurrence id, the
  Gotify status, and the response body. Delivery failures also surface on the
  dashboard and in `/healthz`.
- A silent system is usually a correct one: no open tasks, all cycles resolved.
  Distinguish by checking `last_tick` in `/healthz` and `next_due_at` in the DB.

## 8. Runbook

**Rotate the Gotify app token** — UI → Apps → `kreminders` → rotate → update
`kreminders_gotify_app` → `nixos-rebuild switch` (or restart the unit) →
`test-notify`.

**Rotate the client token** — same, under Clients. Only affects cleanup.

**Rotate the MCP token** — generate, encrypt, redeploy, update every agent
config. Old tokens stop working immediately on restart.

**Rotate the signing key** — old confirm links return 410; the next nag issues a
fresh one. No data is affected.

**Force a nag for an open occurrence** (e.g. testing a new plan):

```sh
sudo -u kreminders sqlite3 /var/lib/kreminders/kreminders.db \
  "UPDATE occurrence SET next_nag_at = (strftime('%s','now') * 1000)
   WHERE id = 42 AND status = 'pending';"
```

The next tick (≤30 s) sends it. This is only safe while the service is running
— it's a normal write under WAL.

**Change the internal port** — set `KREM_LISTEN=127.0.0.1:6742` in the unit env
and update the two `proxy_pass` lines. Nothing in the DB or in any issued URL
contains the port.

**Change `KREM_BASE_URL`** — new nags and `confirm_url` outputs use it
immediately; links already delivered keep the old host until they are replaced
by the next nag. Point both hostnames at the proxy during a transition if old
links must keep working.

**Investigate a missed nag**

```sh
sudo -u kreminders sqlite3 -header -column /var/lib/kreminders/kreminders.db \
  "SELECT id, task_id, status, datetime(due_at/1000,'unixepoch','localtime') AS due,
          datetime(next_nag_at/1000,'unixepoch','localtime') AS next,
          nag_count, delivery_failures
   FROM occurrence ORDER BY due_at DESC LIMIT 10;"

sudo -u kreminders sqlite3 -header -column /var/lib/kreminders/kreminders.db \
  "SELECT id, occurrence_id, ok, priority, error,
          datetime(sent_at/1000,'unixepoch','localtime') AS sent
   FROM notification ORDER BY sent_at DESC LIMIT 10;"
```

**Pause everything** — `systemctl stop kreminders`. The cursor and
`next_nag_at` survive; on start, overdue nags are sent once (no burst), with a
single message per occurrence.

## 9. Troubleshooting

| Symptom | Likely cause | Check |
|---|---|---|
| No notifications at all | Gotify creds, quiet hours, or `next_due_at` in the future | `kreminders test-notify`; `/healthz`; `journalctl` |
| `401` from Gotify | app token wrong/rotated | re-create the app token |
| `403` deleting messages | client token belongs to a different user, or unset | Clients tab; log line `cleanup: 403` |
| Confirm link 404 | signing key changed, or the link was truncated by a client | compare key; check the full URL length |
| Confirm link 410 | older than `max(due, deadline) + 30d` | use the newest notification |
| Proxy 502 | service down, wrong port, or bound elsewhere | `systemctl status`, `ss -ltnp \| grep 5736` |
| MCP `401` | bearer token mismatch | header spelling, agent config substitution |
| MCP `404` | `KREM_MCP_TOKEN` not set | unit env |
| MCP tool calls time out behind proxy | proxy buffering | `proxy_buffering off`, `flush_interval -1` in Caddy |
| `SQLITE_BUSY` in logs | another process holds a write lock (e.g. a manual `sqlite3` session left open) | `lsof /var/lib/kreminders/kreminders.db` |
| Duplicate notifications after a restart | crash between Gotify 2xx and commit | accepted; visible in `notification` rows with the same `occurrence_id` |
| Nag arrives at 03:00 | catch-up after downtime, or `respect_quiet_hours = 0` | `design.md` §6 |
| Everything looks right but it's 11:59 pm | clock | `timedatectl`; NixOS enables timesyncd by default |
