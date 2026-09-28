package proxy

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gofoom/pkg/auth"
	xproxy "golang.org/x/net/proxy"
)

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

// DialContext establishes a connection to target (host:port) according to upstream proxy type.
func (d *UpstreamDialer) DialContext(ctx context.Context, network, target string) (net.Conn, error) {
	switch d.config.Type {
	case ProxyTypeDirect:
		dialer := net.Dialer{Timeout: d.timeout}
		return dialer.DialContext(ctx, network, target)

	case ProxyTypeSocks5:
		proxyAddr := fmt.Sprintf("%s:%d", d.config.Host, d.config.Port)
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
			return contextDialer.DialContext(ctx, network, target)
		}
		return socksDialer.Dial(network, target)

	case ProxyTypeSocks4:
		proxyAddr := fmt.Sprintf("%s:%d", d.config.Host, d.config.Port)
		s4 := &SOCKS4Dialer{
			ProxyAddress: proxyAddr,
			Username:     d.config.Username,
			Timeout:      d.timeout,
		}
		return s4.DialContext(ctx, network, target)

	default:
		dialer := net.Dialer{Timeout: d.timeout}
		return dialer.DialContext(ctx, network, target)
	}
}

// DialTunnel establishes a CONNECT tunnel through an upstream proxy (HTTP, SOCKS4, SOCKS5, or DIRECT).
// For HTTP proxy, it handles multi-step NTLM / Kerberos (SSPI) / Basic 407 challenge-response handshakes.
func (d *UpstreamDialer) DialTunnel(ctx context.Context, target string) (net.Conn, error) {
	switch d.config.Type {
	case ProxyTypeDirect, ProxyTypeSocks4, ProxyTypeSocks5:
		return d.DialContext(ctx, "tcp", target)

	case ProxyTypeHTTP:
		proxyAddr := fmt.Sprintf("%s:%d", d.config.Host, d.config.Port)
		dialer := net.Dialer{Timeout: d.timeout}
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
		connectReq.Header.Set("User-Agent", "Gofoom/1.0")

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
			authReq.Header.Set("User-Agent", "Gofoom/1.0")
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

	default:
		return d.DialContext(ctx, "tcp", target)
	}
}
