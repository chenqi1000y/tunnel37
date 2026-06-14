package contracts

import "time"

type RegisterAgentRequest struct {
	AgentID   string `json:"agent_id,omitempty"`
	ProxyID   string `json:"proxy_id,omitempty"`
	AgentName string `json:"agent_name"`
	Version   string `json:"version"`
	AuthToken string `json:"auth_token"`
}

type RegisterAgentResponse struct {
	AgentID              string    `json:"agent_id"`
	ProxyID              string    `json:"proxy_id"`
	HeartbeatIntervalSec int       `json:"heartbeat_interval_sec"`
	ServerTime           time.Time `json:"server_time"`
}

type HeartbeatRequest struct {
	AgentID string `json:"agent_id"`
	ProxyID string `json:"proxy_id"`
	Version string `json:"version"`
}

type HeartbeatResponse struct {
	OK         bool      `json:"ok"`
	ServerTime time.Time `json:"server_time"`
}

type AgentSummary struct {
	AgentID      string    `json:"agent_id"`
	ProxyID      string    `json:"proxy_id"`
	AgentName    string    `json:"agent_name"`
	Version      string    `json:"version"`
	RemoteAddr   string    `json:"remote_addr"`
	LastSeenAt   time.Time `json:"last_seen_at"`
	RegisteredAt time.Time `json:"registered_at"`
	Status       string    `json:"status"`
}
