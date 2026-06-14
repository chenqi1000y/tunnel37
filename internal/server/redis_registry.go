package server

import (
	"context"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"polyquant/demo-go-tunnel/internal/contracts"
)

type RedisRegistry struct {
	client *redis.Client
	prefix string
	logger *log.Logger
}

func NewRedisRegistry(cfg Config, logger *log.Logger) (*RedisRegistry, error) {
	if strings.TrimSpace(cfg.RedisAddr) == "" {
		return nil, fmt.Errorf("redis address is empty")
	}
	if logger == nil {
		logger = log.Default()
	}
	client := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		return nil, err
	}
	return &RedisRegistry{
		client: client,
		prefix: strings.TrimSpace(cfg.RedisPrefix),
		logger: logger,
	}, nil
}

func (r *RedisRegistry) BackendName() string {
	return "redis"
}

func (r *RedisRegistry) HealthCheck(ctx context.Context) error {
	return r.client.Ping(ctx).Err()
}

func (r *RedisRegistry) RegisterOrResume(agentID, proxyID, agentName, version, remoteAddr string) (*AgentRecord, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	now := time.Now().UTC()
	record, err := r.loadForRegister(ctx, agentID, proxyID)
	if err != nil {
		return nil, err
	}
	if record == nil {
		record = &AgentRecord{
			AgentID:      firstNonEmpty(strings.TrimSpace(agentID), "agent_"+randomHex(8)),
			ProxyID:      firstNonEmpty(strings.TrimSpace(proxyID), randomUUIDLike()),
			RegisteredAt: now,
		}
	}

	record.AgentName = firstNonEmpty(strings.TrimSpace(agentName), record.AgentName)
	record.Version = firstNonEmpty(strings.TrimSpace(version), record.Version)
	record.RemoteAddr = firstNonEmpty(strings.TrimSpace(remoteAddr), record.RemoteAddr)
	record.LastSeenAt = now
	if record.RegisteredAt.IsZero() {
		record.RegisteredAt = now
	}

	if err := r.saveRecord(ctx, record); err != nil {
		return nil, err
	}
	if err := r.ensureProxyConfig(ctx, record.ProxyID); err != nil {
		return nil, err
	}
	return cloneRecord(record), nil
}

func (r *RedisRegistry) Touch(agentID, proxyID, version, remoteAddr string) (*AgentRecord, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	record, err := r.loadForTouch(ctx, agentID, proxyID)
	if err != nil {
		return nil, false, err
	}
	if record == nil {
		return nil, false, nil
	}
	if proxyID != "" && record.ProxyID != strings.TrimSpace(proxyID) {
		return nil, false, nil
	}

	record.LastSeenAt = time.Now().UTC()
	if strings.TrimSpace(version) != "" {
		record.Version = strings.TrimSpace(version)
	}
	if strings.TrimSpace(remoteAddr) != "" {
		record.RemoteAddr = strings.TrimSpace(remoteAddr)
	}
	if err := r.saveRecord(ctx, record); err != nil {
		return nil, false, err
	}
	return cloneRecord(record), true, nil
}

func (r *RedisRegistry) GetByProxyID(proxyID string) (*AgentRecord, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	record, err := r.loadByProxyID(ctx, strings.TrimSpace(proxyID))
	if err != nil {
		return nil, false, err
	}
	if record == nil {
		return nil, false, nil
	}
	return cloneRecord(record), true, nil
}

func (r *RedisRegistry) GetProxyConfig(proxyID string) (*ProxyConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return r.loadProxyConfig(ctx, strings.TrimSpace(proxyID))
}

func (r *RedisRegistry) SaveProxyConfig(cfg ProxyConfig) (*ProxyConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	normalized := normalizeProxyConfig(cfg)
	if normalized.ProxyID == "" {
		return nil, fmt.Errorf("proxy_id is required")
	}
	if err := r.saveProxyConfig(ctx, &normalized); err != nil {
		return nil, err
	}
	return cloneProxyConfig(&normalized), nil
}

func (r *RedisRegistry) ListProxyConfigs() ([]ProxyConfig, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	pattern := r.prefix + ":proxycfg:*"
	cursor := uint64(0)
	items := make([]ProxyConfig, 0)
	for {
		keys, next, err := r.client.Scan(ctx, cursor, pattern, 100).Result()
		if err != nil {
			return nil, err
		}
		for _, key := range keys {
			proxyID := strings.TrimPrefix(key, r.prefix+":proxycfg:")
			cfg, err := r.loadProxyConfig(ctx, proxyID)
			if err != nil {
				r.logger.Printf("load proxy config %s failed: %v", proxyID, err)
				continue
			}
			if cfg != nil {
				items = append(items, *cfg)
			}
		}
		cursor = next
		if cursor == 0 {
			break
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

func (r *RedisRegistry) List() ([]contracts.AgentSummary, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	agentIDs, err := r.client.SMembers(ctx, r.keyAgents()).Result()
	if err != nil {
		return nil, err
	}

	now := time.Now().UTC()
	items := make([]contracts.AgentSummary, 0, len(agentIDs))
	for _, agentID := range agentIDs {
		record, err := r.loadByAgentID(ctx, agentID)
		if err != nil {
			r.logger.Printf("load agent %s failed: %v", agentID, err)
			continue
		}
		if record == nil {
			continue
		}
		items = append(items, recordToSummary(record, now))
	}
	sort.Slice(items, func(i, j int) bool {
		return items[i].RegisteredAt.After(items[j].RegisteredAt)
	})
	return items, nil
}

func (r *RedisRegistry) loadForRegister(ctx context.Context, agentID, proxyID string) (*AgentRecord, error) {
	if strings.TrimSpace(agentID) != "" {
		record, err := r.loadByAgentID(ctx, strings.TrimSpace(agentID))
		if err != nil {
			return nil, err
		}
		if record != nil && (strings.TrimSpace(proxyID) == "" || record.ProxyID == strings.TrimSpace(proxyID)) {
			return record, nil
		}
	}
	if strings.TrimSpace(proxyID) != "" {
		return r.loadByProxyID(ctx, strings.TrimSpace(proxyID))
	}
	return nil, nil
}

func (r *RedisRegistry) loadForTouch(ctx context.Context, agentID, proxyID string) (*AgentRecord, error) {
	if strings.TrimSpace(agentID) != "" {
		record, err := r.loadByAgentID(ctx, strings.TrimSpace(agentID))
		if err != nil {
			return nil, err
		}
		if record != nil {
			return record, nil
		}
	}
	if strings.TrimSpace(proxyID) != "" {
		return r.loadByProxyID(ctx, strings.TrimSpace(proxyID))
	}
	return nil, nil
}

func (r *RedisRegistry) loadByProxyID(ctx context.Context, proxyID string) (*AgentRecord, error) {
	agentID, err := r.client.Get(ctx, r.keyProxy(proxyID)).Result()
	if err == redis.Nil {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return r.loadByAgentID(ctx, agentID)
}

func (r *RedisRegistry) loadByAgentID(ctx context.Context, agentID string) (*AgentRecord, error) {
	values, err := r.client.HGetAll(ctx, r.keyAgent(agentID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		return nil, nil
	}
	record := &AgentRecord{
		AgentID:      values["agent_id"],
		ProxyID:      values["proxy_id"],
		AgentName:    values["agent_name"],
		Version:      values["version"],
		RemoteAddr:   values["remote_addr"],
		RegisteredAt: parseTime(values["registered_at"]),
		LastSeenAt:   parseTime(values["last_seen_at"]),
	}
	if record.AgentID == "" || record.ProxyID == "" {
		return nil, nil
	}
	return record, nil
}

func (r *RedisRegistry) saveRecord(ctx context.Context, record *AgentRecord) error {
	if record == nil {
		return nil
	}
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, r.keyAgent(record.AgentID), map[string]any{
		"agent_id":      record.AgentID,
		"proxy_id":      record.ProxyID,
		"agent_name":    record.AgentName,
		"version":       record.Version,
		"remote_addr":   record.RemoteAddr,
		"registered_at": record.RegisteredAt.UTC().Format(time.RFC3339Nano),
		"last_seen_at":  record.LastSeenAt.UTC().Format(time.RFC3339Nano),
	})
	pipe.Set(ctx, r.keyProxy(record.ProxyID), record.AgentID, 0)
	pipe.SAdd(ctx, r.keyAgents(), record.AgentID)
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisRegistry) ensureProxyConfig(ctx context.Context, proxyID string) error {
	cfg, err := r.loadProxyConfig(ctx, proxyID)
	if err != nil {
		return err
	}
	if cfg != nil {
		return nil
	}
	defaultCfg := defaultProxyConfig(proxyID)
	return r.saveProxyConfig(ctx, &defaultCfg)
}

func (r *RedisRegistry) loadProxyConfig(ctx context.Context, proxyID string) (*ProxyConfig, error) {
	proxyID = strings.TrimSpace(proxyID)
	if proxyID == "" {
		return nil, nil
	}
	values, err := r.client.HGetAll(ctx, r.keyProxyConfig(proxyID)).Result()
	if err != nil {
		return nil, err
	}
	if len(values) == 0 {
		defaultCfg := defaultProxyConfig(proxyID)
		return &defaultCfg, nil
	}
	cfg := ProxyConfig{
		ProxyID:   firstNonEmpty(values["proxy_id"], proxyID),
		Remark:    strings.TrimSpace(values["remark"]),
		Enabled:   parseBoolDefault(values["enabled"], true),
		Permanent: parseBoolDefault(values["permanent"], true),
		ExpiresAt: parseTime(values["expires_at"]),
		UpdatedAt: parseTime(values["updated_at"]),
	}
	normalized := normalizeProxyConfig(cfg)
	return &normalized, nil
}

func (r *RedisRegistry) saveProxyConfig(ctx context.Context, cfg *ProxyConfig) error {
	if cfg == nil {
		return nil
	}
	normalized := normalizeProxyConfig(*cfg)
	pipe := r.client.TxPipeline()
	pipe.HSet(ctx, r.keyProxyConfig(normalized.ProxyID), map[string]any{
		"proxy_id":   normalized.ProxyID,
		"remark":     normalized.Remark,
		"enabled":    boolString(normalized.Enabled),
		"permanent":  boolString(normalized.Permanent),
		"expires_at": formatTime(normalized.ExpiresAt),
		"updated_at": normalized.UpdatedAt.UTC().Format(time.RFC3339Nano),
	})
	_, err := pipe.Exec(ctx)
	return err
}

func (r *RedisRegistry) keyAgents() string {
	return r.prefix + ":agents"
}

func (r *RedisRegistry) keyAgent(agentID string) string {
	return r.prefix + ":agent:" + agentID
}

func (r *RedisRegistry) keyProxy(proxyID string) string {
	return r.prefix + ":proxy:" + proxyID
}

func (r *RedisRegistry) keyProxyConfig(proxyID string) string {
	return r.prefix + ":proxycfg:" + proxyID
}

func parseTime(raw string) time.Time {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}
	}
	if parsed, err := time.Parse(time.RFC3339Nano, raw); err == nil {
		return parsed
	}
	return time.Time{}
}

func formatTime(value time.Time) string {
	if value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339Nano)
}

func boolString(value bool) string {
	if value {
		return "1"
	}
	return "0"
}

func parseBoolDefault(raw string, fallback bool) bool {
	raw = strings.TrimSpace(strings.ToLower(raw))
	if raw == "" {
		return fallback
	}
	switch raw {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	}
	number, err := strconv.Atoi(raw)
	if err == nil {
		return number != 0
	}
	return fallback
}
