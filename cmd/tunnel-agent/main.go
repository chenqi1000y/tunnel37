package main

import (
	"encoding/json"
	"flag"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"

	"polyquant/demo-go-tunnel/internal/contracts"
)

const agentVersion = "0.1.0"

func main() {
	tunnelAddr := flag.String("tunnel", "127.0.0.1:9081", "隧道 TCP 服务地址")
	agentName := flag.String("name", hostnameOr("windows-agent"), "agent 名称")
	token := flag.String("token", "demo-secret", "共享鉴权 token")
	upstream := relayUpstreamFlag()
	flag.Parse()

	logger := log.New(os.Stdout, "[tunnel-agent] ", log.LstdFlags)
	if strings.TrimSpace(*upstream) != "" {
		logger.Printf("当前版本暂未接入上游 SOCKS5，先忽略参数 upstream=%s", *upstream)
	}
	runAgentApp(*tunnelAddr, *agentName, *token, logger)
}

func runTunnelClient(tunnelAddr, agentName, token string, logger *log.Logger) error {
	conn, err := net.DialTimeout("tcp", tunnelAddr, 10*time.Second)
	if err != nil {
		return err
	}
	defer conn.Close()

	var (
		writeMu sync.Mutex
		state   = contracts.ControlMessage{}
	)
	loaded := loadAgentState(agentName, logger)
	state.AgentID = loaded.AgentID
	state.ProxyID = loaded.ProxyID

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
	saveAgentState(&agentState{AgentID: state.AgentID, ProxyID: state.ProxyID}, agentName, logger)
	logger.Printf("长连接注册成功，agent_id=%s proxy_id=%s", state.AgentID, state.ProxyID)

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
			case contracts.ControlTypeError:
				readErrCh <- errUnexpectedAck(msg)
				return
			}
		}
	}()

	ticker := time.NewTicker(HeartbeatInterval())
	defer ticker.Stop()

	for {
		select {
		case err := <-readErrCh:
			return err
		case <-ticker.C:
			if err := send(contracts.ControlMessage{
				Type:      contracts.ControlTypePing,
				AgentID:   state.AgentID,
				ProxyID:   state.ProxyID,
				Version:   agentVersion,
				Timestamp: time.Now(),
			}); err != nil {
				return err
			}
		}
	}
}

func HeartbeatInterval() time.Duration {
	return 15 * time.Second
}

func errUnexpectedAck(msg contracts.ControlMessage) error {
	if strings.TrimSpace(msg.Message) != "" {
		return &protocolError{message: msg.Message}
	}
	return &protocolError{message: "unexpected control response: " + msg.Type}
}

func hostnameOr(fallback string) string {
	name, err := os.Hostname()
	if err != nil || strings.TrimSpace(name) == "" {
		return fallback
	}
	return name
}

type protocolError struct {
	message string
}

func (e *protocolError) Error() string {
	return e.message
}
