package auth

import "errors"

// Common errors in authentication
var (
	ErrSSPINotSupported = errors.New("sspi is only supported on Windows")
	ErrAuthFailed       = errors.New("proxy authentication failed")
	ErrUnsupportedScheme = errors.New("unsupported authentication scheme")
)

// AuthScheme represents the HTTP proxy authentication scheme.
type AuthScheme string

const (
	SchemeAuto      AuthScheme = "AUTO"
	SchemeBasic     AuthScheme = "BASIC"
	SchemeNTLM      AuthScheme = "NTLM"
	SchemeNegotiate AuthScheme = "NEGOTIATE"
	SchemeKerberos  AuthScheme = "KERBEROS"
)

// Credentials contains user credentials or flags for proxy authentication.
type Credentials struct {
	Username              string
	Password              string
	Domain                string
	UseCurrentCredentials bool       // Windows SSO
	PreferredScheme       AuthScheme // AUTO, NTLM, BASIC, etc.
}

// Session tracks multi-step authentication state (e.g. NTLM / Negotiate handshakes).
type Session interface {
	// Scheme returns the active authentication scheme (e.g. "NTLM", "Negotiate", "Basic")
	Scheme() string
	// InitialHeader returns the header value for the first request (e.g. "NTLM <Type1>")
	InitialHeader() (string, error)
	// ProcessChallenge takes the challenge string from 407 Proxy-Authenticate and returns the response token
	ProcessChallenge(challenge string) (string, error)
	// Close releases any native context resources (e.g. SSPI handles)
	Close() error
}
