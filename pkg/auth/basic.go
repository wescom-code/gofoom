package auth

import "encoding/base64"

// BasicSession handles HTTP Basic authentication.
type BasicSession struct {
	username string
	password string
}

// NewBasicSession creates a new Basic authentication session.
func NewBasicSession(username, password string) *BasicSession {
	return &BasicSession{
		username: username,
		password: password,
	}
}

func (s *BasicSession) Scheme() string {
	return "Basic"
}

func (s *BasicSession) InitialHeader() (string, error) {
	raw := s.username + ":" + s.password
	return "Basic " + base64.StdEncoding.EncodeToString([]byte(raw)), nil
}

func (s *BasicSession) ProcessChallenge(challenge string) (string, error) {
	// Basic is single-step, re-return the initial token
	return s.InitialHeader()
}

func (s *BasicSession) Close() error {
	return nil
}
