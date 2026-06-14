package server

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"sort"
	"strings"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

type HTTPServer struct {
	addr        string
	cfg         Config
	store       AgentStore
	relay       *RelayServer
	socks       *SocksEntryServer
	sharedToken string
	logger      *log.Logger
	httpServer  *http.Server
}

type adminProxySaveRequest struct {
	ProxyID   string `json:"proxy_id"`
	Remark    string `json:"remark"`
	Enabled   bool   `json:"enabled"`
	Permanent bool   `json:"permanent"`
	ExpiresAt string `json:"expires_at"`
}

func NewHTTPServer(cfg Config, store AgentStore, relay *RelayServer, sharedToken string, logger *log.Logger) *HTTPServer {
	if logger == nil {
		logger = log.Default()
	}
	if store == nil {
		store = NewRegistry()
	}
	return &HTTPServer{
		addr:        cfg.HTTPAddr,
		cfg:         cfg,
		store:       store,
		relay:       relay,
		sharedToken: sharedToken,
		logger:      logger,
	}
}

func (s *HTTPServer) AttachSocksServer(socks *SocksEntryServer) {
	s.socks = socks
}

func (s *HTTPServer) ListenAndServe() error {
	mux := http.NewServeMux()
	mux.HandleFunc("/livez", s.handleLivez)
	mux.HandleFunc("/healthz", s.handleHealthz)
	mux.HandleFunc("/readyz", s.handleHealthz)
	mux.HandleFunc("/admin", s.wrapAdmin(s.handleAdminPage))
	mux.HandleFunc("/admin/", s.wrapAdmin(s.handleAdminPage))
	mux.HandleFunc("/api/admin/overview", s.wrapAdmin(s.handleAdminOverview))
	mux.HandleFunc("/api/admin/runtime", s.wrapAdmin(s.handleAdminRuntime))
	mux.HandleFunc("/api/admin/proxies", s.wrapAdmin(s.handleAdminProxies))
	mux.HandleFunc("/api/admin/proxy", s.wrapAdmin(s.handleAdminProxy))
	mux.HandleFunc("/api/admin/proxy/save", s.wrapAdmin(s.handleAdminProxySave))
	mux.HandleFunc("/api/v1/agents/register", s.handleRegister)
	mux.HandleFunc("/api/v1/agents/heartbeat", s.handleHeartbeat)
	mux.HandleFunc("/api/v1/agents", s.handleListAgents)
	mux.HandleFunc("/api/demo/tunnel/agents", s.handleDemoAgents)
	mux.HandleFunc("/api/demo/tunnel/status", s.handleDemoStatus)
	mux.HandleFunc("/api/demo/tunnel/test", s.handleDemoTest)
	mux.HandleFunc("/t/", s.handleDemoStatusPage)

	s.httpServer = &http.Server{
		Addr:              s.addr,
		Handler:           s.logRequest(mux),
		ReadHeaderTimeout: 10 * time.Second,
	}
	s.logger.Printf("tunnel-server listening on %s with backend=%s", s.addr, s.store.BackendName())
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return err
	}
	return nil
}

func (s *HTTPServer) Shutdown(ctx context.Context) error {
	if s.httpServer == nil {
		return nil
	}
	return s.httpServer.Shutdown(ctx)
}

func (s *HTTPServer) handleLivez(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]any{
		"ok": true,
		"ts": time.Now().UTC(),
	})
}

func (s *HTTPServer) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	status := http.StatusOK
	payload := map[string]any{
		"ok":      true,
		"backend": s.store.BackendName(),
	}
	if err := s.store.HealthCheck(ctx); err != nil {
		status = http.StatusServiceUnavailable
		payload["ok"] = false
		payload["error"] = err.Error()
	}
	writeJSON(w, status, payload)
}

func (s *HTTPServer) handleAdminRuntime(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	redisOK := true
	redisError := ""
	if err := s.store.HealthCheck(ctx); err != nil {
		redisOK = false
		redisError = err.Error()
	}
	agentItems, err := s.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	onlineAgents := 0
	for _, item := range agentItems {
		if item.Status == "online" {
			onlineAgents++
		}
	}
	payload := map[string]any{
		"ok":              redisOK,
		"backend":         s.store.BackendName(),
		"redis_ok":        redisOK,
		"redis_error":     redisError,
		"agents_total":    len(agentItems),
		"agents_online":   onlineAgents,
		"agents_offline":  len(agentItems) - onlineAgents,
		"http_addr":       s.cfg.HTTPAddr,
		"relay_addr":      s.cfg.RelayAddr,
		"socks_addr":      s.cfg.SocksAddr,
		"public_base_url": s.cfg.PublicBaseURL,
		"updated_at":      time.Now().UTC(),
	}
	if s.relay != nil {
		payload["relay"] = s.relay.Stats()
	}
	if s.socks != nil {
		payload["socks"] = s.socks.Stats()
	}
	status := http.StatusOK
	if !redisOK {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, payload)
}

func (s *HTTPServer) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req contracts.RegisterAgentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	if strings.TrimSpace(req.AgentName) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "agent_name is required"})
		return
	}
	if !s.allowToken(req.AuthToken) {
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid auth token"})
		return
	}

	record, err := s.store.RegisterOrResume(
		strings.TrimSpace(req.AgentID),
		strings.TrimSpace(req.ProxyID),
		strings.TrimSpace(req.AgentName),
		strings.TrimSpace(req.Version),
		remoteIP(r),
	)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, contracts.RegisterAgentResponse{
		AgentID:              record.AgentID,
		ProxyID:              record.ProxyID,
		HeartbeatIntervalSec: int(HeartbeatInterval().Seconds()),
		ServerTime:           record.RegisteredAt,
	})
}

func (s *HTTPServer) handleHeartbeat(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}

	var req contracts.HeartbeatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}

	record, ok, err := s.store.Touch(strings.TrimSpace(req.AgentID), strings.TrimSpace(req.ProxyID), strings.TrimSpace(req.Version), remoteIP(r))
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !ok {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "agent not found"})
		return
	}
	writeJSON(w, http.StatusOK, contracts.HeartbeatResponse{OK: true, ServerTime: record.LastSeenAt})
}

func (s *HTTPServer) handleListAgents(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	items, err := s.store.List()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleDemoAgents(w http.ResponseWriter, _ *http.Request) {
	items, err := s.buildAdminProxyItems()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleDemoStatus(w http.ResponseWriter, r *http.Request) {
	proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
	if proxyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proxy_id is required"})
		return
	}
	payload, status, err := s.buildProxyStatus(proxyID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, status, payload)
}

func (s *HTTPServer) handleDemoTest(w http.ResponseWriter, r *http.Request) {
	proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
	if proxyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proxy_id is required"})
		return
	}
	payload, status, err := s.buildProxyStatus(proxyID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if ok, _ := payload["ok"].(bool); ok {
		payload["message"] = "proxy is available"
		payload["latency_ms"] = 1
	} else {
		payload["latency_ms"] = 0
		if stringValue(payload["message"]) == "" {
			payload["message"] = "proxy is unavailable"
		}
	}
	writeJSON(w, status, payload)
}

func (s *HTTPServer) handleDemoStatusPage(w http.ResponseWriter, r *http.Request) {
	short := strings.TrimSpace(strings.TrimPrefix(r.URL.Path, "/t/"))
	items, err := s.buildAdminProxyItems()
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	for _, item := range items {
		if shortCode(stringValue(item["proxy_id"])) != short {
			continue
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(fmt.Sprintf(
			"<html><body><h1>代理 %s</h1><p>状态：%s</p><p>设备：%s</p><p>最后心跳：%s</p><p>出口：%s</p></body></html>",
			stringValue(item["proxy_id"]),
			stringValue(item["status"]),
			stringValue(item["agent_name"]),
			stringValue(item["last_seen_at"]),
			stringValue(item["remote_ip"]),
		)))
		return
	}
	http.NotFound(w, r)
}

func (s *HTTPServer) handleAdminOverview(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	items, err := s.buildAdminProxyItems()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	online := 0
	disabled := 0
	expired := 0
	for _, item := range items {
		switch stringValue(item["status"]) {
		case "online":
			online++
		case "disabled":
			disabled++
		case "expired":
			expired++
		}
	}
	plan := DetectCapacityPlan()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":                   true,
		"backend":              s.store.BackendName(),
		"proxy_total":          len(items),
		"proxy_online":         online,
		"proxy_disabled":       disabled,
		"proxy_expired":        expired,
		"socks_host":           s.cfg.SocksHost,
		"socks_port":           s.cfg.SocksPort,
		"public_base_url":      s.cfg.PublicBaseURL,
		"public_relay_addr":    s.cfg.PublicRelayAddr,
		"admin_auth_enabled":   adminWantsAuth(s.cfg.AdminUser, s.cfg.AdminPass),
		"agent_command_sample": BuildAgentCommand(s.cfg.PublicRelayAddr, "客户电脑", s.sharedToken),
		"capacity":             plan,
		"updated_at":           time.Now().UTC(),
	})
}

func (s *HTTPServer) handleAdminProxies(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	items, err := s.buildAdminProxyItems()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *HTTPServer) handleAdminProxy(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	proxyID := strings.TrimSpace(r.URL.Query().Get("proxy_id"))
	if proxyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proxy_id is required"})
		return
	}
	item, found, err := s.buildAdminProxyItem(proxyID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if !found {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "proxy not found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *HTTPServer) handleAdminProxySave(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "method not allowed"})
		return
	}
	var req adminProxySaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid json"})
		return
	}
	proxyID := strings.TrimSpace(req.ProxyID)
	if proxyID == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "proxy_id is required"})
		return
	}
	expiresAt, err := parseAdminExpiry(req.ExpiresAt)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid expires_at"})
		return
	}
	saved, err := s.store.SaveProxyConfig(ProxyConfig{
		ProxyID:   proxyID,
		Remark:    req.Remark,
		Enabled:   req.Enabled,
		Permanent: req.Permanent,
		ExpiresAt: expiresAt,
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	item, _, err := s.buildAdminProxyItem(saved.ProxyID)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":      true,
		"message": "保存成功",
		"item":    item,
	})
}

func (s *HTTPServer) buildProxyStatus(proxyID string) (map[string]any, int, error) {
	item, found, err := s.buildAdminProxyItem(proxyID)
	if err != nil {
		return nil, 0, err
	}
	if !found {
		return map[string]any{"ok": false, "proxy_id": proxyID, "online": false, "message": "proxy not found"}, http.StatusNotFound, nil
	}
	status := http.StatusOK
	if !boolValue(item["ok"]) {
		status = http.StatusServiceUnavailable
	}
	return item, status, nil
}

func (s *HTTPServer) buildAdminProxyItems() ([]map[string]any, error) {
	records, err := s.store.List()
	if err != nil {
		return nil, err
	}
	configs, err := s.store.ListProxyConfigs()
	if err != nil {
		return nil, err
	}
	runtimeByProxy := make(map[string]contracts.AgentSummary, len(records))
	proxyIDs := make(map[string]struct{}, len(records)+len(configs))
	for _, record := range records {
		runtimeByProxy[record.ProxyID] = record
		proxyIDs[record.ProxyID] = struct{}{}
	}
	for _, cfg := range configs {
		proxyIDs[cfg.ProxyID] = struct{}{}
	}
	keys := make([]string, 0, len(proxyIDs))
	for proxyID := range proxyIDs {
		keys = append(keys, proxyID)
	}
	sort.Strings(keys)

	items := make([]map[string]any, 0, len(keys))
	for _, proxyID := range keys {
		item, found, err := s.buildAdminProxyItem(proxyID)
		if err != nil {
			return nil, err
		}
		if !found {
			if runtime, ok := runtimeByProxy[proxyID]; ok {
				item = s.adminProxyItemFromSummary(runtime, nil)
			} else {
				continue
			}
		}
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		leftOnline := boolValue(items[i]["online"])
		rightOnline := boolValue(items[j]["online"])
		if leftOnline != rightOnline {
			return leftOnline
		}
		return stringValue(items[i]["proxy_id"]) < stringValue(items[j]["proxy_id"])
	})
	return items, nil
}

func (s *HTTPServer) buildAdminProxyItem(proxyID string) (map[string]any, bool, error) {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return nil, false, nil
	}
	cfg, err := s.store.GetProxyConfig(proxyID)
	if err != nil {
		return nil, false, err
	}
	record, ok, err := s.store.GetByProxyID(proxyID)
	if err != nil {
		return nil, false, err
	}
	if !ok && cfg == nil {
		return nil, false, nil
	}
	return s.adminProxyItem(record, cfg), true, nil
}

func (s *HTTPServer) adminProxyItemFromSummary(summary contracts.AgentSummary, cfg *ProxyConfig) map[string]any {
	record := &AgentRecord{
		AgentID:      summary.AgentID,
		ProxyID:      summary.ProxyID,
		AgentName:    summary.AgentName,
		Version:      summary.Version,
		RemoteAddr:   summary.RemoteAddr,
		RegisteredAt: summary.RegisteredAt,
		LastSeenAt:   summary.LastSeenAt,
	}
	return s.adminProxyItem(record, cfg)
}

func (s *HTTPServer) adminProxyItem(record *AgentRecord, cfg *ProxyConfig) map[string]any {
	now := time.Now().UTC()
	if cfg == nil && record != nil {
		cfg = &ProxyConfig{ProxyID: record.ProxyID, Enabled: true, Permanent: true}
	}
	if cfg == nil {
		cfg = &ProxyConfig{Enabled: true, Permanent: true}
	}
	agentName := "客户电脑"
	if record != nil && strings.TrimSpace(record.AgentName) != "" {
		agentName = strings.TrimSpace(record.AgentName)
	}
	runtimeOnline := s.runtimeOnline(record)
	status := "offline"
	message := "代理离线"
	ok := false
	switch {
	case !cfg.Enabled:
		status = "disabled"
		message = "代理已禁用"
	case cfg.IsExpired(now):
		status = "expired"
		message = "代理已到期"
	case runtimeOnline:
		status = "online"
		message = "代理可用"
		ok = true
	default:
		status = "offline"
		message = "代理离线"
	}

	item := map[string]any{
		"ok":            ok,
		"proxy_id":      cfg.ProxyID,
		"status":        status,
		"message":       message,
		"online":        runtimeOnline,
		"enabled":       cfg.Enabled,
		"remark":        cfg.Remark,
		"permanent":     cfg.Permanent,
		"expires_at":    formatJSONTime(cfg.ExpiresAt),
		"socks_host":    s.cfg.SocksHost,
		"socks_port":    s.cfg.SocksPort,
		"socks_url":     BuildSocksURL(s.cfg.SocksHost, s.cfg.SocksPort, cfg.ProxyID, s.sharedToken),
		"status_link":   s.cfg.PublicBaseURL + "/t/" + shortCode(cfg.ProxyID),
		"agent_command": BuildAgentCommand(s.cfg.PublicRelayAddr, agentName, s.sharedToken),
		"updated_at":    formatJSONTime(cfg.UpdatedAt),
		"runtime_alive": runtimeOnline,
	}
	if record != nil {
		item["agent_id"] = record.AgentID
		item["agent_name"] = record.AgentName
		item["remote_ip"] = record.RemoteAddr
		item["last_seen_at"] = formatJSONTime(record.LastSeenAt)
		item["registered_at"] = formatJSONTime(record.RegisteredAt)
	}
	return item
}

func (s *HTTPServer) runtimeOnline(record *AgentRecord) bool {
	if record == nil {
		return false
	}
	online := time.Since(record.LastSeenAt) <= offlineAfter
	if s.relay != nil {
		_, relayOnline := s.relay.GetSessionByProxyID(record.ProxyID)
		online = online && relayOnline
	}
	return online
}

func (s *HTTPServer) allowToken(got string) bool {
	if s.sharedToken == "" {
		return true
	}
	return strings.TrimSpace(got) == s.sharedToken
}

func (s *HTTPServer) logRequest(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.logger.Printf("%s %s from %s", r.Method, r.URL.Path, remoteIP(r))
		next.ServeHTTP(w, r)
	})
}

func (s *HTTPServer) wrapAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if adminWantsAuth(s.cfg.AdminUser, s.cfg.AdminPass) && !basicAuthOK(r, s.cfg.AdminUser, s.cfg.AdminPass) {
			w.Header().Set("WWW-Authenticate", `Basic realm="Tunnel Admin"`)
			http.Error(w, "Unauthorized", http.StatusUnauthorized)
			return
		}
		next(w, r)
	}
}

func parseAdminExpiry(raw string) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, nil
	}
	layouts := []string{
		time.RFC3339,
		"2006-01-02T15:04",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, time.Local); err == nil {
			return parsed.UTC(), nil
		}
	}
	return time.Time{}, fmt.Errorf("unsupported expires_at")
}

func formatJSONTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func stringValue(value any) string {
	text, _ := value.(string)
	return text
}

func boolValue(value any) bool {
	flag, _ := value.(bool)
	return flag
}

func BuildAgentCommand(relayAddr, agentName, token string) string {
	relayAddr = strings.TrimSpace(relayAddr)
	if relayAddr == "" {
		relayAddr = "tunnel.ma37.com:9081"
	}
	agentName = strings.TrimSpace(agentName)
	if agentName == "" {
		agentName = "客户电脑"
	}
	return ".\\tunnel-agent.exe -tunnel " + powershellQuote(relayAddr) + " -name " + powershellQuote(agentName) + " -token " + powershellQuote(token)
}

func powershellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", "''") + "'"
}

func shortCode(proxyID string) string {
	if len(proxyID) <= 8 {
		return proxyID
	}
	return proxyID[:8]
}

func remoteIP(r *http.Request) string {
	host := strings.TrimSpace(r.Header.Get("X-Forwarded-For"))
	if host != "" {
		return strings.Split(host, ",")[0]
	}
	host = r.RemoteAddr
	if parsedHost, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		host = parsedHost
	}
	return host
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
