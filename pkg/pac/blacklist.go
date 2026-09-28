package pac

import (
	"fmt"
	"sync"
	"time"
)

// Blacklist tracks temporarily unreachable upstream proxies.
type Blacklist struct {
	mu      sync.RWMutex
	entries map[string]time.Time
	ttl     time.Duration
}

// NewBlacklist creates a new Blacklist manager.
func NewBlacklist(ttl time.Duration) *Blacklist {
	if ttl <= 0 {
		ttl = 5 * time.Minute
	}
	return &Blacklist{
		entries: make(map[string]time.Time),
		ttl:     ttl,
	}
}

func key(host string, port int) string {
	return fmt.Sprintf("%s:%d", host, port)
}

// Add adds a proxy endpoint to the blacklist.
func (b *Blacklist) Add(host string, port int) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries[key(host, port)] = time.Now().Add(b.ttl)
}

// IsBlacklisted returns true if the endpoint is currently blacklisted.
func (b *Blacklist) IsBlacklisted(host string, port int) bool {
	b.mu.RLock()
	defer b.mu.RUnlock()
	expiry, exists := b.entries[key(host, port)]
	if !exists {
		return false
	}
	return time.Now().Before(expiry)
}

// Clear removes all blacklisted entries.
func (b *Blacklist) Clear() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.entries = make(map[string]time.Time)
}
