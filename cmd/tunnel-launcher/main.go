package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"html/template"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

const launcherVersion = "1.0.0"

var proxyIDPattern = regexp.MustCompile(`proxy_id=([0-9a-fA-F-]{36})`)

type app struct {
	mu         sync.RWMutex
	agentPath  string
	tunnelAddr string
	agentName  string
	token      string
	demoBase   string
	proc       *exec.Cmd
	status     string
	proxyID    string
	lastTest   string
	startedAt  time.Time
	logs       []string
}

func main() {
	listen := flag.String("listen", "127.0.0.1:18880", "本地界面监听地址")
	tunnelAddr := flag.String("tunnel", "tunnel.ma37.com:9081", "隧道服务地址")
	agentName := flag.String("name", hostnameOr("windows-agent"), "Agent 名称")
	token := flag.String("token", "demo-secret", "连接密钥")
	demoBase := flag.String("demo-base", "http://106.53.68.229:18080", "Demo 接口地址")
	agentPath := flag.String("agent-path", "", "tunnel-agent 可执行文件路径")
	noOpen := flag.Bool("no-open", false, "启动后不自动打开浏览器")
	flag.Parse()

	logger := log.New(os.Stdout, "[tunnel-launcher] ", log.LstdFlags)
	app := &app{
		agentPath:  firstNonEmpty(strings.TrimSpace(*agentPath), defaultAgentPath()),
		tunnelAddr: strings.TrimSpace(*tunnelAddr),
		agentName:  strings.TrimSpace(*agentName),
		token:      strings.TrimSpace(*token),
		demoBase:   strings.TrimRight(strings.TrimSpace(*demoBase), "/"),
		status:     "未启动",
		lastTest:   "尚未测试",
		logs:       make([]string, 0, 120),
	}
	app.log("欢迎使用 Go 本地代理轻量版。")
	app.log("如果本地已有历史代理ID，tunnel-agent 会优先复用它。")

	mux := http.NewServeMux()
	mux.HandleFunc("/", app.handleIndex)
	mux.HandleFunc("/api/status", app.handleStatus)
	mux.HandleFunc("/api/start", app.handleStart)
	mux.HandleFunc("/api/stop", app.handleStop)
	mux.HandleFunc("/api/test", app.handleTest)

	uiURL := "http://" + *listen
	logger.Printf("launcher ui listening on %s", uiURL)
	if !*noOpen {
		go func() {
			time.Sleep(400 * time.Millisecond)
			if err := openBrowser(uiURL); err != nil {
				logger.Printf("自动打开浏览器失败，请手动访问 %s: %v", uiURL, err)
			}
		}()
	}
	if err := http.ListenAndServe(*listen, mux); err != nil {
		logger.Fatalf("launcher 启动失败: %v", err)
	}
}

func (a *app) handleIndex(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.NotFound(w, r)
		return
	}
	_ = pageTmpl.Execute(w, map[string]string{
		"Version": launcherVersion,
	})
}

func (a *app) handleStatus(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	a.mu.RLock()
	defer a.mu.RUnlock()
	writeJSON(w, http.StatusOK, map[string]any{
		"status":      a.status,
		"proxy_id":    a.proxyID,
		"last_test":   a.lastTest,
		"uptime":      formatUptime(a.startedAt, a.status == "已启动"),
		"logs":        a.logs,
		"agent_name":  a.agentName,
		"tunnel_addr": a.tunnelAddr,
		"agent_path":  a.agentPath,
		"version":     launcherVersion,
	})
}

func (a *app) handleStart(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	if err := a.start(); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleStop(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	a.stop()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

func (a *app) handleTest(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]any{"error": "method not allowed"})
		return
	}
	result, err := a.test()
	if err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "result": result})
}

func (a *app) start() error {
	a.mu.Lock()
	if a.proc != nil && a.proc.Process != nil {
		a.mu.Unlock()
		return fmt.Errorf("本地代理已在运行")
	}
	agentPath := a.agentPath
	tunnelAddr := a.tunnelAddr
	agentName := a.agentName
	token := a.token
	a.status = "启动中"
	a.proxyID = ""
	a.lastTest = "尚未测试"
	a.startedAt = time.Time{}
	a.mu.Unlock()

	if _, err := os.Stat(agentPath); err != nil {
		a.setStatus("启动失败")
		return fmt.Errorf("未找到 tunnel-agent: %s", agentPath)
	}

	cmd := exec.Command(agentPath, "-tunnel", tunnelAddr, "-name", agentName, "-token", token)
	cmd.Dir = filepath.Dir(agentPath)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		a.setStatus("启动失败")
		return err
	}
	stderr, err := cmd.StderrPipe()
	if err != nil {
		a.setStatus("启动失败")
		return err
	}
	if err := cmd.Start(); err != nil {
		a.setStatus("启动失败")
		return err
	}

	a.mu.Lock()
	a.proc = cmd
	a.mu.Unlock()
	a.log("已启动 Go 本地代理进程。")

	go a.readPipe(stdout, false)
	go a.readPipe(stderr, true)
	go a.waitExit(cmd)
	return nil
}

func (a *app) stop() {
	a.mu.Lock()
	cmd := a.proc
	a.proc = nil
	a.status = "已停止"
	a.startedAt = time.Time{}
	a.mu.Unlock()
	if cmd != nil && cmd.Process != nil {
		_ = cmd.Process.Kill()
	}
	a.log("已停止本地代理。")
}

func (a *app) test() (map[string]any, error) {
	a.mu.RLock()
	proxyID := a.proxyID
	demoBase := a.demoBase
	a.mu.RUnlock()
	if strings.TrimSpace(proxyID) == "" {
		return nil, fmt.Errorf("当前还没有代理ID")
	}

	url := demoBase + "/api/demo/tunnel/test?proxy_id=" + proxyID
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var data map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, err
	}
	raw, _ := json.Marshal(data["data"])
	var body map[string]any
	_ = json.Unmarshal(raw, &body)
	success, _ := body["success"].(bool)
	exitIP, _ := body["exit_ip"].(string)
	if success {
		a.mu.Lock()
		a.lastTest = "测试成功"
		a.mu.Unlock()
		a.log("测试成功，真实代理已连通，出口 IP: " + firstNonEmpty(exitIP, "-"))
	} else {
		msg, _ := body["message"].(string)
		a.mu.Lock()
		a.lastTest = "测试失败"
		a.mu.Unlock()
		a.log("测试失败: " + firstNonEmpty(msg, "未知错误"))
	}
	return body, nil
}

func (a *app) readPipe(r io.ReadCloser, isErr bool) {
	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if isErr {
			a.log("错误: " + line)
		} else {
			a.log(line)
		}
		if match := proxyIDPattern.FindStringSubmatch(line); len(match) == 2 {
			a.mu.Lock()
			a.proxyID = match[1]
			a.status = "已启动"
			if a.startedAt.IsZero() {
				a.startedAt = time.Now()
			}
			a.mu.Unlock()
			a.log("已获取代理ID: " + match[1])
		}
	}
}

func (a *app) waitExit(cmd *exec.Cmd) {
	err := cmd.Wait()
	a.mu.Lock()
	if a.proc == cmd {
		a.proc = nil
		if a.status != "已停止" {
			a.status = "代理中断"
			a.startedAt = time.Time{}
		}
	}
	a.mu.Unlock()
	if err != nil {
		a.log("代理进程退出: " + err.Error())
	} else {
		a.log("代理进程已退出。")
	}
}

func (a *app) setStatus(status string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.status = status
}

func (a *app) log(line string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	entry := fmt.Sprintf("[%s] %s", time.Now().Format("15:04:05"), line)
	a.logs = append(a.logs, entry)
	if len(a.logs) > 120 {
		a.logs = append([]string(nil), a.logs[len(a.logs)-120:]...)
	}
}

func formatUptime(startedAt time.Time, online bool) string {
	if !online || startedAt.IsZero() {
		return "00:00:00"
	}
	secs := int(time.Since(startedAt).Seconds())
	h := secs / 3600
	m := (secs % 3600) / 60
	s := secs % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

func defaultAgentPath() string {
	exe, err := os.Executable()
	if err == nil {
		dir := filepath.Dir(exe)
		name := "tunnel-agent"
		if runtime.GOOS == "windows" {
			name += ".exe"
		}
		return filepath.Join(dir, name)
	}
	name := "tunnel-agent"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return name
}

func openBrowser(url string) error {
	switch runtime.GOOS {
	case "windows":
		return exec.Command("cmd", "/c", "start", "", url).Start()
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

var pageTmpl = template.Must(template.New("page").Parse(`
<!DOCTYPE html>
<html lang="zh-CN">
<head>
  <meta charset="UTF-8" />
  <meta name="viewport" content="width=device-width, initial-scale=1" />
  <title>Go 本地代理</title>
  <style>
    body { margin:0; font-family:"Segoe UI","Microsoft YaHei",sans-serif; background:linear-gradient(135deg,#0b1325,#122447 50%,#0e3a55); color:#f5f8ff; }
    .wrap { max-width:920px; margin:0 auto; padding:24px; }
    .card { background:rgba(255,255,255,.12); backdrop-filter:blur(18px); border:1px solid rgba(255,255,255,.2); border-radius:24px; padding:20px; box-shadow:0 20px 60px rgba(0,0,0,.22); }
    .grid { display:grid; grid-template-columns:1fr 1fr; gap:14px; margin-top:14px; }
    .pill { display:inline-block; padding:8px 14px; border-radius:999px; background:#ffd9d6; color:#b22d23; font-weight:700; }
    .ok { background:#daf8e7; color:#137744; }
    .mid { background:#fff0c7; color:#a66b00; }
    .mono { font-family:Consolas,monospace; word-break:break-all; }
    .actions { display:flex; gap:10px; flex-wrap:wrap; margin-top:16px; }
    button { border:0; border-radius:14px; padding:12px 18px; font-size:14px; font-weight:700; cursor:pointer; }
    .g { background:#1f8f5f; color:#fff; } .r { background:#cf3d36; color:#fff; } .b { background:#1f6feb; color:#fff; }
    pre { background:rgba(5,10,20,.45); border-radius:16px; padding:14px; min-height:180px; max-height:260px; overflow:auto; white-space:pre-wrap; }
    .sub { color:#c7d5ef; font-size:13px; }
  </style>
</head>
<body>
  <div class="wrap">
    <div class="card">
      <h2 style="margin:0 0 6px;">Go 本地代理轻量版</h2>
      <div class="sub">版本 v{{.Version}}，当前主线已切回 Go。</div>
      <div class="grid">
        <div>
          <div class="sub">当前状态</div>
          <div id="statusPill" class="pill">未启动</div>
        </div>
        <div>
          <div class="sub">启动时长</div>
          <div id="uptime">00:00:00</div>
        </div>
        <div style="grid-column:1 / span 2;">
          <div class="sub">代理ID</div>
          <div id="proxyId" class="mono">-</div>
        </div>
        <div>
          <div class="sub">最近测试</div>
          <div id="lastTest">尚未测试</div>
        </div>
        <div>
          <div class="sub">Agent 名称</div>
          <div id="agentName">-</div>
        </div>
      </div>
      <div class="actions">
        <button class="g" onclick="postAction('/api/start')">启动</button>
        <button class="r" onclick="postAction('/api/stop')">停止</button>
        <button class="b" onclick="postAction('/api/test')">测试代理</button>
        <button onclick="copyProxy()">复制代理ID</button>
      </div>
      <div style="margin-top:18px;" class="sub">运行日志</div>
      <pre id="logs"></pre>
    </div>
  </div>
  <script>
    async function loadStatus() {
      const res = await fetch('/api/status');
      const data = await res.json();
      document.getElementById('proxyId').textContent = data.proxy_id || '-';
      document.getElementById('uptime').textContent = data.uptime || '00:00:00';
      document.getElementById('lastTest').textContent = data.last_test || '尚未测试';
      document.getElementById('agentName').textContent = data.agent_name || '-';
      const pill = document.getElementById('statusPill');
      pill.textContent = data.status || '未知';
      pill.className = 'pill';
      if (data.status === '已启动') pill.classList.add('ok');
      else if (data.status === '启动中') pill.classList.add('mid');
      const logs = Array.isArray(data.logs) ? data.logs.join('\n') : '';
      document.getElementById('logs').textContent = logs;
    }
    async function postAction(url) {
      const res = await fetch(url, {method:'POST'});
      const data = await res.json();
      if (!data.ok && data.error) alert(data.error);
      if (data.result && data.result.exit_ip) alert('测试成功，出口 IP: ' + data.result.exit_ip);
      await loadStatus();
    }
    async function copyProxy() {
      const value = document.getElementById('proxyId').textContent;
      if (value && value !== '-') await navigator.clipboard.writeText(value);
    }
    loadStatus();
    setInterval(loadStatus, 2000);
  </script>
</body>
</html>
`))
