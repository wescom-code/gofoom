//go:build !windows

package auth

// SSPISession stub for non-Windows platforms.
type SSPISession struct{}

// NewSSPISession returns an error on non-Windows platforms.
func NewSSPISession(proxyHost string, useNTLMOnly bool) (*SSPISession, error) {
	return nil, ErrSSPINotSupported
}

func (s *SSPISession) Scheme() string {
	return "None"
}

func (s *SSPISession) InitialHeader() (string, error) {
	return "", ErrSSPINotSupported
}

func (s *SSPISession) ProcessChallenge(challenge string) (string, error) {
	return "", ErrSSPINotSupported
}

func (s *SSPISession) Close() error {
	return nil
}
