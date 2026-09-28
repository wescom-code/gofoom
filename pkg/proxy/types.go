package proxy

import (
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

// ProxyType defines the type of upstream proxy.
type ProxyType string

const (
	ProxyTypeDirect ProxyType = "DIRECT"
	ProxyTypeHTTP   ProxyType = "HTTP"
	ProxyTypeSocks4 ProxyType = "SOCKS4"
	ProxyTypeSocks5 ProxyType = "SOCKS5"
	ProxyTypePAC    ProxyType = "PAC"
)

// NormalizeProxyType converts a string to a valid ProxyType or returns an error.
func NormalizeProxyType(s string) (ProxyType, error) {
	upper := strings.ToUpper(strings.TrimSpace(s))
	switch ProxyType(upper) {
	case ProxyTypeDirect:
		return ProxyTypeDirect, nil
	case ProxyTypeHTTP:
		return ProxyTypeHTTP, nil
	case ProxyTypeSocks4:
		return ProxyTypeSocks4, nil
	case ProxyTypeSocks5:
		return ProxyTypeSocks5, nil
	case ProxyTypePAC:
		return ProxyTypePAC, nil
	default:
		return "", fmt.Errorf("unknown proxy type: %q", s)
	}
}

// UpstreamConfig defines upstream proxy parameters.
type UpstreamConfig struct {
	Type                  ProxyType `json:"proxyType"`
	Host                  string    `json:"proxyHost"`
	Port                  int       `json:"proxyPort"`
	Username              string    `json:"proxyUsername,omitempty"`
	Password              string    `json:"proxyPassword,omitempty"`
	UseCurrentCredentials bool      `json:"useCurrentCredentials"`
	HttpAuthProtocol      string    `json:"httpAuthProtocol,omitempty"` // NTLM, BASIC, KERBEROS, etc.
	PacFileLocation       string    `json:"pacFileLocation,omitempty"`
}

// ServerConfig holds the proxy server configuration.
type ServerConfig struct {
	LocalPort      int            `json:"localPort"`
	Upstream       UpstreamConfig `json:"upstream"`
	ProxyTestURL   string         `json:"proxyTestUrl"`
	ConnectTimeout time.Duration  `json:"connectTimeout"`
	ReadTimeout    time.Duration  `json:"readTimeout"`
	WriteTimeout   time.Duration  `json:"writeTimeout"`
}

// DefaultServerConfig returns standard default settings matching Winfoom defaults.
func DefaultServerConfig() ServerConfig {
	return ServerConfig{
		LocalPort: 3129,
		Upstream: UpstreamConfig{
			Type:                  ProxyTypeDirect,
			Host:                  "",
			Port:                  0,
			UseCurrentCredentials: true,
		},
		ProxyTestURL:   "https://example.com",
		ConnectTimeout: 10 * time.Second,
		ReadTimeout:    60 * time.Second,
		WriteTimeout:   60 * time.Second,
	}
}

// LogEvent represents an event generated during proxy operations.
type LogEvent struct {
	Timestamp time.Time `json:"timestamp"`
	Level     string    `json:"level"` // "INFO", "WARN", "ERROR", "DEBUG"
	Client    string    `json:"client"`
	Method    string    `json:"method"`
	Target    string    `json:"target"`
	Status    int       `json:"status"`
	Message   string    `json:"message"`
	Duration  time.Duration `json:"duration"`
}

// Stats tracks runtime network traffic statistics.
type Stats struct {
	ActiveConnections int64  `json:"activeConnections"`
	TotalRequests     uint64 `json:"totalRequests"`
	TotalConnects     uint64 `json:"totalConnects"`
	BytesReceived     uint64 `json:"bytesReceived"`
	BytesSent         uint64 `json:"bytesSent"`
	ErrorsCount       uint64 `json:"errorsCount"`
}

// IncActive increments active connections.
func (s *Stats) IncActive() { atomic.AddInt64(&s.ActiveConnections, 1) }

// DecActive decrements active connections.
func (s *Stats) DecActive() { atomic.AddInt64(&s.ActiveConnections, -1) }

// IncRequests increments total requests.
func (s *Stats) IncRequests() { atomic.AddUint64(&s.TotalRequests, 1) }

// IncConnects increments total connect requests.
func (s *Stats) IncConnects() { atomic.AddUint64(&s.TotalConnects, 1) }

// AddBytesIn adds received bytes.
func (s *Stats) AddBytesIn(n uint64) { atomic.AddUint64(&s.BytesReceived, n) }

// AddBytesOut adds sent bytes.
func (s *Stats) AddBytesOut(n uint64) { atomic.AddUint64(&s.BytesSent, n) }

// IncErrors increments error count.
func (s *Stats) IncErrors() { atomic.AddUint64(&s.ErrorsCount, 1) }
