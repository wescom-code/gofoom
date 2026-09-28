package auth

import (
	"strings"
	"testing"
)

func TestBasicSession(t *testing.T) {
	session := NewBasicSession("testuser", "secretpass")
	header, err := session.InitialHeader()
	if err != nil {
		t.Fatalf("Failed to generate basic header: %v", err)
	}

	if !strings.HasPrefix(header, "Basic ") {
		t.Errorf("Expected Basic prefix, got %s", header)
	}

	resp, err := session.ProcessChallenge("")
	if err != nil || resp != header {
		t.Errorf("Expected same token for basic challenge, got %s", resp)
	}
}

func TestNTLMSessionInitial(t *testing.T) {
	session := NewNTLMSession("CORP\\johndoe", "MyPassword123")
	header, err := session.InitialHeader()
	if err != nil {
		t.Fatalf("Failed to generate NTLM type 1 header: %v", err)
	}

	if !strings.HasPrefix(header, "NTLM ") {
		t.Errorf("Expected NTLM prefix, got %s", header)
	}
}
