package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

type TunnelServer struct {
	addr        string
	store       AgentStore
	sharedToken string
	logger      *log.Logger

	mu     sync.RWMutex
	active map[string]net.Conn
}

func NewTunnelServer(addr string, store AgentStore, sharedToken string, logger *log.Logger) *TunnelServer {
	if logger == nil {
		logger = log.Default()
	}
	if store == nil {
		store = NewRegistry()
	}
	return &TunnelServer{addr: addr, store: store, sharedToken: sharedToken, logger: logger, active: make(map[string]net.Conn)}
}

func (s *TunnelServer) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.logger.Printf("tunnel tcp listening on %s", s.addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			s.logger.Printf("accept failed: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *TunnelServer) handleConn(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var hello contracts.ControlMessage
	if err := decoder.Decode(&hello); err != nil {
		s.logger.Printf("read register failed %s: %v", remoteAddr, err)
		return
	}
	if hello.Type != contracts.ControlTypeRegister {
		_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "first message must be register"})
		return
	}
	if strings.TrimSpace(hello.AgentName) == "" {
		_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "agent_name is required"})
		return
	}
	if !s.allowToken(hello.AuthToken) {
		_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "invalid auth token"})
		return
	}

	record, err := s.store.RegisterOrResume(strings.TrimSpace(hello.AgentID), strings.TrimSpace(hello.ProxyID), strings.TrimSpace(hello.AgentName), strings.TrimSpace(hello.Version), remoteAddr)
	if err != nil {
		_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: err.Error()})
		return
	}
	s.setActive(record.AgentID, conn)
	defer s.removeActive(record.AgentID, conn)

	if err := encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeRegisterOK, AgentID: record.AgentID, ProxyID: record.ProxyID, AgentName: record.AgentName, Version: record.Version, Timestamp: time.Now(), Message: "registered"}); err != nil {
		s.logger.Printf("send register ack failed %s: %v", remoteAddr, err)
		return
	}

	s.logger.Printf("agent connected agent_id=%s proxy_id=%s remote=%s", record.AgentID, record.ProxyID, remoteAddr)

	for {
		var msg contracts.ControlMessage
		if err := decoder.Decode(&msg); err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Printf("read agent message failed agent_id=%s: %v", record.AgentID, err)
			}
			return
		}
		switch msg.Type {
		case contracts.ControlTypePing:
			record, ok, err := s.store.Touch(record.AgentID, record.ProxyID, strings.TrimSpace(msg.Version), remoteAddr)
			if err != nil {
				_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: err.Error()})
				return
			}
			if !ok {
				_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "agent not found"})
				return
			}
			if err := encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypePong, AgentID: record.AgentID, ProxyID: record.ProxyID, Timestamp: time.Now()}); err != nil {
				return
			}
		default:
			if err := encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "unsupported control message"}); err != nil {
				return
			}
		}
	}
}

func (s *TunnelServer) setActive(agentID string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.active[agentID] = conn
}

func (s *TunnelServer) removeActive(agentID string, conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	current, ok := s.active[agentID]
	if ok && current == conn {
		delete(s.active, agentID)
	}
}

func (s *TunnelServer) allowToken(got string) bool {
	if s.sharedToken == "" {
		return true
	}
	return strings.TrimSpace(got) == s.sharedToken
}
