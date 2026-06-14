package server

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

const (
	defaultHeartbeatInterval = 15 * time.Second
	offlineAfter             = 45 * time.Second
)

type AgentRecord struct {
	AgentID      string
	ProxyID      string
	AgentName    string
	Version      string
	RemoteAddr   string
	RegisteredAt time.Time
	LastSeenAt   time.Time
}

type ProxyConfig struct {
	ProxyID   string
	Remark    string
	Enabled   bool
	Permanent bool
	ExpiresAt time.Time
	UpdatedAt time.Time
}

type AgentStore interface {
	RegisterOrResume(agentID, proxyID, agentName, version, remoteAddr string) (*AgentRecord, error)
	Touch(agentID, proxyID, version, remoteAddr string) (*AgentRecord, bool, error)
	List() ([]contracts.AgentSummary, error)
	GetByProxyID(proxyID string) (*AgentRecord, bool, error)
	GetProxyConfig(proxyID string) (*ProxyConfig, error)
	SaveProxyConfig(cfg ProxyConfig) (*ProxyConfig, error)
	ListProxyConfigs() ([]ProxyConfig, error)
	HealthCheck(ctx context.Context) error
	BackendName() string
}

type MemoryRegistry struct {
	mu      sync.RWMutex
	byAgent map[string]*AgentRecord
	byProxy map[string]*AgentRecord
	proxyCfg map[string]*ProxyConfig
}

func NewRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		byAgent: make(map[string]*AgentRecord),
		byProxy: make(map[string]*AgentRecord),
		proxyCfg: make(map[string]*ProxyConfig),
	}
}

func (r *MemoryRegistry) BackendName() string {
	return "memory"
}

func (r *MemoryRegistry) HealthCheck(_ context.Context) error {
	return nil
}

func (r *MemoryRegistry) Register(agentName, version, remoteAddr string) *AgentRecord {
	record, _ := r.RegisterOrResume("", "", agentName, version, remoteAddr)
	return record
}

func (r *MemoryRegistry) RegisterOrResume(agentID, proxyID, agentName, version, remoteAddr string) (*AgentRecord, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	now := time.Now()
	if agentID != "" {
		if record, ok := r.byAgent[agentID]; ok {
			if proxyID == "" || proxyID == record.ProxyID {
				record.AgentName = firstNonEmpty(agentName, record.AgentName)
				record.Version = firstNonEmpty(version, record.Version)
				record.RemoteAddr = firstNonEmpty(remoteAddr, record.RemoteAddr)
				record.LastSeenAt = now
				r.ensureProxyConfigLocked(record.ProxyID)
				return cloneRecord(record), nil
			}
		}
	}
	if proxyID != "" {
		if record, ok := r.byProxy[proxyID]; ok {
			if agentID != "" && record.AgentID != agentID {
				delete(r.byAgent, record.AgentID)
				record.AgentID = agentID
			}
			record.AgentName = firstNonEmpty(agentName, record.AgentName)
			record.Version = firstNonEmpty(version, record.Version)
			record.RemoteAddr = firstNonEmpty(remoteAddr, record.RemoteAddr)
			record.LastSeenAt = now
			r.byAgent[record.AgentID] = record
			r.byProxy[record.ProxyID] = record
			r.ensureProxyConfigLocked(record.ProxyID)
			return cloneRecord(record), nil
		}
	}

	record := &AgentRecord{
		AgentID:      firstNonEmpty(agentID, "agent_"+randomHex(8)),
		ProxyID:      firstNonEmpty(proxyID, randomUUIDLike()),
		AgentName:    agentName,
		Version:      version,
		RemoteAddr:   remoteAddr,
		RegisteredAt: now,
		LastSeenAt:   now,
	}
	r.byAgent[record.AgentID] = record
	r.byProxy[record.ProxyID] = record
	r.ensureProxyConfigLocked(record.ProxyID)
	return cloneRecord(record), nil
}

func (r *MemoryRegistry) Touch(agentID, proxyID, version, remoteAddr string) (*AgentRecord, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	record, ok := r.byAgent[agentID]
	if !ok && proxyID != "" {
		record, ok = r.byProxy[proxyID]
	}
	if !ok {
		return nil, false, nil
	}
	if proxyID != "" && record.ProxyID != proxyID {
		return nil, false, nil
	}

	record.LastSeenAt = time.Now()
	if version != "" {
		record.Version = version
	}
	if remoteAddr != "" {
		record.RemoteAddr = remoteAddr
	}
	return cloneRecord(record), true, nil
}

func (r *MemoryRegistry) GetByProxyID(proxyID string) (*AgentRecord, bool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	record, ok := r.byProxy[proxyID]
	if !ok {
		return nil, false, nil
	}
	return cloneRecord(record), true, nil
}

func (r *MemoryRegistry) GetProxyConfig(proxyID string) (*ProxyConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	cfg := r.ensureProxyConfigLocked(strings.TrimSpace(proxyID))
	return cloneProxyConfig(cfg), nil
}

func (r *MemoryRegistry) SaveProxyConfig(cfg ProxyConfig) (*ProxyConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	saved := normalizeProxyConfig(cfg)
	if saved.ProxyID == "" {
		return nil, fmt.Errorf("proxy_id is required")
	}
	r.proxyCfg[saved.ProxyID] = &saved
	return cloneProxyConfig(&saved), nil
}

func (r *MemoryRegistry) ListProxyConfigs() ([]ProxyConfig, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	items := make([]ProxyConfig, 0, len(r.proxyCfg))
	for proxyID := range r.proxyCfg {
		cfg := r.ensureProxyConfigLocked(proxyID)
		if cfg != nil {
			items = append(items, *cloneProxyConfig(cfg))
		}
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].UpdatedAt.Equal(items[j].UpdatedAt) {
			return items[i].ProxyID < items[j].ProxyID
		}
		return items[i].UpdatedAt.After(items[j].UpdatedAt)
	})
	return items, nil
}

func (r *MemoryRegistry) List() ([]contracts.AgentSummary, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]contracts.AgentSummary, 0, len(r.byAgent))
	now := time.Now()
	for _, record := range r.byAgent {
		items = append(items, recordToSummary(record, now))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RegisteredAt.After(items[j].RegisteredAt)
	})
	return items, nil
}

func HeartbeatInterval() time.Duration {
	return defaultHeartbeatInterval
}

func recordToSummary(record *AgentRecord, now time.Time) contracts.AgentSummary {
	status := "online"
	if now.Sub(record.LastSeenAt) > offlineAfter {
		status = "offline"
	}
	return contracts.AgentSummary{
		AgentID:      record.AgentID,
		ProxyID:      record.ProxyID,
		AgentName:    record.AgentName,
		Version:      record.Version,
		RemoteAddr:   record.RemoteAddr,
		LastSeenAt:   record.LastSeenAt,
		RegisteredAt: record.RegisteredAt,
		Status:       status,
	}
}

func cloneRecord(record *AgentRecord) *AgentRecord {
	if record == nil {
		return nil
	}
	copy := *record
	return &copy
}

func cloneProxyConfig(cfg *ProxyConfig) *ProxyConfig {
	if cfg == nil {
		return nil
	}
	copy := *cfg
	return &copy
}

func defaultProxyConfig(proxyID string) ProxyConfig {
	return ProxyConfig{
		ProxyID:   strings.TrimSpace(proxyID),
		Enabled:   true,
		Permanent: true,
		UpdatedAt: time.Now().UTC(),
	}
}

func normalizeProxyConfig(cfg ProxyConfig) ProxyConfig {
	normalized := cfg
	normalized.ProxyID = strings.TrimSpace(normalized.ProxyID)
	normalized.Remark = strings.TrimSpace(normalized.Remark)
	if normalized.ProxyID == "" {
		return normalized
	}
	if normalized.UpdatedAt.IsZero() {
		normalized.UpdatedAt = time.Now().UTC()
	} else {
		normalized.UpdatedAt = normalized.UpdatedAt.UTC()
	}
	if normalized.Permanent {
		normalized.ExpiresAt = time.Time{}
	}
	if !normalized.Permanent && normalized.ExpiresAt.IsZero() {
		normalized.Permanent = true
	}
	if normalized.ExpiresAt.IsZero() {
		normalized.Permanent = true
	}
	normalized.Enabled = normalized.Enabled
	return normalized
}

func (cfg ProxyConfig) IsExpired(now time.Time) bool {
	if cfg.Permanent || cfg.ExpiresAt.IsZero() {
		return false
	}
	return now.After(cfg.ExpiresAt)
}

func (r *MemoryRegistry) ensureProxyConfigLocked(proxyID string) *ProxyConfig {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return nil
	}
	if cfg, ok := r.proxyCfg[proxyID]; ok && cfg != nil {
		normalized := normalizeProxyConfig(*cfg)
		r.proxyCfg[proxyID] = &normalized
		return r.proxyCfg[proxyID]
	}
	defaultCfg := defaultProxyConfig(proxyID)
	r.proxyCfg[proxyID] = &defaultCfg
	return r.proxyCfg[proxyID]
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

func randomHex(n int) string {
	buf := make([]byte, n)
	if _, err := rand.Read(buf); err != nil {
		return hex.EncodeToString([]byte(time.Now().Format("150405.000000000")))
	}
	return hex.EncodeToString(buf)
}

func randomUUIDLike() string {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "proxy-" + randomHex(8)
	}
	buf[6] = (buf[6] & 0x0f) | 0x40
	buf[8] = (buf[8] & 0x3f) | 0x80
	hexValue := hex.EncodeToString(buf)
	return fmt.Sprintf("%s-%s-%s-%s-%s", hexValue[0:8], hexValue[8:12], hexValue[12:16], hexValue[16:20], hexValue[20:32])
}

func NewAgentStoreFromEnv(logger *log.Logger) (AgentStore, error) {
	cfg := LoadConfigFromEnv()
	if cfg.RedisAddr == "" {
		return nil, fmt.Errorf("TUNNEL_REDIS_ADDR is required for production deployment")
	}
	return NewRedisRegistry(cfg, logger)
}
