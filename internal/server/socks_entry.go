package server

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"
)

type SocksEntryServer struct {
	addr        string
	relay       *RelayServer
	store       AgentStore
	sharedToken string
	logger      *log.Logger

	mu       sync.RWMutex
	listener net.Listener
	conns    map[net.Conn]struct{}
}

type SocksStats struct {
	ActiveConnections int `json:"active_connections"`
}

func NewSocksEntryServer(addr string, relay *RelayServer, store AgentStore, sharedToken string, logger *log.Logger) *SocksEntryServer {
	if logger == nil {
		logger = log.Default()
	}
	return &SocksEntryServer{
		addr:        addr,
		relay:       relay,
		store:       store,
		sharedToken: sharedToken,
		logger:      logger,
		conns:       make(map[net.Conn]struct{}),
	}
}

func (s *SocksEntryServer) ListenAndServe() error {
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
	s.logger.Printf("socks5 entry listening on %s", s.addr)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logger.Printf("socks accept failed: %v", err)
			continue
		}
		go s.handleConn(conn)
	}
}

func (s *SocksEntryServer) Shutdown(_ context.Context) error {
	s.mu.Lock()
	ln := s.listener
	conns := make([]net.Conn, 0, len(s.conns))
	for conn := range s.conns {
		conns = append(conns, conn)
	}
	s.mu.Unlock()

	if ln != nil {
		_ = ln.Close()
	}
	for _, conn := range conns {
		_ = conn.Close()
	}
	return nil
}

func (s *SocksEntryServer) Stats() SocksStats {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return SocksStats{ActiveConnections: len(s.conns)}
}

func (s *SocksEntryServer) handleConn(conn net.Conn) {
	s.trackConn(conn)
	defer s.untrackConn(conn)
	defer conn.Close()

	proxyID, err := s.handshake(conn)
	if err != nil {
		s.logger.Printf("socks handshake failed: %v", err)
		return
	}

	targetHost, targetPort, err := s.readConnectRequest(conn)
	if err != nil {
		s.logger.Printf("socks connect request failed proxy_id=%s: %v", proxyID, err)
		return
	}

	session, ok := s.relay.GetSessionByProxyID(proxyID)
	if !ok {
		_ = writeSocksReply(conn, 0x04)
		return
	}

	stream, err := session.OpenStream(targetHost, targetPort)
	if err != nil {
		s.logger.Printf("open relay stream failed proxy_id=%s target=%s:%d err=%v", proxyID, targetHost, targetPort, err)
		_ = writeSocksReply(conn, 0x05)
		return
	}
	defer session.CloseStream(stream.ID, "client closed")

	if err := writeSocksReply(conn, 0x00); err != nil {
		return
	}

	errCh := make(chan error, 2)
	go func() {
		buf := make([]byte, 32*1024)
		for {
			n, err := conn.Read(buf)
			if n > 0 {
				if sendErr := session.SendStreamData(stream.ID, buf[:n]); sendErr != nil {
					errCh <- sendErr
					return
				}
			}
			if err != nil {
				if err == io.EOF {
					errCh <- nil
					return
				}
				errCh <- err
				return
			}
		}
	}()

	go func() {
		for {
			select {
			case payload, ok := <-stream.DataCh:
				if !ok {
					errCh <- nil
					return
				}
				if _, err := conn.Write(payload); err != nil {
					errCh <- err
					return
				}
			case reason, ok := <-stream.DoneCh:
				if ok && strings.TrimSpace(reason) != "" {
					errCh <- fmt.Errorf("%s", reason)
				} else {
					errCh <- nil
				}
				return
			}
		}
	}()

	<-errCh
}

func (s *SocksEntryServer) trackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.conns[conn] = struct{}{}
}

func (s *SocksEntryServer) untrackConn(conn net.Conn) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.conns, conn)
}

func (s *SocksEntryServer) handshake(conn net.Conn) (string, error) {
	header := make([]byte, 2)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", err
	}
	if header[0] != 0x05 {
		return "", fmt.Errorf("unsupported socks version: %d", header[0])
	}

	methods := make([]byte, int(header[1]))
	if _, err := io.ReadFull(conn, methods); err != nil {
		return "", err
	}

	found := false
	for _, method := range methods {
		if method == 0x02 {
			found = true
			break
		}
	}
	if !found {
		_, _ = conn.Write([]byte{0x05, 0xff})
		return "", fmt.Errorf("username/password auth required")
	}
	if _, err := conn.Write([]byte{0x05, 0x02}); err != nil {
		return "", err
	}

	authHead := make([]byte, 2)
	if _, err := io.ReadFull(conn, authHead); err != nil {
		return "", err
	}
	userLen := int(authHead[1])
	user := make([]byte, userLen)
	if _, err := io.ReadFull(conn, user); err != nil {
		return "", err
	}
	var passLen [1]byte
	if _, err := io.ReadFull(conn, passLen[:]); err != nil {
		return "", err
	}
	pass := make([]byte, int(passLen[0]))
	if _, err := io.ReadFull(conn, pass); err != nil {
		return "", err
	}

	proxyID := string(user)
	password := string(pass)
	if s.sharedToken != "" && password != s.sharedToken {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return "", fmt.Errorf("invalid password")
	}
	if s.store != nil {
		cfg, err := s.store.GetProxyConfig(proxyID)
		if err != nil {
			_, _ = conn.Write([]byte{0x01, 0x01})
			return "", fmt.Errorf("load proxy config failed: %w", err)
		}
		if cfg != nil {
			if !cfg.Enabled {
				_, _ = conn.Write([]byte{0x01, 0x01})
				return "", fmt.Errorf("proxy disabled")
			}
			if cfg.IsExpired(time.Now().UTC()) {
				_, _ = conn.Write([]byte{0x01, 0x01})
				return "", fmt.Errorf("proxy expired")
			}
		}
	}
	if _, ok := s.relay.GetSessionByProxyID(proxyID); !ok {
		_, _ = conn.Write([]byte{0x01, 0x01})
		return "", fmt.Errorf("proxy_id not online")
	}

	if _, err := conn.Write([]byte{0x01, 0x00}); err != nil {
		return "", err
	}
	return proxyID, nil
}

func (s *SocksEntryServer) readConnectRequest(conn net.Conn) (string, int, error) {
	header := make([]byte, 4)
	if _, err := io.ReadFull(conn, header); err != nil {
		return "", 0, err
	}
	if header[0] != 0x05 {
		return "", 0, fmt.Errorf("unsupported request version: %d", header[0])
	}
	if header[1] != 0x01 {
		return "", 0, fmt.Errorf("only CONNECT is supported")
	}

	var host string
	switch header[3] {
	case 0x01:
		addr := make([]byte, 4)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", 0, err
		}
		host = net.IP(addr).String()
	case 0x03:
		var size [1]byte
		if _, err := io.ReadFull(conn, size[:]); err != nil {
			return "", 0, err
		}
		addr := make([]byte, int(size[0]))
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", 0, err
		}
		host = string(addr)
	case 0x04:
		addr := make([]byte, 16)
		if _, err := io.ReadFull(conn, addr); err != nil {
			return "", 0, err
		}
		host = net.IP(addr).String()
	default:
		return "", 0, fmt.Errorf("unsupported atyp: %d", header[3])
	}

	var portBytes [2]byte
	if _, err := io.ReadFull(conn, portBytes[:]); err != nil {
		return "", 0, err
	}
	port := int(binary.BigEndian.Uint16(portBytes[:]))
	return host, port, nil
}

func writeSocksReply(conn net.Conn, rep byte) error {
	reply := []byte{0x05, rep, 0x00, 0x01, 0, 0, 0, 0, 0, 0}
	_, err := conn.Write(reply)
	return err
}

func BuildSocksURL(host string, port int, proxyID, password string) string {
	return "socks5://" + proxyID + ":" + password + "@" + host + ":" + strconv.Itoa(port)
}
