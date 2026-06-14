# 宝塔 Docker 一键部署说明

当服务器没有 Go 环境，或者 `go build` 报 `go: command not found` 时，推荐使用 Docker 部署。

当前 Dockerfile 已使用仓库内的 `vendor/` 依赖构建，构建时不再访问 `proxy.golang.org`，适合国内服务器。

## 1. 拉取代码

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

## 2. 创建 Docker 环境变量

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

## 3. 启动

新版 Docker：

```bash
docker compose -f docker-compose.baota.yml up -d --build
```

旧版 Docker Compose：

```bash
docker-compose -f docker-compose.baota.yml up -d --build
```

## 4. 验证

```bash
docker ps
curl http://127.0.0.1:9080/readyz
curl -u admin:R2SQTNt-O-dqbLhSyYpYnuKD http://127.0.0.1:9080/api/admin/runtime
ss -lntp | grep -E ':9080|:9081|:21080'
```

Expected:
预期能看到：

```txt
127.0.0.1:9080
0.0.0.0:9081
0.0.0.0:21080
```

## 5. 宝塔 Nginx 反向代理

创建或更新 `/www/server/panel/vhost/nginx/tunnel.ma37.com.conf`：

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

## 6. 放行端口

宝塔安全和云服务器安全组都要放行：

```txt
80/tcp
443/tcp
9081/tcp
21080/tcp
```

不要公网开放：

```txt
9080/tcp
6379/tcp
```

## 7. 更新

```bash
cd /www/wwwroot/tunnel37git
git pull --ff-only
docker compose -f docker-compose.baota.yml up -d --build
```

如果你的服务器使用旧版命令：

```bash
docker-compose -f docker-compose.baota.yml up -d --build
```

## 8. 查看日志

```bash
docker logs -f tunnel37-server
docker logs -f tunnel37-redis
```

## 9. SOCKS5 格式

```txt
socks5://<proxy_id>:rqGO1Z0_W-M-vxw-bgN8XigzoaNNwZMx@tunnel.ma37.com:21080
```

## 10. 常见错误

### 10.1 go mod download 超时

错误示例：

```txt
go: github.com/xxx: Get "https://proxy.golang.org/...": i/o timeout
```

处理：

```bash
cd /www/wwwroot/tunnel37git
git pull --ff-only
docker-compose -f docker-compose.baota.yml up -d --build
```

原因：

当前仓库已提交 `vendor/` 目录，新的 Dockerfile 会使用 `go build -mod=vendor`，不会再访问 `proxy.golang.org`。

### 10.2 9080 connection refused

说明服务还没启动成功。

检查：

```bash
docker ps -a
docker logs tunnel37-server --tail 100
docker logs tunnel37-redis --tail 100
```

### 10.3 systemd 旧服务反复失败

如果之前已经创建过 `tunnel-server.service`，现在改用 Docker，可以先停掉 systemd 旧服务：

```bash
systemctl stop tunnel-server.service || true
systemctl disable tunnel-server.service || true
systemctl reset-failed tunnel-server.service || true
```
