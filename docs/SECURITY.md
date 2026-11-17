# Securing a Streamero server

Streamero is meant to be reachable from the Internet, so expect scanners and
password guessers within hours of pointing a domain at it. This page lists what
the server does on its own and what to set up on the host.

## What the server does
- **Sign-in:** argon2id passwords, optional two-factor codes, per-account lockout
  (5 failures → 15 minutes), a per-client rate limit on every sign-in style endpoint
  (`STREAMERO_AUTH_RATE_LIMIT`, default 20 a minute) and an automatic ban: 15 failed
  attempts from one address within 10 minutes block that address from signing in for
  an hour (IPv6 clients are counted per /64; private and loopback addresses are never
  banned). Bans are logged as `client banned from sign-in after repeated failures`.
- **Requests:** 1 MiB body limit on the API, signed and expiring stream links,
  per-device and per-account transcode caps, security headers (CSP, HSTS on HTTPS).
- **Scanners and crawlers:** `/.env`, `/.git/…`, `*.php` and similar paths answer 404
  instead of the web client, and `/robots.txt` disallows everything.
- **Containers:** the server runs as an unprivileged user with all Linux capabilities
  dropped and `no-new-privileges`; the database and cache are not published to the host.

## What to set on the host
1. **Reverse proxy address:** set `STREAMERO_TRUSTED_PROXIES` in `.env` to the address
   the proxy connects from (for nginx on the same host with Docker: the bridge gateway,
   e.g. `172.18.0.1`). Without it every visitor looks like the proxy, so rate limits are
   shared by everyone and the sign-in ban cannot work.
2. **Run the hardening script** on the server:
   ```bash
   sudo ./scripts/harden-host.sh --site <your.domain>
   ```
   It adds per-client limits, a log without query strings and "no answer" for scanner
   paths to that nginx site only, installs fail2ban with jails for SSH and for clients
   probing the site, sets `PermitRootLogin no` and `MaxAuthTries 3` for SSH, and makes
   sure nginx logs rotate. Every step is idempotent, tests the configuration first and
   rolls back on failure. `--dry-run` shows what it would do.
3. **SSH keys instead of passwords** (the most effective single step):
   ```bash
   ssh-copy-id -p <ssh-port> <user>@<server>      # from your computer
   ssh -p <ssh-port> <user>@<server>              # check that the key works
   sudo ./scripts/harden-host.sh --site <your.domain> --ssh-keys-only
   ```
   The script refuses `--ssh-keys-only` while your user has no key installed, so it
   cannot lock you out.
4. **Firewall (optional):** `--firewall` enables ufw with only SSH, 80 and 443 open.
   Allow any other service on the host first (`sudo ufw allow <port>`). Ports Docker
   publishes bypass ufw: keep them bound to `127.0.0.1` (the compose defaults do).
5. **Keep `.env` private** (`chmod 600`) and keep backups somewhere safe: a backup
   holds password hashes, two-factor secrets and integration keys.

## Checking for attacks
```bash
sudo fail2ban-client status sshd                 # banned SSH guessers
sudo fail2ban-client status streamero-probes     # banned scanners
docker compose logs streamero | grep 'banned from sign-in'
journalctl -u ssh --since "7 days ago" | grep -c 'Failed password'
```
Failed sign-ins are also in Settings → Activity.
