package proxy

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Hop-by-hop headers to remove in proxy requests according to RFC 7230
var hopByHopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

// removeHopByHopHeaders strips connection-specific headers.
func removeHopByHopHeaders(header http.Header) {
	for _, h := range hopByHopHeaders {
		header.Del(h)
	}
	// Also remove headers listed in the "Connection" header itself
	if connHeader := header.Get("Connection"); connHeader != "" {
		for _, token := range strings.Split(connHeader, ",") {
			header.Del(strings.TrimSpace(token))
		}
	}
}

// handleForward processes standard non-CONNECT HTTP requests.
func (s *ProxyServer) handleForward(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	s.stats.IncRequests()
	s.stats.IncActive()
	defer s.stats.DecActive()

	// Ensure target URL is complete
	targetURL := r.URL
	if targetURL.Scheme == "" {
		targetURL.Scheme = "http"
	}
	if targetURL.Host == "" {
		targetURL.Host = r.Host
	}

	// Create cloned outbound request
	outReq, err := http.NewRequestWithContext(r.Context(), r.Method, targetURL.String(), r.Body)
	if err != nil {
		s.stats.IncErrors()
		http.Error(w, fmt.Sprintf("Gofoom: Bad Request: %v", err), http.StatusBadRequest)
		return
	}

	// Copy headers
	outReq.Header = r.Header.Clone()
	removeHopByHopHeaders(outReq.Header)

	// Add standard forwarding metadata
	clientIP, _, err := net.SplitHostPort(r.RemoteAddr)
	if err == nil {
		if prior := outReq.Header.Get("X-Forwarded-For"); prior != "" {
			clientIP = prior + ", " + clientIP
		}
		outReq.Header.Set("X-Forwarded-For", clientIP)
	}

	// Execute through transport
	transport := s.getTransport()
	resp, err := transport.RoundTrip(outReq)
	if err != nil {
		s.stats.IncErrors()
		s.log(LogEvent{
			Timestamp: time.Now(),
			Level:     "ERROR",
			Client:    r.RemoteAddr,
			Method:    r.Method,
			Target:    targetURL.String(),
			Status:    http.StatusBadGateway,
			Message:   fmt.Sprintf("RoundTrip failed: %v", err),
			Duration:  time.Since(start),
		})
		http.Error(w, fmt.Sprintf("Gofoom: Bad Gateway: %v", err), http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	// Copy response headers
	destHeader := w.Header()
	for k, vv := range resp.Header {
		for _, v := range vv {
			destHeader.Add(k, v)
		}
	}
	removeHopByHopHeaders(destHeader)
	destHeader.Set("Via", "1.1 Gofoom")

	// Write status code
	w.WriteHeader(resp.StatusCode)

	// Stream response body back to client
	bufPtr := bufferPool.Get().(*[]byte)
	defer bufferPool.Put(bufPtr)

	n, _ := io.CopyBuffer(w, resp.Body, *bufPtr)
	s.stats.AddBytesIn(uint64(n))

	// Flush if supported
	if flusher, ok := w.(http.Flusher); ok {
		flusher.Flush()
	}

	s.log(LogEvent{
		Timestamp: time.Now(),
		Level:     "INFO",
		Client:    r.RemoteAddr,
		Method:    r.Method,
		Target:    targetURL.String(),
		Status:    resp.StatusCode,
		Message:   fmt.Sprintf("%d bytes transferred", n),
		Duration:  time.Since(start),
	})
}

// createTransport builds an http.Transport according to current upstream proxy configuration.
func (s *ProxyServer) createTransport() *http.Transport {
	dialer := NewUpstreamDialer(s.config.Upstream, s.config.ConnectTimeout)

	transport := &http.Transport{
		MaxIdleConns:          100,
		MaxIdleConnsPerHost:   20,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
	}

	switch s.config.Upstream.Type {
	case ProxyTypeHTTP:
		proxyURLStr := fmt.Sprintf("http://%s:%d", s.config.Upstream.Host, s.config.Upstream.Port)
		if u, err := url.Parse(proxyURLStr); err == nil {
			if s.config.Upstream.Username != "" {
				u.User = url.UserPassword(s.config.Upstream.Username, s.config.Upstream.Password)
			}
			transport.Proxy = http.ProxyURL(u)
		}
		// If custom upstream headers are needed (e.g. Basic Proxy-Authorization)
		if s.config.Upstream.Username != "" {
			auth := base64.StdEncoding.EncodeToString([]byte(s.config.Upstream.Username + ":" + s.config.Upstream.Password))
			transport.ProxyConnectHeader = http.Header{
				"Proxy-Authorization": []string{"Basic " + auth},
			}
		}

	case ProxyTypeSocks4, ProxyTypeSocks5:
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			return dialer.DialContext(ctx, network, addr)
		}

	case ProxyTypeDirect:
		transport.DialContext = (&net.Dialer{
			Timeout:   s.config.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext

	default:
		transport.DialContext = (&net.Dialer{
			Timeout:   s.config.ConnectTimeout,
			KeepAlive: 30 * time.Second,
		}).DialContext
	}

	return transport
}
