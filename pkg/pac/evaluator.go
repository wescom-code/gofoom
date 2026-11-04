package pac

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/dop251/goja"
)

// ProxyCandidate represents an evaluated proxy option from a PAC script.
type ProxyCandidate struct {
	Type string // "DIRECT", "HTTP", "SOCKS", "SOCKS4", "SOCKS5"
	Host string
	Port int
}

// PacEvaluator evaluates Proxy Auto-Configuration (PAC) scripts with high performance.
type PacEvaluator struct {
	program     *goja.Program
	runtimePool sync.Pool
	blacklist   *Blacklist
}

// NewEvaluator creates a PacEvaluator from PAC JavaScript source code.
func NewEvaluator(script string) (*PacEvaluator, error) {
	prog, err := goja.Compile("pac.js", script, true)
	if err != nil {
		return nil, fmt.Errorf("failed to compile PAC script: %w", err)
	}

	eval := &PacEvaluator{
		program:   prog,
		blacklist: NewBlacklist(5 * time.Minute),
	}

	eval.runtimePool = sync.Pool{
		New: func() interface{} {
			vm := goja.New()
			eval.registerBuiltins(vm)
			_, _ = vm.RunProgram(prog)
			return vm
		},
	}

	return eval, nil
}

// NewEvaluatorFromLocation loads a PAC script from a file path or URL and creates an evaluator.
func NewEvaluatorFromLocation(loc string) (*PacEvaluator, error) {
	var content []byte
	u, err := url.Parse(loc)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		client := &http.Client{Timeout: 10 * time.Second}
		resp, err := client.Get(loc)
		if err != nil {
			return nil, fmt.Errorf("failed to fetch PAC from %s: %w", loc, err)
		}
		defer resp.Body.Close()
		content, err = io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("failed to read PAC body: %w", err)
		}
	} else {
		filePath := loc
		if u != nil && u.Scheme == "file" {
			filePath = strings.TrimPrefix(loc, "file://")
			filePath = strings.TrimPrefix(filePath, "/")
		}
		content, err = os.ReadFile(filePath)
		if err != nil {
			return nil, fmt.Errorf("failed to read PAC file from %s: %w", loc, err)
		}
	}

	return NewEvaluator(string(content))
}

func (e *PacEvaluator) registerBuiltins(vm *goja.Runtime) {
	// Netscape functions
	_ = vm.Set("isPlainHostName", isPlainHostName)
	_ = vm.Set("dnsDomainIs", dnsDomainIs)
	_ = vm.Set("localHostOrDomainIs", localHostOrDomainIs)
	_ = vm.Set("isResolvable", isResolvable)
	_ = vm.Set("isInNet", isInNet)
	_ = vm.Set("dnsResolve", dnsResolve)
	_ = vm.Set("myIpAddress", myIpAddress)
	_ = vm.Set("dnsDomainLevels", dnsDomainLevels)
	_ = vm.Set("shExpMatch", shExpMatch)
	_ = vm.Set("weekdayRange", weekdayRange)
	_ = vm.Set("dateRange", dateRange)
	_ = vm.Set("timeRange", timeRange)

	// Microsoft functions
	_ = vm.Set("dnsResolveEx", dnsResolveEx)
	_ = vm.Set("myIpAddressEx", myIpAddressEx)
	_ = vm.Set("isInNetEx", isInNetEx)
	_ = vm.Set("sortIpAddressList", sortIpAddressList)
	_ = vm.Set("getClientVersion", getClientVersion)
}

// FindProxyForURL evaluates the PAC script for a given URL and host.
// Returns an ordered list of viable ProxyCandidates.
func (e *PacEvaluator) FindProxyForURL(rawURL, host string) ([]ProxyCandidate, error) {
	vm := e.runtimePool.Get().(*goja.Runtime)
	defer e.runtimePool.Put(vm)

	var fnName string
	if _, ok := goja.AssertFunction(vm.Get("FindProxyForURLEx")); ok {
		fnName = "FindProxyForURLEx"
	} else if _, ok := goja.AssertFunction(vm.Get("FindProxyForURL")); ok {
		fnName = "FindProxyForURL"
	} else {
		return nil, errors.New("neither FindProxyForURL nor FindProxyForURLEx is defined in PAC script")
	}

	fn, _ := goja.AssertFunction(vm.Get(fnName))
	val, err := fn(goja.Undefined(), vm.ToValue(rawURL), vm.ToValue(host))
	if err != nil {
		return nil, fmt.Errorf("error executing %s: %w", fnName, err)
	}

	resStr := val.String()
	candidates := ParsePacResult(resStr)

	// Filter out blacklisted candidates unless all are blacklisted
	var nonBlacklisted []ProxyCandidate
	for _, c := range candidates {
		if c.Type == "DIRECT" || !e.blacklist.IsBlacklisted(c.Host, c.Port) {
			nonBlacklisted = append(nonBlacklisted, c)
		}
	}

	if len(nonBlacklisted) > 0 {
		return nonBlacklisted, nil
	}
	return candidates, nil
}

// Blacklist returns the blacklist manager.
func (e *PacEvaluator) Blacklist() *Blacklist {
	return e.blacklist
}

// SetBlacklistTTL updates the timeout duration for blacklisting proxies.
func (e *PacEvaluator) SetBlacklistTTL(ttl time.Duration) {
	if e.blacklist != nil {
		e.blacklist.SetTTL(ttl)
	}
}

// ParsePacResult parses return strings like:
// "PROXY proxy1.example.com:8080; SOCKS5 127.0.0.1:1080; DIRECT"
func ParsePacResult(res string) []ProxyCandidate {
	var candidates []ProxyCandidate
	parts := strings.Split(res, ";")
	for _, p := range parts {
		token := strings.TrimSpace(p)
		if token == "" {
			continue
		}
		fields := strings.Fields(token)
		if len(fields) == 0 {
			continue
		}
		ptype := strings.ToUpper(fields[0])
		if ptype == "DIRECT" {
			candidates = append(candidates, ProxyCandidate{Type: "DIRECT"})
			continue
		}

		if len(fields) >= 2 {
			endpoint := fields[1]
			host, portStr, err := splitHostPortDefault(endpoint, 8080)
			if err == nil {
				normType := "HTTP"
				switch ptype {
				case "PROXY", "HTTP", "HTTPS":
					normType = "HTTP"
				case "SOCKS", "SOCKS5":
					normType = "SOCKS5"
				case "SOCKS4":
					normType = "SOCKS4"
				}
				candidates = append(candidates, ProxyCandidate{
					Type: normType,
					Host: host,
					Port: portStr,
				})
			}
		}
	}

	if len(candidates) == 0 {
		candidates = append(candidates, ProxyCandidate{Type: "DIRECT"})
	}
	return candidates
}

func splitHostPortDefault(addr string, defaultPort int) (string, int, error) {
	if !strings.Contains(addr, ":") {
		return addr, defaultPort, nil
	}
	parts := strings.Split(addr, ":")
	port, err := strconv.Atoi(parts[1])
	if err != nil {
		return parts[0], defaultPort, nil
	}
	return parts[0], port, nil
}
