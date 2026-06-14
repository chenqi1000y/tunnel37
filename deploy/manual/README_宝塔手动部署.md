# 宝塔 Linux 手动上传部署说明

适用场景：

1. 服务器访问不了 GitHub。
2. 服务器没有 Go 环境。
3. Docker 构建访问外网超时。
4. 想今天最快把 `tunnel.ma37.com` 服务端跑起来。

## 1. 上传文件

将本地生成的部署包上传到宝塔：

```txt
release/baota-manual/tunnel37-baota-manual.zip
```

建议上传到：

```txt
/www/wwwroot/
```

然后在宝塔终端执行：

```bash
cd /www/wwwroot
unzip -o tunnel37-baota-manual.zip -d tunnel37-upload
cd /www/wwwroot/tunnel37-upload
```

## 2. 修改配置

编辑：

```bash
vi tunnel-server.env.example
```

确认这些值：

```txt
TUNNEL_SHARED_TOKEN
TUNNEL_ADMIN_USER
TUNNEL_ADMIN_PASS
TUNNEL_REDIS_ADDR
TUNNEL_PUBLIC_BASE
TUNNEL_PUBLIC_RELAY_ADDR
TUNNEL_SOCKS_HOST
```

如果宝塔 Redis 没有密码，保持：

```txt
TUNNEL_REDIS_PASSWORD=
```

如果 Redis 有密码，填入：

```txt
TUNNEL_REDIS_PASSWORD=你的Redis密码
```

## 3. 执行安装

```bash
chmod +x install-baota.sh
APP_DIR=/www/wwwroot/tunnel37git DOMAIN=tunnel.ma37.com bash install-baota.sh
```

安装脚本会自动：

1. 创建 `/www/wwwroot/tunnel37git`。
2. 备份旧二进制。
3. 复制新的 `tunnel-server`。
4. 创建或保留 `tunnel-server.env`。
5. 安装 systemd 服务。
6. 写入 Nginx 反代配置。
7. 重启服务。
8. 执行 `/readyz` 健康检查。

## 4. 检查服务

```bash
systemctl status tunnel-server.service --no-pager -l
ss -lntp | grep -E ':9080|:9081|:21080'
curl http://127.0.0.1:9080/readyz
curl -u admin:R2SQTNt-O-dqbLhSyYpYnuKD http://127.0.0.1:9080/api/admin/runtime
```

预期端口：

```txt
127.0.0.1:9080
0.0.0.0:9081
0.0.0.0:21080
```

## 5. 宝塔和云服务器放行端口

必须放行：

```txt
80
443
9081
21080
```

不要公网放行：

```txt
9080
6379
```

## 6. 管理后台

浏览器打开：

```txt
http://tunnel.ma37.com/admin
```

账号密码来自：

```txt
TUNNEL_ADMIN_USER
TUNNEL_ADMIN_PASS
```

## 7. 客户端连接

客户端连接地址：

```txt
tunnel.ma37.com:9081
```

token：

```txt
TUNNEL_SHARED_TOKEN
```

## 8. SOCKS5 使用格式

```txt
socks5://<proxy_id>:<TUNNEL_SHARED_TOKEN>@tunnel.ma37.com:21080
```

## 9. 查看日志

```bash
journalctl -u tunnel-server.service -n 100 --no-pager
tail -n 100 /www/wwwroot/tunnel37git/logs/tunnel-server.log
tail -n 100 /www/wwwroot/tunnel37git/logs/tunnel-server.err.log
```

## 10. 更新版本

以后更新时：

1. 本地重新生成 `tunnel37-baota-manual.zip`。
2. 上传覆盖到 `/www/wwwroot/`。
3. 重新执行：

```bash
cd /www/wwwroot
rm -rf tunnel37-upload
unzip -o tunnel37-baota-manual.zip -d tunnel37-upload
cd /www/wwwroot/tunnel37-upload
APP_DIR=/www/wwwroot/tunnel37git DOMAIN=tunnel.ma37.com bash install-baota.sh
```

