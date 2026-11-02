#!/usr/bin/env bash
# One-time VPS setup (Ubuntu/Debian). Run as root on the VPS:
#   DOMAIN=skinverdict.example.com bash setup.sh
set -euo pipefail
: "${DOMAIN:?set DOMAIN=your.domain}"
apt-get update -qq && apt-get install -y -qq curl debian-keyring debian-archive-keyring apt-transport-https >/dev/null
# Caddy
if ! command -v caddy >/dev/null; then
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/gpg.key' | gpg --dearmor -o /usr/share/keyrings/caddy-stable-archive-keyring.gpg
  curl -1sLf 'https://dl.cloudsmith.io/public/caddy/stable/debian.deb.txt' > /etc/apt/sources.list.d/caddy-stable.list
  apt-get update -qq && apt-get install -y -qq caddy >/dev/null
fi
id -u skinverdict >/dev/null 2>&1 || useradd -m -r -s /bin/bash skinverdict
mkdir -p /opt/skinverdict/data && chown -R skinverdict:skinverdict /opt/skinverdict
sed "s/{\$DOMAIN}/$DOMAIN/" "$(dirname "$0")/Caddyfile" > /etc/caddy/Caddyfile
cp "$(dirname "$0")/skinverdict.service" /etc/systemd/system/skinverdict.service
systemctl daemon-reload && systemctl enable skinverdict >/dev/null && systemctl restart caddy
# Node + Claude Code CLI for the agent (optional; AGENT=rules works without it)
if ! command -v node >/dev/null; then curl -fsSL https://deb.nodesource.com/setup_22.x | bash - >/dev/null && apt-get install -y -qq nodejs >/dev/null; fi
npm install -g @anthropic-ai/claude-code >/dev/null 2>&1 || true
echo "setup done. Next: put YOUCAM_API_KEY in /opt/skinverdict/.env, then 'su - skinverdict -c claude' once to log the CLI in, then run deploy.sh from your Mac."
