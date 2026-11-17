#!/usr/bin/env bash
# Hardens the host a Streamero server runs on. Run with sudo on the server:
#
#   sudo ./scripts/harden-host.sh --site demo.streamero.net
#
# What it does (each step is idempotent and leaves other sites alone):
#   1. nginx: per-client rate and connection limits, a log without query strings,
#      and no answer (444) for the files scanners probe for, on the given site only.
#   2. fail2ban: bans SSH password guessers and clients that keep probing the site.
#   3. sshd: no root login, three tries per connection.
#   4. log rotation for nginx (installs logrotate when it is missing).
# Options:
#   --site NAME        nginx site (file in /etc/nginx/sites-available); may repeat
#   --ssh-keys-only    also turn off SSH password sign-in (refused unless the
#                      calling user already has a key in ~/.ssh/authorized_keys)
#   --firewall         also enable ufw, allowing only SSH, 80 and 443
#   --dry-run          show what would be done
set -euo pipefail

SITES=(); KEYS_ONLY=0; FIREWALL=0; DRY=0
while [ $# -gt 0 ]; do
  case "$1" in
    --site) SITES+=("$2"); shift 2 ;;
    --ssh-keys-only) KEYS_ONLY=1; shift ;;
    --firewall) FIREWALL=1; shift ;;
    --dry-run) DRY=1; shift ;;
    -h|--help) sed -n '2,19p' "$0"; exit 0 ;;
    *) echo "unknown option: $1" >&2; exit 2 ;;
  esac
done
[ "$(id -u)" = 0 ] || { echo "run with sudo" >&2; exit 1; }
REPO="$(cd "$(dirname "$0")/.." && pwd)"
run() { if [ "$DRY" = 1 ]; then echo "would run: $*"; else "$@"; fi; }
say() { printf '\n==> %s\n' "$*"; }

SSH_PORT="$(sshd -T 2>/dev/null | awk '$1=="port"{print $2; exit}')"
SSH_PORT="${SSH_PORT:-22}"

say "packages"
need=()
command -v fail2ban-client >/dev/null || need+=(fail2ban)
command -v logrotate >/dev/null || need+=(logrotate)
[ "$FIREWALL" = 1 ] && ! command -v ufw >/dev/null && need+=(ufw)
if [ ${#need[@]} -gt 0 ]; then
  run apt-get update -q
  run env DEBIAN_FRONTEND=noninteractive apt-get install -y -q "${need[@]}"
else
  echo "nothing to install"
fi

if command -v nginx >/dev/null && [ ${#SITES[@]} -gt 0 ]; then
  say "nginx"
  run install -m 0644 "$REPO/deploy/nginx/streamero-http.conf" /etc/nginx/conf.d/streamero-http.conf
  run install -D -m 0644 "$REPO/deploy/nginx/streamero-hardening.conf" /etc/nginx/snippets/streamero-hardening.conf
  stamp="$(date +%Y%m%d%H%M%S)"
  changed=()
  for site in "${SITES[@]}"; do
    f="/etc/nginx/sites-available/$site"
    [ -f "$f" ] || { echo "no such site: $f" >&2; exit 1; }
    if grep -q 'snippets/streamero-hardening.conf' "$f"; then
      echo "$site: already includes the hardening snippet"
      continue
    fi
    # Only the HTTPS server block (the one certbot manages) gets the snippet.
    if ! grep -q 'listen .*443' "$f"; then
      echo "$site: no HTTPS server block yet (run certbot first); skipped" >&2
      continue
    fi
    run cp -a "$f" "$f.bak-$stamp"
    if [ "$DRY" = 0 ]; then
      awk -v inc='    include /etc/nginx/snippets/streamero-hardening.conf;' '
        { lines[NR] = $0 }
        /^[[:space:]]*server[[:space:]]*\{/ { start = NR }
        /listen .*443/ { https[start] = 1 }
        END {
          for (i = 1; i <= NR; i++) {
            print lines[i]
            if (i in https) print inc
          }
        }' "$f.bak-$stamp" > "$f"
    else
      echo "would add the include to the HTTPS server block of $f"
    fi
    changed+=("$f")
  done
  if [ "$DRY" = 0 ]; then
    if nginx -t 2>/tmp/streamero-nginx-test.log; then
      systemctl reload nginx
      echo "nginx reloaded"
    else
      cat /tmp/streamero-nginx-test.log >&2
      for f in "${changed[@]}"; do cp -a "$f.bak-$stamp" "$f"; done
      rm -f /etc/nginx/conf.d/streamero-http.conf /etc/nginx/snippets/streamero-hardening.conf
      echo "nginx configuration test failed: changes were rolled back" >&2
      exit 1
    fi
  fi
  [ -f /var/log/nginx/streamero.access.log ] || run install -m 0640 -o www-data -g adm /dev/null /var/log/nginx/streamero.access.log
fi

say "fail2ban"
run install -m 0644 "$REPO/deploy/security/fail2ban/filter-streamero-probes.conf" /etc/fail2ban/filter.d/streamero-probes.conf
if [ "$DRY" = 0 ]; then
  sed "s/SSH_PORT/$SSH_PORT/" "$REPO/deploy/security/fail2ban/jail-streamero.conf" > /etc/fail2ban/jail.d/streamero.conf
  # Without an nginx site there is no probe log: keep only the SSH jail.
  if [ ! -f /var/log/nginx/streamero.access.log ]; then
    sed -i '/^\[streamero-probes\]/,$d' /etc/fail2ban/jail.d/streamero.conf
  fi
  systemctl enable --now fail2ban >/dev/null
  systemctl restart fail2ban
  sleep 2
  fail2ban-client status | sed 's/^/  /'
else
  echo "would write /etc/fail2ban/jail.d/streamero.conf (SSH port $SSH_PORT) and restart fail2ban"
fi

say "sshd (port $SSH_PORT)"
drop=/etc/ssh/sshd_config.d/60-streamero-hardening.conf
conf="PermitRootLogin no
MaxAuthTries 3
LoginGraceTime 30
"
if [ "$KEYS_ONLY" = 1 ]; then
  caller="${SUDO_USER:-root}"
  home="$(getent passwd "$caller" | cut -d: -f6)"
  if [ -s "$home/.ssh/authorized_keys" ]; then
    conf+="PasswordAuthentication no
KbdInteractiveAuthentication no
"
  else
    echo "refusing --ssh-keys-only: $caller has no key in $home/.ssh/authorized_keys." >&2
    echo "Add one first (ssh-copy-id -p $SSH_PORT $caller@<server>), check that it works, then run again." >&2
    KEYS_ONLY=0
  fi
fi
if [ "$DRY" = 0 ]; then
  old=""; [ -f "$drop" ] && old="$(cat "$drop")"
  printf '%s' "$conf" > "$drop"
  if sshd -t; then
    systemctl reload ssh 2>/dev/null || systemctl reload sshd
    echo "sshd reloaded (open sessions stay connected)"
  else
    if [ -n "$old" ]; then printf '%s' "$old" > "$drop"; else rm -f "$drop"; fi
    echo "sshd configuration test failed: change rolled back" >&2
    exit 1
  fi
else
  printf 'would write %s:\n%s' "$drop" "$conf"
fi
[ "$KEYS_ONLY" = 1 ] || echo "SSH password sign-in is still on: add a key and run again with --ssh-keys-only."

say "log rotation"
if systemctl list-unit-files logrotate.timer >/dev/null 2>&1 && systemctl cat logrotate.timer >/dev/null 2>&1; then
  run systemctl enable --now logrotate.timer
  echo "logrotate timer enabled"
else
  echo "logrotate timer not found (installing logrotate adds it)"
fi

if [ "$FIREWALL" = 1 ]; then
  say "firewall (ufw)"
  run ufw allow "$SSH_PORT/tcp"
  run ufw allow 80/tcp
  run ufw allow 443/tcp
  run ufw --force enable
  [ "$DRY" = 1 ] || ufw status | sed 's/^/  /'
  echo "Other services on this host that need open ports must be allowed separately (ufw allow <port>)."
fi

say "done"
