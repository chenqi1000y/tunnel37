#!/usr/bin/env python3
from __future__ import annotations

import argparse
import secrets
import shlex
import sys
import textwrap
from pathlib import Path

import paramiko


def parse_args() -> argparse.Namespace:
    parser = argparse.ArgumentParser(description="Deploy tunnel.ma37.com server to Linux/BaoTa over SSH.")
    parser.add_argument("--host", required=True)
    parser.add_argument("--port", type=int, default=22)
    parser.add_argument("--user", required=True)
    parser.add_argument("--password", required=True)
    parser.add_argument("--domain", required=True)
    parser.add_argument("--release-dir", default=str(Path(__file__).resolve().parents[1] / "release" / "linux-amd64"))
    parser.add_argument("--remote-dir", default="/opt/demo-go-tunnel")
    parser.add_argument("--service-name", default="tunnel-server.service")
    parser.add_argument("--nginx-conf-dir", default="/www/server/panel/vhost/nginx")
    parser.add_argument("--shared-token", default="")
    parser.add_argument("--admin-user", default="admin")
    parser.add_argument("--admin-pass", default="")
    parser.add_argument("--redis-addr", default="127.0.0.1:6379")
    parser.add_argument("--redis-password", default="")
    parser.add_argument("--redis-db", type=int, default=0)
    parser.add_argument("--socks-port", type=int, default=21080)
    parser.add_argument("--relay-port", type=int, default=9081)
    parser.add_argument("--skip-nginx", action="store_true", help="Do not write or reload BaoTa/Nginx config.")
    parser.add_argument("--skip-firewall", action="store_true", help="Do not modify ufw/firewalld rules.")
    parser.add_argument("--no-rollback", action="store_true", help="Do not restore previous binary if health checks fail.")
    return parser.parse_args()


def q(value: str) -> str:
    return shlex.quote(value)


def build_service_text(remote_dir: str) -> str:
    return textwrap.dedent(
        f"""
        [Unit]
        Description=tunnel.ma37.com Tunnel Server
        After=network.target redis.service

        [Service]
        Type=simple
        WorkingDirectory={remote_dir}
        EnvironmentFile={remote_dir}/tunnel-server.env
        ExecStart={remote_dir}/tunnel-server
        Restart=always
        RestartSec=3
        User=root
        LimitNOFILE=1048576
        StandardOutput=append:{remote_dir}/logs/tunnel-server.log
        StandardError=append:{remote_dir}/logs/tunnel-server.err.log

        [Install]
        WantedBy=multi-user.target
        """
    ).strip() + "\n"


def build_env_text(args: argparse.Namespace, shared_token: str, admin_pass: str) -> str:
    domain = args.domain.strip()
    return textwrap.dedent(
        f"""
        TUNNEL_SERVER_ADDR=127.0.0.1:9080
        TUNNEL_TCP_ADDR=0.0.0.0:{args.relay_port}
        TUNNEL_SOCKS_ADDR=0.0.0.0:{args.socks_port}
        TUNNEL_SHARED_TOKEN={shared_token}
        TUNNEL_ADMIN_USER={args.admin_user}
        TUNNEL_ADMIN_PASS={admin_pass}
        TUNNEL_REDIS_ADDR={args.redis_addr}
        TUNNEL_REDIS_PASSWORD={args.redis_password}
        TUNNEL_REDIS_DB={args.redis_db}
        TUNNEL_REDIS_PREFIX=tunnel
        TUNNEL_PUBLIC_BASE=https://{domain}
        TUNNEL_PUBLIC_RELAY_ADDR={domain}:{args.relay_port}
        TUNNEL_SOCKS_HOST={domain}
        TUNNEL_SOCKS_PORT_PUBLIC={args.socks_port}
        """
    ).strip() + "\n"


def build_nginx_text(domain: str) -> str:
    return textwrap.dedent(
        f"""
        server {{
            listen 80;
            server_name {domain};

            location / {{
                proxy_pass http://127.0.0.1:9080;
                proxy_http_version 1.1;
                proxy_set_header Host $host;
                proxy_set_header X-Real-IP $remote_addr;
                proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
                proxy_set_header X-Forwarded-Proto $scheme;
            }}
        }}
        """
    ).strip() + "\n"


def run(client: paramiko.SSHClient, command: str, timeout: int = 60) -> tuple[int, str, str]:
    _, stdout, stderr = client.exec_command(command, timeout=timeout)
    out = stdout.read().decode("utf-8", errors="ignore")
    err = stderr.read().decode("utf-8", errors="ignore")
    code = stdout.channel.recv_exit_status()
    return code, out, err


def ensure_success(code: int, out: str, err: str, step: str) -> None:
    if code == 0:
        return
    raise RuntimeError(f"{step} failed with exit={code}\nSTDOUT:\n{out}\nSTDERR:\n{err}")


def print_block(title: str, out: str = "", err: str = "") -> None:
    print(f"---{title.upper()}---")
    if out.strip():
        print(out.strip())
    if err.strip():
        print(err.strip())


def run_step(client: paramiko.SSHClient, title: str, command: str, *, required: bool = True, timeout: int = 60) -> tuple[int, str, str]:
    code, out, err = run(client, command, timeout=timeout)
    print_block(title, out, err)
    if required:
        ensure_success(code, out, err, title)
    return code, out, err


def remote_timestamp(client: paramiko.SSHClient) -> str:
    code, out, err = run(client, "date +%Y%m%d-%H%M%S")
    ensure_success(code, out, err, "get remote timestamp")
    return out.strip()


def rollback_binary(client: paramiko.SSHClient, service_name: str, remote_bin: str, backup_bin: str) -> None:
    print("---ROLLBACK---")
    command = (
        f"if [ -f {q(backup_bin)} ]; then "
        f"cp -a {q(backup_bin)} {q(remote_bin)} && chmod +x {q(remote_bin)} && systemctl restart {q(service_name)}; "
        f"else echo 'backup binary not found: {backup_bin}' >&2; exit 1; fi"
    )
    code, out, err = run(client, command, timeout=120)
    if out.strip():
        print(out.strip())
    if err.strip():
        print(err.strip())
    if code == 0:
        print("rollback complete")
    else:
        print(f"rollback failed with exit={code}", file=sys.stderr)


def main() -> int:
    args = parse_args()
    release_dir = Path(args.release_dir)
    binary_path = release_dir / "tunnel-server"
    if not binary_path.exists():
        raise FileNotFoundError(f"missing binary: {binary_path}")

    shared_token = args.shared_token or secrets.token_urlsafe(24)
    admin_pass = args.admin_pass or secrets.token_urlsafe(18)
    remote_dir = args.remote_dir.rstrip("/")
    remote_bin = f"{remote_dir}/tunnel-server"
    remote_env = f"{remote_dir}/tunnel-server.env"
    remote_backup_dir = f"{remote_dir}/backups"
    remote_service = f"/etc/systemd/system/{args.service_name}"
    remote_nginx_conf = f"{args.nginx_conf_dir.rstrip('/')}/{args.domain}.conf"

    service_text = build_service_text(remote_dir)
    env_text = build_env_text(args, shared_token, admin_pass)
    nginx_text = build_nginx_text(args.domain)

    client = paramiko.SSHClient()
    client.set_missing_host_key_policy(paramiko.AutoAddPolicy())
    client.connect(
        hostname=args.host,
        port=args.port,
        username=args.user,
        password=args.password,
        timeout=20,
        banner_timeout=30,
        auth_timeout=30,
    )

    try:
        run_step(client, "remote os", "uname -a && echo ==== && cat /etc/os-release", required=False)
        run_step(client, "precheck tools", "command -v systemctl && command -v ss && command -v curl", timeout=20)
        run_step(
            client,
            "precheck redis",
            "if command -v redis-cli >/dev/null 2>&1; then redis-cli -h 127.0.0.1 -p 6379 ping || true; else echo 'redis-cli not installed, skip direct redis ping'; fi",
            required=False,
            timeout=20,
        )
        run_step(client, "precheck ports", f"ss -lntp | grep -E ':9080|:{args.relay_port}|:{args.socks_port}' || true", required=False, timeout=20)

        stamp = remote_timestamp(client)
        backup_bin = f"{remote_backup_dir}/tunnel-server.{stamp}"
        backup_env = f"{remote_backup_dir}/tunnel-server.env.{stamp}"
        backup_nginx_conf = f"{remote_backup_dir}/{args.domain}.conf.{stamp}"

        run_step(client, "prepare dirs", f"mkdir -p {q(remote_dir)} {q(remote_dir + '/logs')} {q(remote_backup_dir)} {q(args.nginx_conf_dir.rstrip('/'))}")
        run_step(
            client,
            "backup current files",
            " && ".join(
                [
                    f"if [ -f {q(remote_bin)} ]; then cp -a {q(remote_bin)} {q(backup_bin)}; else echo 'no existing binary to backup'; fi",
                    f"if [ -f {q(remote_env)} ]; then cp -a {q(remote_env)} {q(backup_env)}; else echo 'no existing env to backup'; fi",
                    f"if [ -f {q(remote_nginx_conf)} ]; then cp -a {q(remote_nginx_conf)} {q(backup_nginx_conf)}; else echo 'no existing nginx conf to backup'; fi",
                ]
            ),
            required=False,
        )

        sftp = client.open_sftp()
        try:
            sftp.put(str(binary_path), remote_bin)
            with sftp.file(remote_env, "w") as f:
                f.write(env_text)
            with sftp.file(remote_service, "w") as f:
                f.write(service_text)
            if not args.skip_nginx:
                with sftp.file(remote_nginx_conf, "w") as f:
                    f.write(nginx_text)
        finally:
            sftp.close()

        try:
            run_step(client, "chmod binary", f"chmod +x {q(remote_bin)} && chmod 600 {q(remote_env)}")
            run_step(client, "systemd reload", "systemctl daemon-reload")
            run_step(client, "enable service", f"systemctl enable --now {q(args.service_name)}")
            run_step(client, "restart service", f"systemctl restart {q(args.service_name)}", timeout=120)
            run_step(client, "service status", f"systemctl status {q(args.service_name)} --no-pager -l | head -n 40", required=False)
            run_step(client, "listen ports", f"ss -lntp | grep -E ':9080|:{args.relay_port}|:{args.socks_port}'", timeout=20)
            run_step(client, "readyz", "curl -fsS --max-time 5 http://127.0.0.1:9080/readyz")
            run_step(client, "runtime", f"curl -fsS --max-time 5 -u {q(args.admin_user + ':' + admin_pass)} http://127.0.0.1:9080/api/admin/runtime")
        except Exception:
            if not args.no_rollback:
                rollback_binary(client, args.service_name, remote_bin, backup_bin)
            raise

        if not args.skip_nginx:
            run_step(client, "nginx", "if command -v nginx >/dev/null 2>&1; then nginx -t && systemctl reload nginx; else echo 'nginx not installed, skip'; fi", required=False)

        if not args.skip_firewall:
            run_step(client, "ufw", f"if command -v ufw >/dev/null 2>&1; then ufw allow 80/tcp; ufw allow 443/tcp; ufw allow {args.relay_port}/tcp; ufw allow {args.socks_port}/tcp; else echo 'ufw not installed, skip'; fi", required=False)
            run_step(client, "firewalld", f"if command -v firewall-cmd >/dev/null 2>&1; then firewall-cmd --permanent --add-port=80/tcp; firewall-cmd --permanent --add-port=443/tcp; firewall-cmd --permanent --add-port={args.relay_port}/tcp; firewall-cmd --permanent --add-port={args.socks_port}/tcp; firewall-cmd --reload; else echo 'firewalld not installed, skip'; fi", required=False)

        print("---DEPLOY-SUMMARY---")
        print(f"admin_url=https://{args.domain}/admin")
        print(f"admin_user={args.admin_user}")
        print(f"admin_pass={admin_pass}")
        print(f"shared_token={shared_token}")
        print(f"backup_binary={backup_bin}")
        print(f"agent_command=.\\tunnel-agent.exe -tunnel {args.domain}:{args.relay_port} -name client-pc -token {shared_token}")
        print(f"socks_template=socks5://<proxy_id>:{shared_token}@{args.domain}:{args.socks_port}")
        return 0
    finally:
        client.close()


if __name__ == "__main__":
    try:
        raise SystemExit(main())
    except Exception as exc:  # noqa: BLE001
        print(f"deploy failed: {exc}", file=sys.stderr)
        raise
