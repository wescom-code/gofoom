//go:build windows

package sysproxy

import (
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// DetectSystemProxy reads Internet Explorer / Windows system proxy settings from Registry.
func DetectSystemProxy() (*DetectedProxy, error) {
	k, err := registry.OpenKey(registry.CURRENT_USER, `Software\Microsoft\Windows\CurrentVersion\Internet Settings`, registry.QUERY_VALUE)
	if err != nil {
		return nil, err
	}
	defer k.Close()

	res := &DetectedProxy{}

	// Check PAC AutoConfigURL
	pacURL, _, err := k.GetStringValue("AutoConfigURL")
	if err == nil && pacURL != "" {
		res.Type = "PAC"
		res.PacURL = pacURL
		return res, nil
	}

	// Check ProxyEnable
	proxyEnable, _, err := k.GetIntegerValue("ProxyEnable")
	if err == nil && proxyEnable == 1 {
		server, _, err := k.GetStringValue("ProxyServer")
		if err == nil && server != "" {
			parseProxyServerString(server, res)
			return res, nil
		}
	}

	res.Type = "DIRECT"
	return res, nil
}

func parseProxyServerString(server string, res *DetectedProxy) {
	// server can be "127.0.0.1:8080" or "http=1.2.3.4:80;https=1.2.3.4:80;socks=1.2.3.4:1080"
	if !strings.Contains(server, "=") {
		res.Type = "HTTP"
		host, port := splitHostPort(server, 8080)
		res.Host = host
		res.Port = port
		return
	}

	parts := strings.Split(server, ";")
	for _, p := range parts {
		kv := strings.SplitN(strings.TrimSpace(p), "=", 2)
		if len(kv) == 2 {
			proto := strings.ToLower(kv[0])
			addr := kv[1]
			if proto == "http" || proto == "https" {
				res.Type = "HTTP"
				host, port := splitHostPort(addr, 8080)
				res.Host = host
				res.Port = port
				return
			} else if proto == "socks" {
				res.Type = "SOCKS5"
				host, port := splitHostPort(addr, 1080)
				res.Host = host
				res.Port = port
				return
			}
		}
	}
	res.Type = "DIRECT"
}

func splitHostPort(addr string, defaultPort int) (string, int) {
	if !strings.Contains(addr, ":") {
		return addr, defaultPort
	}
	parts := strings.Split(addr, ":")
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return parts[0], defaultPort
	}
	return parts[0], port
}
