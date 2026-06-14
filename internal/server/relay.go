package server

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

type RelayServer struct {
	addr        string
	store       AgentStore
	sharedToken string
	logger      *log.Logger

	mu            sync.RWMutex
	listener      net.Listener
	activeByAgent map[string]*RelaySession
	activeByProxy map[string]*RelaySession
}

type RelaySession struct {
	agentID string
	proxyID string
	conn    net.Conn
	encoder *json.Encoder
	logger  *log.Logger

	sendMu  sync.Mutex
	stateMu sync.RWMutex
	streams map[string]*RelayStream
}

type RelayStream struct {
	ID        string
	OpenCh    chan error
	DataCh    chan []byte
	DoneCh    chan string
	closeOnce sync.Once
}

type RelayStats struct {
	ActiveAgents  int `json:"active_agents"`
	ActiveProxies int `json:"active_proxies"`
	ActiveStreams int `json:"active_streams"`
}

func NewRelayServer(addr string, store AgentStore, sharedToken string, logger *log.Logger) *RelayServer {
	if logger == nil {
		logger = log.Default()
	}
	if store == nil {
		store = NewRegistry()
	}
	return &RelayServer{
		addr:          addr,
		store:         store,
		sharedToken:   sharedToken,
		logger:        logger,
		activeByAgent: make(map[string]*RelaySession),
		activeByProxy: make(map[string]*RelaySession),
	}
}

func (s *RelayServer) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.addr)
	if err != nil {
		return err
	}
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		if s.listener == ln {
			s.listener = nil
		}
		s.mu.Unlock()
	}()
	s.logger.Printf("relay tcp listening on %s", s.addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Printf("relay accept failed: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *RelayServer) Shutdown(_ context.Context) error {
	s.mu.Lock()
	ln := s.listener
	sessions := make([]*RelaySession, 0, len(s.activeByAgent))
	seen := make(map[*RelaySession]struct{}, len(s.activeByAgent))
	for _, session := range s.activeByAgent {
		if _, ok := seen[session]; ok {
			continue
		}
		seen[session] = struct{}{}
		sessions = append(sessions, session)
	}
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, session := range sessions {
		session.Close("server shutting down")
	}
	return nil
}

func (s *RelayServer) GetSessionByProxyID(proxyID string) (*RelaySession, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	session, ok := s.activeByProxy[proxyID]
	return session, ok
}

func (s *RelayServer) Stats() RelayStats {
	s.mu.RLock()
	sessions := make([]*RelaySession, 0, len(s.activeByAgent))
	for _, session := range s.activeByAgent {
		sessions = append(sessions, session)
	}
	stats := RelayStats{
		ActiveAgents:  len(s.activeByAgent),
		ActiveProxies: len(s.activeByProxy),
	}
	s.mu.RUnlock()

	for _, session := range sessions {
		stats.ActiveStreams += session.StreamCount()
	}
	return stats
}

func (s *RelayServer) handleConn(conn net.Conn) {
	defer conn.Close()

	remoteAddr := conn.RemoteAddr().String()
	decoder := json.NewDecoder(conn)
	encoder := json.NewEncoder(conn)

	var hello contracts.ControlMessage
	if err := decoder.Decode(&hello); err != nil {
		s.logger.Printf("read relay register failed %s: %v", remoteAddr, err)
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

	record, err := s.store.RegisterOrResume(
		strings.TrimSpace(hello.AgentID),
		strings.TrimSpace(hello.ProxyID),
		strings.TrimSpace(hello.AgentName),
		strings.TrimSpace(hello.Version),
		remoteAddr,
	)
	if err != nil {
		_ = encoder.Encode(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: err.Error()})
		return
	}

	session := &RelaySession{agentID: record.AgentID, proxyID: record.ProxyID, conn: conn, encoder: encoder, logger: s.logger, streams: make(map[string]*RelayStream)}
	s.setActive(session)
	defer s.removeActive(session)

	if err := session.Send(contracts.ControlMessage{Type: contracts.ControlTypeRegisterOK, AgentID: record.AgentID, ProxyID: record.ProxyID, AgentName: record.AgentName, Version: record.Version, Message: "registered", Timestamp: time.Now()}); err != nil {
		s.logger.Printf("send relay register ack failed %s: %v", remoteAddr, err)
		return
	}

	s.logger.Printf("relay agent online agent_id=%s proxy_id=%s remote=%s", record.AgentID, record.ProxyID, remoteAddr)

	for {
		var msg contracts.ControlMessage
		if err := decoder.Decode(&msg); err != nil {
			if !errors.Is(err, io.EOF) {
				s.logger.Printf("read relay message failed agent_id=%s: %v", record.AgentID, err)
			}
			return
		}

		switch msg.Type {
		case contracts.ControlTypePing:
			record, ok, err := s.store.Touch(record.AgentID, record.ProxyID, strings.TrimSpace(msg.Version), remoteAddr)
			if err != nil {
				_ = session.Send(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: err.Error()})
				return
			}
			if !ok {
				_ = session.Send(contracts.ControlMessage{Type: contracts.ControlTypeError, Message: "agent not found"})
				return
			}
			if err := session.Send(contracts.ControlMessage{Type: contracts.ControlTypePong, AgentID: record.AgentID, ProxyID: record.ProxyID, Timestamp: time.Now()}); err != nil {
				return
			}
		case contracts.ControlTypeStreamOK:
			session.ResolveOpen(msg.StreamID, msg.OK, msg.Message)
		case contracts.ControlTypeStreamData:
			payload, err := base64.StdEncoding.DecodeString(msg.Payload)
			if err != nil {
				session.CloseStreamWithReason(msg.StreamID, "invalid payload")
				continue
			}
			session.PushData(msg.StreamID, payload)
		case contracts.ControlTypeStreamClose:
			session.CloseStreamWithReason(msg.StreamID, msg.Message)
		case contracts.ControlTypeError:
			s.logger.Printf("agent error agent_id=%s message=%s", record.AgentID, msg.Message)
		}
	}
}

func (s *RelayServer) setActive(session *RelaySession) {
	s.mu.Lock()
	var oldSessions []*RelaySession
	if old := s.activeByAgent[session.agentID]; old != nil && old != session {
		oldSessions = append(oldSessions, old)
	}
	if old := s.activeByProxy[session.proxyID]; old != nil && old != session {
		oldSessions = append(oldSessions, old)
	}
	s.activeByAgent[session.agentID] = session
	s.activeByProxy[session.proxyID] = session
	s.mu.Unlock()

	for _, old := range oldSessions {
		old.Close("replaced by new session")
	}
}

func (s *RelayServer) removeActive(session *RelaySession) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if current, ok := s.activeByAgent[session.agentID]; ok && current == session {
		delete(s.activeByAgent, session.agentID)
	}
	if current, ok := s.activeByProxy[session.proxyID]; ok && current == session {
		delete(s.activeByProxy, session.proxyID)
	}
	session.CloseAllStreams("agent disconnected")
}

func (s *RelayServer) allowToken(got string) bool {
	if s.sharedToken == "" {
		return true
	}
	return strings.TrimSpace(got) == s.sharedToken
}

func (s *RelaySession) Send(msg contracts.ControlMessage) error {
	s.sendMu.Lock()
	defer s.sendMu.Unlock()
	return s.encoder.Encode(msg)
}

func (s *RelaySession) Close(reason string) {
	s.CloseAllStreams(reason)
	if s.conn != nil {
		_ = s.conn.Close()
	}
}

func (s *RelaySession) OpenStream(targetHost string, targetPort int) (*RelayStream, error) {
	streamID := randomHex(8)
	stream := &RelayStream{ID: streamID, OpenCh: make(chan error, 1), DataCh: make(chan []byte, 32), DoneCh: make(chan string, 1)}

	s.stateMu.Lock()
	s.streams[streamID] = stream
	s.stateMu.Unlock()

	if err := s.Send(contracts.ControlMessage{Type: contracts.ControlTypeStreamOpen, AgentID: s.agentID, ProxyID: s.proxyID, StreamID: streamID, TargetHost: targetHost, TargetPort: targetPort, Timestamp: time.Now()}); err != nil {
		s.removeStream(streamID)
		return nil, err
	}

	select {
	case err := <-stream.OpenCh:
		if err != nil {
			s.removeStream(streamID)
			return nil, err
		}
		return stream, nil
	case <-time.After(10 * time.Second):
		s.removeStream(streamID)
		return nil, fmt.Errorf("stream open timeout")
	}
}

func (s *RelaySession) SendStreamData(streamID string, payload []byte) error {
	return s.Send(contracts.ControlMessage{Type: contracts.ControlTypeStreamData, AgentID: s.agentID, ProxyID: s.proxyID, StreamID: streamID, Payload: base64.StdEncoding.EncodeToString(payload), Timestamp: time.Now()})
}

func (s *RelaySession) CloseStream(streamID, reason string) error {
	s.CloseStreamWithReason(streamID, reason)
	return s.Send(contracts.ControlMessage{Type: contracts.ControlTypeStreamClose, AgentID: s.agentID, ProxyID: s.proxyID, StreamID: streamID, Message: reason, Timestamp: time.Now()})
}

func (s *RelaySession) ResolveOpen(streamID string, ok bool, message string) {
	s.stateMu.RLock()
	stream := s.streams[streamID]
	s.stateMu.RUnlock()
	if stream == nil {
		return
	}
	if ok {
		stream.OpenCh <- nil
		return
	}
	if strings.TrimSpace(message) == "" {
		message = "stream open failed"
	}
	stream.OpenCh <- fmt.Errorf("%s", message)
}

func (s *RelaySession) PushData(streamID string, payload []byte) {
	s.stateMu.RLock()
	stream := s.streams[streamID]
	s.stateMu.RUnlock()
	if stream == nil {
		return
	}
	select {
	case stream.DataCh <- payload:
	case <-time.After(5 * time.Second):
		s.CloseStreamWithReason(streamID, "server stream blocked")
	}
}

func (s *RelaySession) CloseStreamWithReason(streamID, reason string) {
	s.stateMu.RLock()
	stream := s.streams[streamID]
	s.stateMu.RUnlock()
	if stream == nil {
		return
	}

	stream.closeOnce.Do(func() {
		if strings.TrimSpace(reason) == "" {
			reason = "closed"
		}
		stream.DoneCh <- reason
		close(stream.DoneCh)
		close(stream.DataCh)
		s.removeStream(streamID)
	})
}

func (s *RelaySession) CloseAllStreams(reason string) {
	s.stateMu.RLock()
	ids := make([]string, 0, len(s.streams))
	for id := range s.streams {
		ids = append(ids, id)
	}
	s.stateMu.RUnlock()
	for _, id := range ids {
		s.CloseStreamWithReason(id, reason)
	}
}

func (s *RelaySession) removeStream(streamID string) {
	s.stateMu.Lock()
	defer s.stateMu.Unlock()
	delete(s.streams, streamID)
}

func (s *RelaySession) StreamCount() int {
	s.stateMu.RLock()
	defer s.stateMu.RUnlock()
	return len(s.streams)
}
