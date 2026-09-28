package auth

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/Azure/go-ntlmssp"
)

// NTLMSession handles cross-platform NTLMv2 challenge-response authentication.
type NTLMSession struct {
	username     string
	password     string
	domain       string
	domainNeeded bool
}

// NewNTLMSession creates a new NTLM authentication session.
func NewNTLMSession(rawUsername, password string) *NTLMSession {
	user, domain, domainNeeded := ntlmssp.GetDomain(rawUsername)
	return &NTLMSession{
		username:     user,
		password:     password,
		domain:       domain,
		domainNeeded: domainNeeded,
	}
}

func (s *NTLMSession) Scheme() string {
	return "NTLM"
}

// InitialHeader generates NTLM Type 1 Negotiate message.
func (s *NTLMSession) InitialHeader() (string, error) {
	negotiateBytes, err := ntlmssp.NewNegotiateMessage(s.domain, "")
	if err != nil {
		return "", fmt.Errorf("failed to create NTLM negotiate message: %w", err)
	}
	return "NTLM " + base64.StdEncoding.EncodeToString(negotiateBytes), nil
}

// ProcessChallenge processes NTLM Type 2 Challenge message and returns Type 3 Authenticate message.
func (s *NTLMSession) ProcessChallenge(challengeHeader string) (string, error) {
	// Challenge header format: "NTLM <base64>"
	parts := strings.Fields(challengeHeader)
	var rawChallenge string
	if len(parts) >= 2 {
		rawChallenge = parts[1]
	} else if len(parts) == 1 && !strings.EqualFold(parts[0], "NTLM") {
		rawChallenge = parts[0]
	} else {
		return "", fmt.Errorf("invalid NTLM challenge header: %q", challengeHeader)
	}

	challengeBytes, err := base64.StdEncoding.DecodeString(rawChallenge)
	if err != nil {
		return "", fmt.Errorf("failed to base64 decode NTLM challenge: %w", err)
	}

	authBytes, err := ntlmssp.ProcessChallenge(challengeBytes, s.username, s.password, s.domainNeeded)
	if err != nil {
		return "", fmt.Errorf("failed to process NTLM challenge: %w", err)
	}

	return "NTLM " + base64.StdEncoding.EncodeToString(authBytes), nil
}

func (s *NTLMSession) Close() error {
	return nil
}
