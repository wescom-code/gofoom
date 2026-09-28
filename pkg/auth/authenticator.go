package auth

import "strings"

// CreateSession instantiates an authentication session matching the configured credentials and schemes.
func CreateSession(cred Credentials, proxyHost string) (Session, error) {
	if cred.UseCurrentCredentials {
		useNTLM := strings.EqualFold(string(cred.PreferredScheme), string(SchemeNTLM))
		sspiSession, err := NewSSPISession(proxyHost, useNTLM)
		if err == nil {
			return sspiSession, nil
		}
		// If SSPI failed (e.g. not on Windows) and username is provided, fall back
		if cred.Username == "" {
			return nil, err
		}
	}

	if strings.EqualFold(string(cred.PreferredScheme), string(SchemeBasic)) {
		return NewBasicSession(cred.Username, cred.Password), nil
	}

	// Default to NTLM for enterprise proxies if not strictly Basic
	if cred.Username != "" {
		return NewNTLMSession(cred.Username, cred.Password), nil
	}

	return nil, ErrAuthFailed
}
