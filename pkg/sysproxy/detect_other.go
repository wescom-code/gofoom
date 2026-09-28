//go:build !windows

package sysproxy

import (
	"os"
	"net/url"
	"strconv"
)

// DetectSystemProxy reads environment variables (http_proxy, all_proxy) on non-Windows platforms.
func DetectSystemProxy() (*DetectedProxy, error) {
	proxyEnv := os.Getenv("http_proxy")
	if proxyEnv == "" {
		proxyEnv = os.Getenv("HTTP_PROXY")
	}
	if proxyEnv == "" {
		proxyEnv = os.Getenv("all_proxy")
	}

	if proxyEnv == "" {
		return &DetectedProxy{Type: "DIRECT"}, nil
	}

	u, err := url.Parse(proxyEnv)
	if err != nil {
		return &DetectedProxy{Type: "DIRECT"}, nil
	}

	port, _ := strconv.Atoi(u.Port())
	if port == 0 {
		port = 8080
	}

	pType := "HTTP"
	if u.Scheme == "socks5" || u.Scheme == "socks" {
		pType = "SOCKS5"
	}

	return &DetectedProxy{
		Type: pType,
		Host: u.Hostname(),
		Port: port,
	}, nil
}
