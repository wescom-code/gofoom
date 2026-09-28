package proxy

import (
	"context"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"strconv"
	"time"
)

// SOCKS4Dialer implements dialing via SOCKS4 / SOCKS4a proxy servers.
type SOCKS4Dialer struct {
	ProxyAddress string
	Username     string
	Timeout      time.Duration
}

// DialContext dials the target address via SOCKS4/SOCKS4a.
func (d *SOCKS4Dialer) DialContext(ctx context.Context, network, targetAddr string) (net.Conn, error) {
	if network != "tcp" && network != "tcp4" && network != "tcp6" {
		return nil, fmt.Errorf("socks4: unsupported network %s", network)
	}

	host, portStr, err := net.SplitHostPort(targetAddr)
	if err != nil {
		return nil, fmt.Errorf("socks4: invalid target address %s: %w", targetAddr, err)
	}

	portNum, err := strconv.Atoi(portStr)
	if err != nil || portNum < 1 || portNum > 65535 {
		return nil, fmt.Errorf("socks4: invalid port %s", portStr)
	}

	dialer := net.Dialer{Timeout: d.Timeout}
	conn, err := dialer.DialContext(ctx, "tcp", d.ProxyAddress)
	if err != nil {
		return nil, fmt.Errorf("socks4: failed to connect to proxy %s: %w", d.ProxyAddress, err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		_ = conn.SetDeadline(deadline)
	}

	// Try parsing target host as IPv4
	ip := net.ParseIP(host)
	isIPv4 := ip != nil && ip.To4() != nil

	req := make([]byte, 0, 128)
	req = append(req, 0x04) // VN: SOCKS 4
	req = append(req, 0x01) // CD: CONNECT

	// DSTPORT (2 bytes big endian)
	var portBuf [2]byte
	binary.BigEndian.PutUint16(portBuf[:], uint16(portNum))
	req = append(req, portBuf[:]...)

	if isIPv4 {
		// Standard SOCKS4: 4 bytes IPv4
		req = append(req, ip.To4()...)
		// USERID (null terminated)
		req = append(req, []byte(d.Username)...)
		req = append(req, 0x00)
	} else {
		// SOCKS4a: IP = 0.0.0.1, followed by USERID (null), followed by DOMAIN (null)
		req = append(req, 0x00, 0x00, 0x00, 0x01)
		req = append(req, []byte(d.Username)...)
		req = append(req, 0x00)
		req = append(req, []byte(host)...)
		req = append(req, 0x00)
	}

	if _, err := conn.Write(req); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks4: failed to send handshake: %w", err)
	}

	// Server reply: 8 bytes
	// [VN(1), CD(1), DSTPORT(2), DSTIP(4)]
	var resp [8]byte
	if _, err := io.ReadFull(conn, resp[:]); err != nil {
		_ = conn.Close()
		return nil, fmt.Errorf("socks4: failed to read reply: %w", err)
	}

	// Reset deadline
	_ = conn.SetDeadline(time.Time{})

	// Check response status CD
	// 0x5A (90): request granted
	// 0x5B (91): request rejected or failed
	// 0x5C (92): request failed because client is not running identd
	// 0x5D (93): request failed because client's identd could not confirm user ID
	if resp[1] != 0x5A {
		_ = conn.Close()
		return nil, fmt.Errorf("socks4: proxy connection rejected with code 0x%02X", resp[1])
	}

	return conn, nil
}
