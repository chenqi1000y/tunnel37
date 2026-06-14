# Docker One-Click Deployment for BaoTa

Docker deployment is recommended when the server does not have Go installed.

## 1. Clone Repository

```bash
mkdir -p /www/wwwroot
cd /www/wwwroot
git clone https://github.com/chenqi1000y/tunnel37.git tunnel37git
cd /www/wwwroot/tunnel37git
```

If the directory already exists:

```bash
cd /www/wwwroot/tunnel37git
git pull --ff-only
```

## 2. Create Docker Env File

```bash
cat > .env <<'EOF'
TUNNEL_SHARED_TOKEN=rqGO1Z0_W-M-vxw-bgN8XigzoaNNwZMx
TUNNEL_ADMIN_USER=admin
TUNNEL_ADMIN_PASS=R2SQTNt-O-dqbLhSyYpYnuKD
TUNNEL_PUBLIC_BASE=https://tunnel.ma37.com
TUNNEL_PUBLIC_RELAY_ADDR=tunnel.ma37.com:9081
TUNNEL_SOCKS_HOST=tunnel.ma37.com
TUNNEL_SOCKS_PORT_PUBLIC=21080
EOF

chmod 600 .env
```

## 3. Start

For modern Docker:

```bash
docker compose -f docker-compose.baota.yml up -d --build
```

For older Docker Compose:

```bash
docker-compose -f docker-compose.baota.yml up -d --build
```

## 4. Verify

```bash
docker ps
curl http://127.0.0.1:9080/readyz
curl -u admin:R2SQTNt-O-dqbLhSyYpYnuKD http://127.0.0.1:9080/api/admin/runtime
ss -lntp | grep -E ':9080|:9081|:21080'
```

Expected:

```txt
127.0.0.1:9080
0.0.0.0:9081
0.0.0.0:21080
```

## 5. BaoTa Nginx Reverse Proxy

Create or update `/www/server/panel/vhost/nginx/tunnel.ma37.com.conf`:

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

Reload:

```bash
nginx -t && systemctl reload nginx
```

## 6. Open Ports

Open these ports in BaoTa security and cloud security group:

```txt
80/tcp
443/tcp
9081/tcp
21080/tcp
```

Do not expose:

```txt
9080/tcp
6379/tcp
```

## 7. Update

```bash
cd /www/wwwroot/tunnel37git
git pull --ff-only
docker compose -f docker-compose.baota.yml up -d --build
```

## 8. Logs

```bash
docker logs -f tunnel37-server
docker logs -f tunnel37-redis
```

## 9. SOCKS5 Format

```txt
socks5://<proxy_id>:rqGO1Z0_W-M-vxw-bgN8XigzoaNNwZMx@tunnel.ma37.com:21080
```

