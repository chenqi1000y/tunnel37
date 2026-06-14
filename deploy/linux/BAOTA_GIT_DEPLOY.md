# BaoTa Git Deployment

This project can be deployed on a BaoTa Linux server by pulling source code from GitHub and building the Go binary on the server.

## 1. Install Server Dependencies

```bash
yum install -y git curl || apt update && apt install -y git curl
```

Install Go if it is not available:

```bash
go version
```

Recommended Go version: `1.25+`.

## 2. Clone Repository

BaoTa recommended path:

```bash
mkdir -p /www/wwwroot
cd /www/wwwroot
git clone https://github.com/chenqi1000y/tunnel37.git tunnel37git
cd /www/wwwroot/tunnel37git
```

If the repository is private, use SSH clone or GitHub token authentication.

## 3. Create Environment File

```bash
cat >/www/wwwroot/tunnel37git/tunnel-server.env <<'EOF'
TUNNEL_SERVER_ADDR=127.0.0.1:9080
TUNNEL_TCP_ADDR=0.0.0.0:9081
TUNNEL_SOCKS_ADDR=0.0.0.0:21080
TUNNEL_SHARED_TOKEN=replace-with-strong-token
TUNNEL_ADMIN_USER=admin
TUNNEL_ADMIN_PASS=replace-with-strong-admin-password
TUNNEL_REDIS_ADDR=127.0.0.1:6379
TUNNEL_REDIS_PASSWORD=
TUNNEL_REDIS_DB=0
TUNNEL_REDIS_PREFIX=tunnel
TUNNEL_PUBLIC_BASE=https://tunnel.ma37.com
TUNNEL_PUBLIC_RELAY_ADDR=tunnel.ma37.com:9081
TUNNEL_SOCKS_HOST=tunnel.ma37.com
TUNNEL_SOCKS_PORT_PUBLIC=21080
EOF

chmod 600 /www/wwwroot/tunnel37git/tunnel-server.env
```

## 4. Install systemd Service

```bash
cp /www/wwwroot/tunnel37git/deploy/linux/tunnel-server.baota.service /etc/systemd/system/tunnel-server.service
systemctl daemon-reload
systemctl enable tunnel-server.service
```

## 5. Build and Start

```bash
cd /www/wwwroot/tunnel37git
go build -o tunnel-server ./cmd/tunnel-server
chmod +x tunnel-server
systemctl restart tunnel-server.service
```

## 6. Nginx for BaoTa

Create `/www/server/panel/vhost/nginx/tunnel.ma37.com.conf`:

```nginx
server {
    listen 80;
    server_name tunnel.ma37.com;

    location / {
        proxy_pass http://127.0.0.1:9080;
        proxy_http_version 1.1;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
    }
}
```

Then reload Nginx:

```bash
nginx -t && systemctl reload nginx
```

## 7. Open Ports

Open these ports in BaoTa security and cloud security group:

```txt
80/tcp
443/tcp
9081/tcp
21080/tcp
```

Do not expose `9080` or `6379` to the public internet.

## 8. Update Deployment

After the first deployment:

```bash
cd /www/wwwroot/tunnel37git
bash deploy/linux/git-deploy.sh
```

Or with runtime detail:

```bash
cd /www/wwwroot/tunnel37git
APP_DIR=/www/wwwroot/tunnel37git TUNNEL_ADMIN_USER=admin TUNNEL_ADMIN_PASS='your-admin-password' bash deploy/linux/git-deploy.sh
```

## 9. Verify

```bash
systemctl status tunnel-server.service --no-pager -l
ss -lntp | grep -E ':9080|:9081|:21080'
curl http://127.0.0.1:9080/readyz
curl -u admin:your-admin-password http://127.0.0.1:9080/api/admin/runtime
```

Admin URL:

```txt
http://tunnel.ma37.com/admin
```

SOCKS5 format:

```txt
socks5://<proxy_id>:<shared-token>@tunnel.ma37.com:21080
```
