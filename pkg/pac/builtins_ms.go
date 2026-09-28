package pac

import (
	"net"
	"strings"
)

// dnsResolveEx resolves hostname to all IPv4 and IPv6 addresses, separated by semicolons.
func dnsResolveEx(host string) string {
	ips, err := net.LookupIP(host)
	if err != nil || len(ips) == 0 {
		return ""
	}
	var res []string
	for _, ip := range ips {
		res = append(res, ip.String())
	}
	return strings.Join(res, ";")
}

// myIpAddressEx returns all local IPv4 and IPv6 addresses separated by semicolons.
func myIpAddressEx() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	var res []string
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			res = append(res, ipnet.IP.String())
		}
	}
	if len(res) == 0 {
		return "127.0.0.1"
	}
	return strings.Join(res, ";")
}

// isInNetEx checks if an IP address is within an IP/prefix string (supporting both IPv4 & IPv6 CIDR).
func isInNetEx(ipStr, prefixStr string) bool {
	ip := net.ParseIP(ipStr)
	if ip == nil {
		resolved := dnsResolve(ipStr)
		if resolved == "" {
			return false
		}
		ip = net.ParseIP(resolved)
	}
	if ip == nil {
		return false
	}

	_, ipnet, err := net.ParseCIDR(prefixStr)
	if err != nil {
		// If prefixStr is not in CIDR notation, try treating it as IP
		prefixIP := net.ParseIP(prefixStr)
		if prefixIP != nil {
			return ip.Equal(prefixIP)
		}
		return false
	}
	return ipnet.Contains(ip)
}

// sortIpAddressList sorts an IP list by prefix or returns as is.
func sortIpAddressList(list string) string {
	return list
}

// getClientVersion returns the client version.
func getClientVersion() string {
	return "1.0"
}
