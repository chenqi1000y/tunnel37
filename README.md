# demo-go-tunnel

第一版最小隧道控制面骨架，目标是先跑通下面这条链路：

1. 本地 agent 启动
2. agent 通过 TCP 长连接向公网服务器注册
3. 服务器返回 `agent_id` 和 `proxy_id`
4. agent 通过长连接定时心跳保活
5. 服务器可查看当前在线 agent

当前版本已经补到了“服务端标准 SOCKS5 入口 + agent 远程开流”的 DEMO 级实现，方便先把“代理 ID 模式”的核心链路打通。

## 目录

- `cmd/tunnel-server`：控制面服务端
- `cmd/tunnel-agent`：本地 agent 命令行版本
- `internal/contracts`：接口结构
- `internal/server`：服务端在线状态管理

## 启动服务端

```powershell
cd E:\chatgptcodex\newipad0869\demo-go-tunnel
go run .\cmd\tunnel-server
```

默认监听：

- HTTP：`0.0.0.0:9080`
- TCP 隧道：`0.0.0.0:9081`
- SOCKS5 入口：`0.0.0.0:21080`

## 启动 agent

```powershell
cd E:\chatgptcodex\newipad0869\demo-go-tunnel
go run .\cmd\tunnel-agent -tunnel 127.0.0.1:9081 -name win-client-01 -token demo-secret
```

## 已实现接口

- `POST /api/v1/agents/register`
- `POST /api/v1/agents/heartbeat`
- `GET /api/v1/agents`
- `GET /healthz`

## 已实现长连接能力

- agent 通过 TCP 连接服务端
- 首包注册获取 `agent_id` 和 `proxy_id`
- 长连接 `ping/pong`
- 断线后 agent 自动重连

## 已实现代理入口

- 服务端提供标准 SOCKS5 入口
- SOCKS5 用户名使用 `proxy_id`
- SOCKS5 密码使用共享 token
- 服务端收到 CONNECT 请求后，会通过 agent 长连接转发到本地机器出网

## Linux 部署文件

- `deploy/linux/tunnel-server.service`
- `deploy/linux/tunnel-server.env.example`
- `scripts/deploy_server.py`

## 下一步

- 加入 WebSocket 长连接
- 服务端分配标准 SOCKS5 入口
- 代理数据流转发到本地 agent
