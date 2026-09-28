package pac

import (
	"testing"
)

const samplePacScript = `
function FindProxyForURL(url, host) {
    if (isPlainHostName(host) || dnsDomainIs(host, ".local")) {
        return "DIRECT";
    }
    if (shExpMatch(host, "*.internal.corp")) {
        return "PROXY 10.0.0.1:8080; PROXY 10.0.0.2:8080; DIRECT";
    }
    if (isInNet(host, "192.168.0.0", "255.255.0.0")) {
        return "SOCKS5 192.168.1.1:1080";
    }
    return "PROXY corp-proxy.example.com:3128; DIRECT";
}
`

func TestPacEvaluation(t *testing.T) {
	eval, err := NewEvaluator(samplePacScript)
	if err != nil {
		t.Fatalf("Failed to compile PAC script: %v", err)
	}

	// 1. Plain hostname test -> DIRECT
	cands, err := eval.FindProxyForURL("http://localhost/test", "localhost")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if len(cands) != 1 || cands[0].Type != "DIRECT" {
		t.Errorf("Expected DIRECT for localhost, got %+v", cands)
	}

	// 2. Wildcard test -> Fallback chain
	cands, err = eval.FindProxyForURL("https://dev.internal.corp/api", "dev.internal.corp")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if len(cands) != 3 {
		t.Fatalf("Expected 3 candidates, got %d", len(cands))
	}
	if cands[0].Host != "10.0.0.1" || cands[0].Port != 8080 {
		t.Errorf("Expected 10.0.0.1:8080, got %s:%d", cands[0].Host, cands[0].Port)
	}

	// 3. Blacklist first proxy -> Fallback to second
	eval.Blacklist().Add("10.0.0.1", 8080)
	cands, err = eval.FindProxyForURL("https://dev.internal.corp/api", "dev.internal.corp")
	if err != nil {
		t.Fatalf("Failed to evaluate after blacklist: %v", err)
	}
	if len(cands) != 2 || cands[0].Host != "10.0.0.2" {
		t.Errorf("Expected fallback to 10.0.0.2, got %+v", cands)
	}

	// 4. Default proxy test
	cands, err = eval.FindProxyForURL("https://google.com", "google.com")
	if err != nil {
		t.Fatalf("Failed to evaluate: %v", err)
	}
	if len(cands) != 2 || cands[0].Host != "corp-proxy.example.com" || cands[0].Port != 3128 {
		t.Errorf("Expected corp-proxy.example.com:3128, got %+v", cands)
	}
}

func TestBuiltins(t *testing.T) {
	if !isPlainHostName("myhost") {
		t.Errorf("Expected true for plain hostname")
	}
	if isPlainHostName("myhost.domain.com") {
		t.Errorf("Expected false for domain hostname")
	}
	if !dnsDomainIs("foo.example.com", ".example.com") {
		t.Errorf("Expected true for dnsDomainIs")
	}
	if !shExpMatch("foo.bar.baz", "*.bar.*") {
		t.Errorf("Expected true for shExpMatch")
	}
	if !isInNet("192.168.1.50", "192.168.1.0", "255.255.255.0") {
		t.Errorf("Expected true for isInNet")
	}
	if isInNet("10.0.0.1", "192.168.1.0", "255.255.255.0") {
		t.Errorf("Expected false for isInNet")
	}
	if !isInNetEx("192.168.1.50", "192.168.1.0/24") {
		t.Errorf("Expected true for isInNetEx CIDR")
	}
}
