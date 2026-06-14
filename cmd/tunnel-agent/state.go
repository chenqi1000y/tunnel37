package main

import (
	"encoding/json"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

type persistedAgentState struct {
	AgentID   string    `json:"agent_id,omitempty"`
	ProxyID   string    `json:"proxy_id,omitempty"`
	AgentName string    `json:"agent_name,omitempty"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

func loadAgentState(agentName string, logger *log.Logger) *agentState {
	path := stateFilePath()
	raw, err := os.ReadFile(path)
	if err != nil {
		if !os.IsNotExist(err) && logger != nil {
			logger.Printf("读取本地状态失败: %v", err)
		}
		return &agentState{}
	}
	var saved persistedAgentState
	if err := json.Unmarshal(raw, &saved); err != nil {
		if logger != nil {
			logger.Printf("解析本地状态失败: %v", err)
		}
		return &agentState{}
	}
	state := &agentState{
		AgentID: strings.TrimSpace(saved.AgentID),
		ProxyID: strings.TrimSpace(saved.ProxyID),
	}
	if logger != nil && state.ProxyID != "" {
		logger.Printf("读取到历史代理ID，启动时优先尝试复用 proxy_id=%s", state.ProxyID)
	}
	return state
}

func saveAgentState(state *agentState, agentName string, logger *log.Logger) {
	if state == nil {
		return
	}
	path := stateFilePath()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		if logger != nil {
			logger.Printf("创建状态目录失败: %v", err)
		}
		return
	}
	payload := persistedAgentState{
		AgentID:   strings.TrimSpace(state.AgentID),
		ProxyID:   strings.TrimSpace(state.ProxyID),
		AgentName: strings.TrimSpace(agentName),
		UpdatedAt: time.Now(),
	}
	raw, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		if logger != nil {
			logger.Printf("序列化状态失败: %v", err)
		}
		return
	}
	if err := os.WriteFile(path, append(raw, '\n'), 0o644); err != nil && logger != nil {
		logger.Printf("写入状态文件失败: %v", err)
	}
}

func stateFilePath() string {
	exePath, err := os.Executable()
	if err != nil || strings.TrimSpace(exePath) == "" {
		return "tunnel-agent.state.json"
	}
	return filepath.Join(filepath.Dir(exePath), "tunnel-agent.state.json")
}
