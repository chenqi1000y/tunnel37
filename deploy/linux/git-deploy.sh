#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/opt/demo-go-tunnel}"
SERVICE_NAME="${SERVICE_NAME:-tunnel-server.service}"

cd "$APP_DIR"

echo "---GIT---"
git pull --ff-only

echo "---BUILD---"
go build -o tunnel-server ./cmd/tunnel-server
chmod +x tunnel-server

echo "---RESTART---"
systemctl daemon-reload
systemctl restart "$SERVICE_NAME"

echo "---STATUS---"
systemctl status "$SERVICE_NAME" --no-pager -l | head -n 40 || true

echo "---READYZ---"
curl -fsS --max-time 5 http://127.0.0.1:9080/readyz

echo
echo "---RUNTIME---"
if [ -n "${TUNNEL_ADMIN_USER:-}" ] && [ -n "${TUNNEL_ADMIN_PASS:-}" ]; then
  curl -fsS --max-time 5 -u "$TUNNEL_ADMIN_USER:$TUNNEL_ADMIN_PASS" http://127.0.0.1:9080/api/admin/runtime
else
  echo "Set TUNNEL_ADMIN_USER and TUNNEL_ADMIN_PASS to print /api/admin/runtime"
fi
echo

