//go:build windows

package auth

import (
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/alexbrainman/sspi"
	"github.com/alexbrainman/sspi/negotiate"
	"github.com/alexbrainman/sspi/ntlm"
)

// SSPISession handles Windows Integrated Authentication (SSO) using current user credentials.
type SSPISession struct {
	targetSpn    string
	useNTLMOnly  bool
	cred         *sspi.Credentials
	negCtx       *negotiate.ClientContext
	ntlmCtx      *ntlm.ClientContext
	initialToken []byte
	scheme       string
}

// NewSSPISession creates a new SSPI session using current Windows user's logon credentials.
func NewSSPISession(proxyHost string, useNTLMOnly bool) (*SSPISession, error) {
	s := &SSPISession{
		useNTLMOnly: useNTLMOnly,
	}

	if proxyHost != "" {
		s.targetSpn = "HTTP/" + proxyHost
	}

	if useNTLMOnly {
		s.scheme = "NTLM"
		cred, err := ntlm.AcquireCurrentUserCredentials()
		if err != nil {
			return nil, fmt.Errorf("sspi: failed to acquire current user NTLM credentials: %w", err)
		}
		s.cred = cred

		ctx, token, err := ntlm.NewClientContext(cred)
		if err != nil {
			_ = cred.Release()
			return nil, fmt.Errorf("sspi: failed to initialize NTLM client context: %w", err)
		}
		s.ntlmCtx = ctx
		s.initialToken = token
	} else {
		s.scheme = "Negotiate"
		cred, err := negotiate.AcquireCurrentUserCredentials()
		if err != nil {
			return nil, fmt.Errorf("sspi: failed to acquire current user Negotiate credentials: %w", err)
		}
		s.cred = cred

		ctx, token, err := negotiate.NewClientContext(cred, s.targetSpn)
		if err != nil {
			_ = cred.Release()
			return nil, fmt.Errorf("sspi: failed to initialize Negotiate client context: %w", err)
		}
		s.negCtx = ctx
		s.initialToken = token
	}

	return s, nil
}

func (s *SSPISession) Scheme() string {
	return s.scheme
}

// InitialHeader returns the Type 1 / initial token for Negotiate or NTLM.
func (s *SSPISession) InitialHeader() (string, error) {
	return s.scheme + " " + base64.StdEncoding.EncodeToString(s.initialToken), nil
}

// ProcessChallenge processes the challenge returned by upstream proxy and produces response token.
func (s *SSPISession) ProcessChallenge(challengeHeader string) (string, error) {
	parts := strings.Fields(challengeHeader)
	var rawChallenge string
	if len(parts) >= 2 {
		rawChallenge = parts[1]
	} else if len(parts) == 1 {
		rawChallenge = parts[0]
	} else {
		return "", fmt.Errorf("sspi: empty challenge header")
	}

	challengeBytes, err := base64.StdEncoding.DecodeString(rawChallenge)
	if err != nil {
		return "", fmt.Errorf("sspi: failed to base64 decode challenge: %w", err)
	}

	if s.useNTLMOnly && s.ntlmCtx != nil {
		respToken, err := s.ntlmCtx.Update(challengeBytes)
		if err != nil {
			return "", fmt.Errorf("sspi: NTLM Update failed: %w", err)
		}
		return "NTLM " + base64.StdEncoding.EncodeToString(respToken), nil
	} else if s.negCtx != nil {
		_, respToken, err := s.negCtx.Update(challengeBytes)
		if err != nil {
			return "", fmt.Errorf("sspi: Negotiate Update failed: %w", err)
		}
		return "Negotiate " + base64.StdEncoding.EncodeToString(respToken), nil
	}

	return "", fmt.Errorf("sspi: context not initialized")
}

// Close releases native SSPI resources.
func (s *SSPISession) Close() error {
	if s.negCtx != nil {
		_ = s.negCtx.Release()
		s.negCtx = nil
	}
	if s.ntlmCtx != nil {
		_ = s.ntlmCtx.Release()
		s.ntlmCtx = nil
	}
	if s.cred != nil {
		_ = s.cred.Release()
		s.cred = nil
	}
	return nil
}
