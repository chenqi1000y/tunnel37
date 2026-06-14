package server

import (
	"os"
	"strconv"
	"strings"
)

type Config struct {
	HTTPAddr    string
	RelayAddr   string
	SocksAddr   string
	SharedToken string
	AdminUser   string
	AdminPass   string

	RedisAddr     string
	RedisPassword string
	RedisDB       int
	RedisPrefix   string

	PublicBaseURL   string
	PublicRelayAddr string
	SocksHost       string
	SocksPort       int
}

func LoadConfigFromEnv() Config {
	cfg := Config{
		HTTPAddr:        getenv("TUNNEL_SERVER_ADDR", ":9080"),
		RelayAddr:       getenv("TUNNEL_TCP_ADDR", ":9081"),
		SocksAddr:       getenv("TUNNEL_SOCKS_ADDR", ":21080"),
		SharedToken:     getenv("TUNNEL_SHARED_TOKEN", "demo-secret"),
		AdminUser:       os.Getenv("TUNNEL_ADMIN_USER"),
		AdminPass:       os.Getenv("TUNNEL_ADMIN_PASS"),
		RedisAddr:       strings.TrimSpace(os.Getenv("TUNNEL_REDIS_ADDR")),
		RedisPassword:   os.Getenv("TUNNEL_REDIS_PASSWORD"),
		RedisDB:         getenvInt("TUNNEL_REDIS_DB", 0),
		RedisPrefix:     getenv("TUNNEL_REDIS_PREFIX", "tunnel"),
		PublicBaseURL:   strings.TrimRight(strings.TrimSpace(os.Getenv("TUNNEL_PUBLIC_BASE")), "/"),
		PublicRelayAddr: strings.TrimSpace(os.Getenv("TUNNEL_PUBLIC_RELAY_ADDR")),
		SocksHost:       strings.TrimSpace(os.Getenv("TUNNEL_SOCKS_HOST")),
		SocksPort:       getenvInt("TUNNEL_SOCKS_PORT_PUBLIC", parseAddrPort(getenv("TUNNEL_SOCKS_ADDR", ":21080"), 21080)),
	}
	if cfg.PublicBaseURL == "" {
		cfg.PublicBaseURL = "https://tunnel.ma37.com"
	}
	if cfg.PublicRelayAddr == "" {
		cfg.PublicRelayAddr = "tunnel.ma37.com:9081"
	}
	if cfg.SocksHost == "" {
		cfg.SocksHost = "tunnel.ma37.com"
	}
	return cfg
}

func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

func getenvInt(key string, fallback int) int {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback
	}
	number, err := strconv.Atoi(value)
	if err != nil {
		return fallback
	}
	return number
}

func parseAddrPort(addr string, fallback int) int {
	addr = strings.TrimSpace(addr)
	if addr == "" {
		return fallback
	}
	if idx := strings.LastIndex(addr, ":"); idx >= 0 && idx < len(addr)-1 {
		if port, err := strconv.Atoi(addr[idx+1:]); err == nil {
			return port
		}
	}
	return fallback
}
