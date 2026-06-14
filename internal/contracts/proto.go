package contracts

import "time"

const (
	ControlTypeRegister    = "register"
	ControlTypeRegisterOK  = "register_ok"
	ControlTypePing        = "ping"
	ControlTypePong        = "pong"
	ControlTypeStreamOpen  = "stream_open"
	ControlTypeStreamOK    = "stream_ok"
	ControlTypeStreamData  = "stream_data"
	ControlTypeStreamClose = "stream_close"
	ControlTypeError       = "error"
)

type ControlMessage struct {
	Type       string    `json:"type"`
	AgentID    string    `json:"agent_id,omitempty"`
	ProxyID    string    `json:"proxy_id,omitempty"`
	AgentName  string    `json:"agent_name,omitempty"`
	Version    string    `json:"version,omitempty"`
	AuthToken  string    `json:"auth_token,omitempty"`
	Message    string    `json:"message,omitempty"`
	StreamID   string    `json:"stream_id,omitempty"`
	TargetHost string    `json:"target_host,omitempty"`
	TargetPort int       `json:"target_port,omitempty"`
	Payload    string    `json:"payload,omitempty"`
	OK         bool      `json:"ok,omitempty"`
	Timestamp  time.Time `json:"timestamp,omitempty"`
}
