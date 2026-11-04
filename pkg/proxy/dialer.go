package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"gofoom/pkg/auth"
	xproxy "golang.org/x/net/proxy"
)

type dnsEntry struct {
	ip        string
	expiresAt time.Time
}

var (
	dnsCache   = make(map[string]dnsEntry)
	dnsCacheMu sync.RWMutex
)

// resolveHost resolves a hostname to an IP address string with in-memory caching and stale fallback.
func resolveHost(ctx context.Context, host string) (string, error) {
	if net.ParseIP(host) != nil {
		return host, nil
	}

	now := time.Now()
	dnsCacheMu.RLock()
	entry, found := dnsCache[host]
	dnsCacheMu.RUnlock()

	if found && now.Before(entry.expiresAt) {
		return entry.ip, nil
	}

	// Not in cache or expired: query DNS with a dedicated timeout
	lookupCtx, cancel := context.WithTimeout(ctx, 4*time.Second)
	defer cancel()

	ips, err := net.DefaultResolver.LookupIP(lookupCtx, "ip", host)
	if err == nil && len(ips) > 0 {
		selectedIP := ips[0].String()
		dnsCacheMu.Lock()
		dnsCache[host] = dnsEntry{
			ip:        selectedIP,
			expiresAt: now.Add(5 * time.Minute),
		}
		dnsCacheMu.Unlock()
		return selectedIP, nil
	}

	// Lookup failed: if we have a stale entry, fall back to it!
	if found && entry.ip != "" {
		return entry.ip, nil
	}

	if err != nil {
		return host, err
	}
	return host, fmt.Errorf("no IP addresses found for host: %s", host)
}

// resolveTarget splits target (host:port), resolves the host with DNS cache, and returns resolved host:port.
func resolveTarget(ctx context.Context, target string) string {
	host, port, err := net.SplitHostPort(target)
	if err != nil {
		return target
	}
	resolvedIP, err := resolveHost(ctx, host)
	if err != nil {
		return target
	}
	return net.JoinHostPort(resolvedIP, port)
}

// UpstreamDialer creates network connections to target endpoints through the configured upstream.
type UpstreamDialer struct {
	config  UpstreamConfig
	timeout time.Duration
}

// NewUpstreamDialer creates a new UpstreamDialer.
func NewUpstreamDialer(cfg UpstreamConfig, timeout time.Duration) *UpstreamDialer {
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &UpstreamDialer{
		config:  cfg,
		timeout: timeout,
	}
}

// DialContext establishes a connection to target (host:port) according to upstream proxy type,
// with automatic transient retry (up to 2 attempts).
func (d *UpstreamDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	var lastErr error
	maxAttempts := 2

	for attempt := 1; attempt <= maxAttempts; attempt++ {
		attemptCtx, cancel := context.WithTimeout(ctx, d.timeout)
		conn, err := d.dialContextOnce(attemptCtx, network, target)
		cancel()

		if err == nil {
			return conn, nil
		}
		lastErr = err

		// If caller canceled (e.g. client closed browser tab), exit immediately
		if ctx.Err() != nil {
			return nil, lastErr
		}

		if attempt < maxAttempts {
			time.Sleep(50 * time.Millisecond)
		}
	}
	return nil, lastErr
}

func (d *UpstreamDialer) dialContextOnce(ctx context.Context, network, target string) (net.Conn, error) {
	dialTarget := resolveTarget(ctx, target)
	switch d.config.Type {
	case ProxyTypeDirect:
		dialer := net.Dialer{
			Timeout:   d.timeout,
			KeepAlive: 30 * time.Second,
		}
		return dialer.DialContext(ctx, network, dialTarget)

	case ProxyTypeSocks5:
		proxyAddr := resolveTarget(ctx, fmt.Sprintf("%s:%d", d.config.Host, d.config.Port))
		var sAuth *xproxy.Auth
		if d.config.Username != "" {
			sAuth = &xproxy.Auth{
				User:     d.config.Username,
				Password: d.config.Password,
			}
		}
		socksDialer, err := xproxy.SOCKS5("tcp", proxyAddr, sAuth, xproxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("failed to create socks5 dialer: %w", err)
		}
		if contextDialer, ok := socksDialer.(xproxy.ContextDialer); ok {
			return contextDialer.DialContext(ctx, network, dialTarget)
		}
		return socksDialer.Dial(network, dialTarget)

	case ProxyTypeSocks4:
		proxyAddr := resolveTarget(ctx, fmt.Sprintf("%s:%d", d.config.Host, d.config.Port))
		s4 := &SOCKS4Dialer{
			ProxyAddress: proxyAddr,
			Username:     d.config.Username,
			Timeout:      d.timeout,
		}
		return s4.DialContext(ctx, network, dialTarget)

	default:
		dialer := net.Dialer{
			Timeout:   d.timeout,
			KeepAlive: 30 * time.Second,
		}
		return dialer.DialContext(ctx, network, dialTarget)
	}
}

// DialTunnel establishes a CONNECT tunnel through an upstream proxy (HTTP, SOCKS4, SOCKS5, or DIRECT).
// For HTTP proxy, it handles multi-step NTLM / Kerberos (SSPI) / Basic 407 challenge-response handshakes.
func (d *UpstreamDialer) DialTunnel(ctx context.Context, target string) (net.Conn, error) {
	switch d.config.Type {
	case ProxyTypeDirect, ProxyTypeSocks4, ProxyTypeSocks5:
		return d.DialContext(ctx, "tcp", target)

	case ProxyTypeHTTP:
		var lastErr error
		for attempt := 1; attempt <= 2; attempt++ {
			attemptCtx, cancel := context.WithTimeout(ctx, d.timeout)
			conn, err := d.dialHttpTunnelOnce(attemptCtx, target)
			cancel()
			if err == nil {
				return conn, nil
			}
			lastErr = err
			if ctx.Err() != nil {
				return nil, lastErr
			}
			if attempt < 2 {
				time.Sleep(50 * time.Millisecond)
			}
		}
		return nil, lastErr

	default:
		return d.DialContext(ctx, "tcp", target)
	}
}

func (d *UpstreamDialer) dialHttpTunnelOnce(ctx context.Context, target string) (net.Conn, error) {
	proxyAddr := resolveTarget(ctx, fmt.Sprintf("%s:%d", d.config.Host, d.config.Port))
	dialer := net.Dialer{
		Timeout:   d.timeout,
		KeepAlive: 30 * time.Second,
	}
	conn, err := dialer.DialContext(ctx, "tcp", proxyAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to upstream http proxy %s: %w", proxyAddr, err)
	}

	cred := auth.Credentials{
		Username:              d.config.Username,
		Password:              d.config.Password,
		UseCurrentCredentials: d.config.UseCurrentCredentials,
		PreferredScheme:       auth.AuthScheme(strings.ToUpper(d.config.HttpAuthProtocol)),
	}

	session, _ := auth.CreateSession(cred, d.config.Host)
	if session != nil {
		defer session.Close()
	}

	// Initial CONNECT request
	connectReq := &http.Request{
		Method: http.MethodConnect,
		URL:    &url.URL{Opaque: target},
		Host:   target,
		Header: make(http.Header),
	}
	connectReq.Header.Set("Proxy-Connection", "Keep-Alive")
	connectReq.Header.Set("User-Agent", "Gofoom/1.1")

	if session != nil {
		if initHdr, err := session.InitialHeader(); err == nil && initHdr != "" {
			connectReq.Header.Set("Proxy-Authorization", initHdr)
		}
	}

	if err := connectReq.Write(conn); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to send CONNECT to upstream proxy: %w", err)
	}

	br := bufio.NewReader(conn)
	resp, err := http.ReadResponse(br, connectReq)
	if err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("failed to read CONNECT response: %w", err)
	}
	_ = resp.Body.Close()

	// If 200 OK, tunnel established!
	if resp.StatusCode == http.StatusOK {
		return conn, nil
	}

	// If 407 Proxy Authentication Required, handle challenge
	if resp.StatusCode == http.StatusProxyAuthRequired && session != nil {
		challenge := resp.Header.Get("Proxy-Authenticate")
		respToken, err := session.ProcessChallenge(challenge)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("authentication challenge processing failed: %w", err)
		}

		// Send Type 3 response on the same keep-alive TCP connection
		authReq := &http.Request{
			Method: http.MethodConnect,
			URL:    &url.URL{Opaque: target},
			Host:   target,
			Header: make(http.Header),
		}
		authReq.Header.Set("Proxy-Connection", "Keep-Alive")
		authReq.Header.Set("User-Agent", "Gofoom/1.1")
		authReq.Header.Set("Proxy-Authorization", respToken)

		if err := authReq.Write(conn); err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("failed to send auth response: %w", err)
		}

		authResp, err := http.ReadResponse(br, authReq)
		if err != nil {
			_ = conn.Close()
			return nil, fmt.Errorf("failed to read auth response: %w", err)
		}
		_ = authResp.Body.Close()

		if authResp.StatusCode == http.StatusOK {
			return conn, nil
		}

		_ = conn.Close()
		return nil, fmt.Errorf("upstream proxy auth rejected: status %d %s", authResp.StatusCode, authResp.Status)
	}

	_ = conn.Close()
	return nil, fmt.Errorf("upstream proxy rejected CONNECT with status: %d %s", resp.StatusCode, resp.Status)
}
