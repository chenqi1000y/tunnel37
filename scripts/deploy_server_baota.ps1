param(
    [Parameter(Mandatory=$true)][string]$HostName,
    [string]$Port = "22",
    [string]$User = "root",
    [Parameter(Mandatory=$true)][string]$Domain,
    [string]$RemoteDir = "/opt/demo-go-tunnel",
    [string]$ServiceName = "tunnel-server.service",
    [string]$NginxConfDir = "/www/server/panel/vhost/nginx",
    [string]$AdminUser = "admin",
    [Parameter(Mandatory=$true)][string]$AdminPass,
    [Parameter(Mandatory=$true)][string]$SharedToken,
    [string]$RedisAddr = "127.0.0.1:6379",
    [string]$RedisPassword = "",
    [int]$RedisDB = 0,
    [int]$RelayPort = 9081,
    [int]$SocksPort = 21080,
    [switch]$SkipNginx,
    [switch]$SkipFirewall
)

$ErrorActionPreference = "Stop"

$Root = Split-Path -Parent (Split-Path -Parent $MyInvocation.MyCommand.Path)
$Binary = Join-Path $Root "release\linux-amd64\tunnel-server"
if (!(Test-Path $Binary)) {
    throw "Missing Linux binary: $Binary. Run: `$env:GOOS='linux'; `$env:GOARCH='amd64'; go build -o release\linux-amd64\tunnel-server ./cmd/tunnel-server"
}

$RemoteBin = "$RemoteDir/tunnel-server"
$RemoteEnv = "$RemoteDir/tunnel-server.env"
$RemoteBackupDir = "$RemoteDir/backups"
$RemoteService = "/etc/systemd/system/$ServiceName"
$RemoteNginxConf = "$($NginxConfDir.TrimEnd('/'))/$Domain.conf"
$SshTarget = "$User@$HostName"
$SshBase = @("-p", $Port, $SshTarget)
$ScpBase = @("-P", $Port)

function Invoke-Remote {
    param([string]$Title, [string]$Command, [switch]$AllowFail)
    Write-Host "---$($Title.ToUpper())---"
    & ssh @SshBase $Command
    if ($LASTEXITCODE -ne 0 -and !$AllowFail) {
        throw "$Title failed with exit=$LASTEXITCODE"
    }
}

function Write-TempFile {
    param([string]$Content)
    $Path = [System.IO.Path]::GetTempFileName()
    Set-Content -Path $Path -Value $Content -Encoding UTF8
    return $Path
}

$ServiceText = @"
[Unit]
Description=tunnel.ma37.com Tunnel Server
After=network.target redis.service

[Service]
Type=simple
WorkingDirectory=$RemoteDir
EnvironmentFile=$RemoteEnv
ExecStart=$RemoteBin
Restart=always
RestartSec=3
User=root
LimitNOFILE=1048576
StandardOutput=append:$RemoteDir/logs/tunnel-server.log
StandardError=append:$RemoteDir/logs/tunnel-server.err.log

[Install]
WantedBy=multi-user.target
"@

$EnvText = @"
TUNNEL_SERVER_ADDR=127.0.0.1:9080
TUNNEL_TCP_ADDR=0.0.0.0:$RelayPort
TUNNEL_SOCKS_ADDR=0.0.0.0:$SocksPort
TUNNEL_SHARED_TOKEN=$SharedToken
TUNNEL_ADMIN_USER=$AdminUser
TUNNEL_ADMIN_PASS=$AdminPass
TUNNEL_REDIS_ADDR=$RedisAddr
TUNNEL_REDIS_PASSWORD=$RedisPassword
TUNNEL_REDIS_DB=$RedisDB
TUNNEL_REDIS_PREFIX=tunnel
TUNNEL_PUBLIC_BASE=https://$Domain
TUNNEL_PUBLIC_RELAY_ADDR=$Domain`:$RelayPort
TUNNEL_SOCKS_HOST=$Domain
TUNNEL_SOCKS_PORT_PUBLIC=$SocksPort
"@

$NginxText = @"
server {
    listen 80;
    server_name $Domain;

    location / {
        proxy_pass http://127.0.0.1:9080;
        proxy_http_version 1.1;
        proxy_set_header Host `$host;
        proxy_set_header X-Real-IP `$remote_addr;
        proxy_set_header X-Forwarded-For `$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto `$scheme;
    }
}
"@

$EnvFile = Write-TempFile $EnvText
$ServiceFile = Write-TempFile $ServiceText
$NginxFile = Write-TempFile $NginxText

try {
    Invoke-Remote "remote os" "uname -a && echo ==== && cat /etc/os-release" -AllowFail
    Invoke-Remote "precheck tools" "command -v systemctl && command -v ss && command -v curl"
    Invoke-Remote "precheck redis" "if command -v redis-cli >/dev/null 2>&1; then redis-cli -h 127.0.0.1 -p 6379 ping || true; else echo 'redis-cli not installed, skip direct redis ping'; fi" -AllowFail
    Invoke-Remote "precheck ports" "ss -lntp | grep -E ':9080|:$RelayPort|:$SocksPort' || true" -AllowFail

    $Stamp = (& ssh @SshBase "date +%Y%m%d-%H%M%S").Trim()
    $BackupBin = "$RemoteBackupDir/tunnel-server.$Stamp"
    $BackupEnv = "$RemoteBackupDir/tunnel-server.env.$Stamp"
    $BackupNginx = "$RemoteBackupDir/$Domain.conf.$Stamp"

    Invoke-Remote "prepare dirs" "mkdir -p '$RemoteDir' '$RemoteDir/logs' '$RemoteBackupDir' '$NginxConfDir'"
    Invoke-Remote "backup current files" "if [ -f '$RemoteBin' ]; then cp -a '$RemoteBin' '$BackupBin'; else echo 'no existing binary to backup'; fi; if [ -f '$RemoteEnv' ]; then cp -a '$RemoteEnv' '$BackupEnv'; else echo 'no existing env to backup'; fi; if [ -f '$RemoteNginxConf' ]; then cp -a '$RemoteNginxConf' '$BackupNginx'; else echo 'no existing nginx conf to backup'; fi" -AllowFail

    Write-Host "---UPLOAD---"
    & scp @ScpBase $Binary "$SshTarget`:$RemoteBin"
    if ($LASTEXITCODE -ne 0) { throw "upload tunnel-server failed" }
    & scp @ScpBase $EnvFile "$SshTarget`:$RemoteEnv"
    if ($LASTEXITCODE -ne 0) { throw "upload env failed" }
    & scp @ScpBase $ServiceFile "$SshTarget`:$RemoteService"
    if ($LASTEXITCODE -ne 0) { throw "upload service failed" }
    if (!$SkipNginx) {
        & scp @ScpBase $NginxFile "$SshTarget`:$RemoteNginxConf"
        if ($LASTEXITCODE -ne 0) { throw "upload nginx conf failed" }
    }

    try {
        Invoke-Remote "chmod binary" "chmod +x '$RemoteBin' && chmod 600 '$RemoteEnv'"
        Invoke-Remote "systemd reload" "systemctl daemon-reload"
        Invoke-Remote "enable service" "systemctl enable --now '$ServiceName'"
        Invoke-Remote "restart service" "systemctl restart '$ServiceName'"
        Invoke-Remote "service status" "systemctl status '$ServiceName' --no-pager -l | head -n 40" -AllowFail
        Invoke-Remote "listen ports" "ss -lntp | grep -E ':9080|:$RelayPort|:$SocksPort'"
        Invoke-Remote "readyz" "curl -fsS --max-time 5 http://127.0.0.1:9080/readyz"
        Invoke-Remote "runtime" "curl -fsS --max-time 5 -u '$AdminUser`:$AdminPass' http://127.0.0.1:9080/api/admin/runtime"
    } catch {
        Write-Host "---ROLLBACK---"
        & ssh @SshBase "if [ -f '$BackupBin' ]; then cp -a '$BackupBin' '$RemoteBin' && chmod +x '$RemoteBin' && systemctl restart '$ServiceName'; else echo 'backup binary not found: $BackupBin' >&2; exit 1; fi"
        throw
    }

    if (!$SkipNginx) {
        Invoke-Remote "nginx" "if command -v nginx >/dev/null 2>&1; then nginx -t && systemctl reload nginx; else echo 'nginx not installed, skip'; fi" -AllowFail
    }

    if (!$SkipFirewall) {
        Invoke-Remote "ufw" "if command -v ufw >/dev/null 2>&1; then ufw allow 80/tcp; ufw allow 443/tcp; ufw allow $RelayPort/tcp; ufw allow $SocksPort/tcp; else echo 'ufw not installed, skip'; fi" -AllowFail
        Invoke-Remote "firewalld" "if command -v firewall-cmd >/dev/null 2>&1; then firewall-cmd --permanent --add-port=80/tcp; firewall-cmd --permanent --add-port=443/tcp; firewall-cmd --permanent --add-port=$RelayPort/tcp; firewall-cmd --permanent --add-port=$SocksPort/tcp; firewall-cmd --reload; else echo 'firewalld not installed, skip'; fi" -AllowFail
    }

    Write-Host "---DEPLOY-SUMMARY---"
    Write-Host "admin_url=http://$Domain/admin"
    Write-Host "admin_user=$AdminUser"
    Write-Host "admin_pass=$AdminPass"
    Write-Host "shared_token=$SharedToken"
    Write-Host "backup_binary=$BackupBin"
    Write-Host "agent_command=.\tunnel-agent.exe -tunnel $Domain`:$RelayPort -name client-pc -token $SharedToken"
    Write-Host "socks_template=socks5://<proxy_id>:$SharedToken@$Domain`:$SocksPort"
} finally {
    Remove-Item -Force $EnvFile, $ServiceFile, $NginxFile -ErrorAction SilentlyContinue
}
