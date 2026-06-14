#!/usr/bin/env bash
set -euo pipefail

APP_DIR="${APP_DIR:-/www/wwwroot/tunnel37git}"
SERVICE_NAME="${SERVICE_NAME:-tunnel-server.service}"
DOMAIN="${DOMAIN:-tunnel.ma37.com}"
NGINX_CONF_DIR="${NGINX_CONF_DIR:-/www/server/panel/vhost/nginx}"

PACKAGE_DIR="$(cd "$(dirname "$0")" && pwd)"

echo "应用目录: $APP_DIR"
echo "域名: $DOMAIN"

mkdir -p "$APP_DIR" "$APP_DIR/logs" "$APP_DIR/backups" "$NGINX_CONF_DIR"

if [ -f "$APP_DIR/tunnel-server" ]; then
  backup="$APP_DIR/backups/tunnel-server.$(date +%Y%m%d-%H%M%S)"
  echo "备份旧二进制: $backup"
  cp -a "$APP_DIR/tunnel-server" "$backup"
fi

echo "停止旧 systemd 服务"
systemctl stop "$SERVICE_NAME" 2>/dev/null || true
systemctl reset-failed "$SERVICE_NAME" 2>/dev/null || true

echo "复制 tunnel-server"
cp -a "$PACKAGE_DIR/tunnel-server" "$APP_DIR/tunnel-server"
chmod +x "$APP_DIR/tunnel-server"

if [ ! -f "$APP_DIR/tunnel-server.env" ]; then
  echo "创建默认 tunnel-server.env"
  cp -a "$PACKAGE_DIR/tunnel-server.env.example" "$APP_DIR/tunnel-server.env"
  chmod 600 "$APP_DIR/tunnel-server.env"
else
  echo "保留已有 tunnel-server.env"
fi

echo "安装 systemd 服务"
sed "s#__APP_DIR__#$APP_DIR#g" "$PACKAGE_DIR/tunnel-server.service.template" >"/etc/systemd/system/$SERVICE_NAME"
systemctl daemon-reload
systemctl enable "$SERVICE_NAME"
systemctl restart "$SERVICE_NAME"

echo "写入 Nginx 配置"
sed "s#__DOMAIN__#$DOMAIN#g" "$PACKAGE_DIR/nginx-tunnel.conf.template" >"$NGINX_CONF_DIR/$DOMAIN.conf"
if command -v nginx >/dev/null 2>&1; then
  nginx -t && systemctl reload nginx
else
  echo "未找到 nginx 命令，请在宝塔面板手动重载 Nginx"
fi

echo "尝试放行本机防火墙端口"
if command -v firewall-cmd >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port=80/tcp || true
  firewall-cmd --permanent --add-port=443/tcp || true
  firewall-cmd --permanent --add-port=9081/tcp || true
  firewall-cmd --permanent --add-port=21080/tcp || true
  firewall-cmd --reload || true
fi

if command -v ufw >/dev/null 2>&1; then
  ufw allow 80/tcp || true
  ufw allow 443/tcp || true
  ufw allow 9081/tcp || true
  ufw allow 21080/tcp || true
fi

echo "服务状态"
systemctl status "$SERVICE_NAME" --no-pager -l | head -n 40 || true

echo "健康检查"
sleep 2
curl -fsS --max-time 5 http://127.0.0.1:9080/readyz || {
  echo
  echo "readyz 检查失败，请查看日志:"
  echo "journalctl -u $SERVICE_NAME -n 100 --no-pager"
  echo "tail -n 100 $APP_DIR/logs/tunnel-server.err.log"
  exit 1
}

echo
echo "部署完成"
echo "管理后台: http://$DOMAIN/admin"
echo "Relay 端口: $DOMAIN:9081"
echo "SOCKS5 端口: $DOMAIN:21080"

