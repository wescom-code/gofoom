package proxy

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

var bufferPool = sync.Pool{
	New: func() interface{} {
		buf := make([]byte, 32*1024) // 32KB buffer for high throughput
		return &buf
	},
}

// handleConnect processes HTTP CONNECT requests for TLS tunneling.
func (s *ProxyServer) handleConnect(w http.ResponseWriter, r *http.Request) {
	start := time.Now()
	target := r.URL.Host
	if target == "" {
		target = r.Host
	}
	if !strings.Contains(target, ":") {
		target += ":443"
	}

	s.stats.IncConnects()
	s.stats.IncActive()
	defer s.stats.DecActive()

	ctx, cancel := context.WithTimeout(r.Context(), s.config.ConnectTimeout)
	defer cancel()

	// Dial upstream (DIRECT, SOCKS, or upstream HTTP proxy)
	dialer := NewUpstreamDialer(s.config.Upstream, s.config.ConnectTimeout)
	upstreamConn, err := dialer.DialTunnel(ctx, target)
	if err != nil {
		s.stats.IncErrors()
		s.log(LogEvent{
			Timestamp: time.Now(),
			Level:     "ERROR",
			Client:    r.RemoteAddr,
			Method:    r.Method,
			Target:    target,
			Status:    http.StatusBadGateway,
			Message:   fmt.Sprintf("Failed to dial upstream: %v", err),
			Duration:  time.Since(start),
		})
		http.Error(w, fmt.Sprintf("Gofoom: Bad Gateway: %v", err), http.StatusBadGateway)
		return
	}
	defer upstreamConn.Close()

	// Hijack client connection
	hijacker, ok := w.(http.Hijacker)
	if !ok {
		s.stats.IncErrors()
		http.Error(w, "Gofoom: Hijacking not supported", http.StatusInternalServerError)
		return
	}

	clientConn, clientBuf, err := hijacker.Hijack()
	if err != nil {
		s.stats.IncErrors()
		http.Error(w, fmt.Sprintf("Gofoom: Hijack failed: %v", err), http.StatusInternalServerError)
		return
	}
	defer clientConn.Close()

	// Write 200 Connection Established to client
	_, err = clientConn.Write([]byte("HTTP/1.1 200 Connection Established\r\nProxy-Agent: Gofoom/1.0\r\n\r\n"))
	if err != nil {
		s.stats.IncErrors()
		return
	}

	// Flush any buffered bytes already read by clientBuf
	if clientBuf != nil && clientBuf.Reader.Buffered() > 0 {
		bufferedData := make([]byte, clientBuf.Reader.Buffered())
		n, _ := clientBuf.Read(bufferedData)
		if n > 0 {
			if _, err := upstreamConn.Write(bufferedData[:n]); err != nil {
				return
			}
			s.stats.AddBytesOut(uint64(n))
		}
	}

	// Perform full-duplex bidirectional stream transfer
	s.pipeDuplex(clientConn, upstreamConn, r.RemoteAddr, target, start)
}

// pipeDuplex handles bidirectional byte copying between client and upstream connections.
func (s *ProxyServer) pipeDuplex(clientConn, upstreamConn net.Conn, clientAddr, target string, start time.Time) {
	var wg sync.WaitGroup
	wg.Add(2)

	var bytesIn, bytesOut uint64

	// Client -> Upstream
	go func() {
		defer wg.Done()
		bufPtr := bufferPool.Get().(*[]byte)
		defer bufferPool.Put(bufPtr)

		n, _ := io.CopyBuffer(upstreamConn, clientConn, *bufPtr)
		bytesOut = uint64(n)
		s.stats.AddBytesOut(bytesOut)
		_ = upstreamConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	}()

	// Upstream -> Client
	go func() {
		defer wg.Done()
		bufPtr := bufferPool.Get().(*[]byte)
		defer bufferPool.Put(bufPtr)

		n, _ := io.CopyBuffer(clientConn, upstreamConn, *bufPtr)
		bytesIn = uint64(n)
		s.stats.AddBytesIn(bytesIn)
		_ = clientConn.SetReadDeadline(time.Now().Add(500 * time.Millisecond))
	}()

	wg.Wait()

	s.log(LogEvent{
		Timestamp: time.Now(),
		Level:     "INFO",
		Client:    clientAddr,
		Method:    "CONNECT",
		Target:    target,
		Status:    http.StatusOK,
		Message:   fmt.Sprintf("Tunnel closed (In: %d bytes, Out: %d bytes)", bytesIn, bytesOut),
		Duration:  time.Since(start),
	})
}
