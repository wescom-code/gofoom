package sysproxy

// DetectedProxy represents the system-wide proxy configuration detected from the OS.
type DetectedProxy struct {
	Type   string // "DIRECT", "HTTP", "SOCKS5", "PAC"
	Host   string
	Port   int
	PacURL string
}
