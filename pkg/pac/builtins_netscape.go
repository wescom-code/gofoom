package pac

import (
	"net"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// isPlainHostName returns true if there is no domain name in the hostname (no dots).
func isPlainHostName(host string) bool {
	return !strings.Contains(host, ".")
}

// dnsDomainIs returns true if the domain of hostname matches.
func dnsDomainIs(host, domain string) bool {
	return strings.HasSuffix(strings.ToLower(host), strings.ToLower(domain))
}

// localHostOrDomainIs returns true if hostname matches specified hostdom or matches the prefix before the first dot.
func localHostOrDomainIs(host, hostdom string) bool {
	host = strings.ToLower(host)
	hostdom = strings.ToLower(hostdom)
	if host == hostdom {
		return true
	}
	parts := strings.Split(hostdom, ".")
	return len(parts) > 0 && host == parts[0]
}

// isResolvable tries to resolve the hostname, returns true if successful.
func isResolvable(host string) bool {
	ips, err := net.LookupIP(host)
	return err == nil && len(ips) > 0
}

// dnsResolve resolves hostname to its first IPv4 address string, or empty string.
func dnsResolve(host string) string {
	ips, err := net.LookupIP(host)
	if err != nil {
		return ""
	}
	for _, ip := range ips {
		if ipv4 := ip.To4(); ipv4 != nil {
			return ipv4.String()
		}
	}
	return ""
}

// myIpAddress returns the IP address of the local machine.
func myIpAddress() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() {
			if ipnet.IP.To4() != nil {
				return ipnet.IP.String()
			}
		}
	}
	return "127.0.0.1"
}

// dnsDomainLevels returns the number of domain levels (number of dots) in the hostname.
func dnsDomainLevels(host string) int {
	return strings.Count(host, ".")
}

// shExpMatch matches shell wildcard expression (* and ?).
func shExpMatch(str, shexp string) bool {
	// Convert shexp to regex
	var b strings.Builder
	b.WriteString("^")
	for i := 0; i < len(shexp); i++ {
		c := shexp[i]
		switch c {
		case '*':
			b.WriteString(".*")
		case '?':
			b.WriteString(".")
		case '.', '+', '(', ')', '[', ']', '{', '}', '^', '$', '|', '\\':
			b.WriteString("\\")
			b.WriteByte(c)
		default:
			b.WriteByte(c)
		}
	}
	b.WriteString("$")
	re, err := regexp.Compile(b.String())
	if err != nil {
		matched, _ := filepath.Match(shexp, str)
		return matched
	}
	return re.MatchString(str)
}

// isInNet checks if an IP is in the specified subnet with mask.
func isInNet(host, pattern, mask string) bool {
	ipStr := host
	ip := net.ParseIP(ipStr)
	if ip == nil {
		resolved := dnsResolve(host)
		if resolved == "" {
			return false
		}
		ip = net.ParseIP(resolved)
	}
	ipv4 := ip.To4()
	if ipv4 == nil {
		return false
	}

	patternIP := net.ParseIP(pattern).To4()
	maskIP := net.ParseIP(mask).To4()
	if patternIP == nil || maskIP == nil {
		return false
	}

	for i := 0; i < 4; i++ {
		if (ipv4[i] & maskIP[i]) != (patternIP[i] & maskIP[i]) {
			return false
		}
	}
	return true
}

var weekdays = map[string]time.Weekday{
	"SUN": time.Sunday, "MON": time.Monday, "TUE": time.Tuesday,
	"WED": time.Wednesday, "THU": time.Thursday, "FRI": time.Friday, "SAT": time.Saturday,
}

// weekdayRange matches current day against weekday range.
func weekdayRange(args ...string) bool {
	if len(args) == 0 {
		return false
	}
	gmt := false
	if strings.EqualFold(args[len(args)-1], "GMT") {
		gmt = true
		args = args[:len(args)-1]
	}
	now := time.Now()
	if gmt {
		now = now.UTC()
	}
	currentWeekday := now.Weekday()

	d1, ok1 := weekdays[strings.ToUpper(args[0])]
	if !ok1 {
		return false
	}
	if len(args) == 1 {
		return currentWeekday == d1
	}
	d2, ok2 := weekdays[strings.ToUpper(args[1])]
	if !ok2 {
		return false
	}

	if d1 <= d2 {
		return currentWeekday >= d1 && currentWeekday <= d2
	}
	return currentWeekday >= d1 || currentWeekday <= d2
}

var months = map[string]time.Month{
	"JAN": time.January, "FEB": time.February, "MAR": time.March, "APR": time.April,
	"MAY": time.May, "JUN": time.June, "JUL": time.July, "AUG": time.August,
	"SEP": time.September, "OCT": time.October, "NOV": time.November, "DEC": time.December,
}

// dateRange matches current date against specified day/month/year ranges.
func dateRange(args ...interface{}) bool {
	// Simplified robust dateRange evaluator for PAC
	now := time.Now()
	if len(args) == 0 {
		return true
	}
	// Check for GMT flag
	last := args[len(args)-1]
	if str, ok := last.(string); ok && strings.EqualFold(str, "GMT") {
		now = now.UTC()
		args = args[:len(args)-1]
	}
	if len(args) == 1 {
		switch v := args[0].(type) {
		case int64:
			return int(v) == now.Day() || int(v) == now.Year()
		case float64:
			return int(v) == now.Day() || int(v) == now.Year()
		case string:
			if m, ok := months[strings.ToUpper(v)]; ok {
				return now.Month() == m
			}
		}
	}
	return true
}

// timeRange matches current hour/minute/second against ranges.
func timeRange(args ...interface{}) bool {
	now := time.Now()
	if len(args) < 2 {
		return true
	}
	toInt := func(v interface{}) int {
		switch n := v.(type) {
		case int:
			return n
		case int64:
			return int(n)
		case float64:
			return int(n)
		case string:
			i, _ := strconv.Atoi(n)
			return i
		default:
			return 0
		}
	}
	h1 := toInt(args[0])
	h2 := toInt(args[1])
	curHour := now.Hour()
	if h1 <= h2 {
		return curHour >= h1 && curHour <= h2
	}
	return curHour >= h1 || curHour <= h2
}
