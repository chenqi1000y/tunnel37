package main

import (
	"encoding/base64"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"sync"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

type agentState struct {
	AgentID string
	ProxyID string
}

type relayStreamConn struct {
	id   string
	conn net.Conn
}

func runAgentApp(tunnelAddr, agentName, token string, logger *log.Logger) {
	state := loadAgentState(agentName, logger)
	for {
		if err := runRelayClient(tunnelAddr, agentName, token, state, logger); err != nil {
			logger.Printf("relay 连接中断: %v", err)
		}
		time.Sleep(3 * time.Second)
	}
}

func relayUpstreamFlag() *string {
	return flag.String("upstream", "", "可选，上游 SOCKS5 地址，当前版本暂未接入")
}

func runRelayClient(tunnelAddr, agentName, token string, state *agentState, logger *log.Logger) error {
	conn, err := net.DialTimeout("tcp", tunnelAddr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	var (
		writeMu  sync.Mutex
		streamMu sync.RWMutex
		streams  = make(map[string]*relayStreamConn)
	)

	encoder := json.NewEncoder(conn)
	decoder := json.NewDecoder(conn)

	send := func(msg contracts.ControlMessage) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		return encoder.Encode(msg)
	}

	if err := send(contracts.ControlMessage{
		Type:      contracts.ControlTypeRegister,
		AgentID:   state.AgentID,
		ProxyID:   state.ProxyID,
		AgentName: agentName,
		Version:   agentVersion,
		AuthToken: token,
		Timestamp: time.Now(),
	}); err != nil {
		return err
	}

	var ack contracts.ControlMessage
	if err := decoder.Decode(&ack); err != nil {
		return err
	}
	if ack.Type != contracts.ControlTypeRegisterOK {
		return errUnexpectedAck(ack)
	}
	state.AgentID = ack.AgentID
	state.ProxyID = ack.ProxyID
	saveAgentState(state, agentName, logger)
	logger.Printf("relay 注册成功，agent_id=%s proxy_id=%s", state.AgentID, state.ProxyID)
	logger.Printf("云端代理地址：socks5://%s:%s@服务器公网IP:21080", state.ProxyID, token)

	ticker := time.NewTicker(HeartbeatInterval())
	defer ticker.Stop()

	readErrCh := make(chan error, 1)
	go func() {
		for {
			var msg contracts.ControlMessage
			if err := decoder.Decode(&msg); err != nil {
				readErrCh <- err
				return
			}

			switch msg.Type {
			case contracts.ControlTypePong:
				logger.Printf("收到 pong，代理在线 proxy_id=%s", state.ProxyID)
			case contracts.ControlTypeStreamOpen:
				targetConn, err := net.DialTimeout("tcp", net.JoinHostPort(msg.TargetHost, fmt.Sprintf("%d", msg.TargetPort)), 15*time.Second)
				if err != nil {
					_ = send(contracts.ControlMessage{
						Type:      contracts.ControlTypeStreamOK,
						AgentID:   state.AgentID,
						ProxyID:   state.ProxyID,
						StreamID:  msg.StreamID,
						OK:        false,
						Message:   err.Error(),
						Timestamp: time.Now(),
					})
					continue
				}

				stream := &relayStreamConn{id: msg.StreamID, conn: targetConn}
				streamMu.Lock()
				streams[msg.StreamID] = stream
				streamMu.Unlock()

				if err := send(contracts.ControlMessage{
					Type:      contracts.ControlTypeStreamOK,
					AgentID:   state.AgentID,
					ProxyID:   state.ProxyID,
					StreamID:  msg.StreamID,
					OK:        true,
					Timestamp: time.Now(),
				}); err != nil {
					targetConn.Close()
					readErrCh <- err
					return
				}

				go pumpLocalToRelay(stream, state, send, logger, func() {
					streamMu.Lock()
					delete(streams, stream.id)
					streamMu.Unlock()
				})
			case contracts.ControlTypeStreamData:
				payload, err := base64.StdEncoding.DecodeString(msg.Payload)
				if err != nil {
					continue
				}
				streamMu.RLock()
				stream := streams[msg.StreamID]
				streamMu.RUnlock()
				if stream == nil {
					continue
				}
				if _, err := stream.conn.Write(payload); err != nil {
					stream.conn.Close()
				}
			case contracts.ControlTypeStreamClose:
				streamMu.Lock()
				stream := streams[msg.StreamID]
				if stream != nil {
					delete(streams, msg.StreamID)
				}
				streamMu.Unlock()
				if stream != nil {
					_ = stream.conn.Close()
				}
			case contracts.ControlTypeError:
				readErrCh <- errUnexpectedAck(msg)
				return
			}
		}
	}()

	for {
		select {
		case err := <-readErrCh:
			closeRelayStreams(streams, &streamMu)
			return err
		case <-ticker.C:
			if err := send(contracts.ControlMessage{
				Type:      contracts.ControlTypePing,
				AgentID:   state.AgentID,
				ProxyID:   state.ProxyID,
				Version:   agentVersion,
				Timestamp: time.Now(),
			}); err != nil {
				closeRelayStreams(streams, &streamMu)
				return err
			}
		}
	}
}

func pumpLocalToRelay(stream *relayStreamConn, state *agentState, send func(contracts.ControlMessage) error, logger *log.Logger, cleanup func()) {
	defer cleanup()
	defer stream.conn.Close()

	buf := make([]byte, 32*1024)
	for {
		n, err := stream.conn.Read(buf)
		if n > 0 {
			if sendErr := send(contracts.ControlMessage{
				Type:      contracts.ControlTypeStreamData,
				AgentID:   state.AgentID,
				ProxyID:   state.ProxyID,
				StreamID:  stream.id,
				Payload:   base64.StdEncoding.EncodeToString(buf[:n]),
				Timestamp: time.Now(),
			}); sendErr != nil {
				logger.Printf("发送 stream_data 失败 stream_id=%s: %v", stream.id, sendErr)
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				logger.Printf("读取本地连接失败 stream_id=%s: %v", stream.id, err)
			}
			_ = send(contracts.ControlMessage{
				Type:      contracts.ControlTypeStreamClose,
				AgentID:   state.AgentID,
				ProxyID:   state.ProxyID,
				StreamID:  stream.id,
				Message:   "local connection closed",
				Timestamp: time.Now(),
			})
			return
		}
	}
}

func closeRelayStreams(streams map[string]*relayStreamConn, mu *sync.RWMutex) {
	mu.Lock()
	defer mu.Unlock()
	for id, stream := range streams {
		_ = stream.conn.Close()
		delete(streams, id)
	}
}
