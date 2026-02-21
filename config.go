package threads

import (
	stealth "github.com/anatolykoptev/go-stealth"
)

// Config holds configuration for a Threads client.
type Config struct {
	ProxyPool   stealth.ProxyPoolProvider // optional proxy rotation
	Timeout     int                       // request timeout in seconds (default 15)
	MetricsHook func(endpoint string, success bool)

	// Auth fields (optional, enables Private API)
	Username string // Instagram username
	Password string // Instagram password
	Token    string // Pre-existing Bearer token (skips login)
}

func (c *Config) defaults() {
	if c.Timeout <= 0 {
		c.Timeout = 15
	}
}
