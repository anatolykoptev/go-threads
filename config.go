package threads

import (
	stealth "github.com/anatolykoptev/go-stealth"
)

// Config holds configuration for a Threads client.
type Config struct {
	ProxyPool   stealth.ProxyPoolProvider // optional proxy rotation
	Timeout     int                       // request timeout in seconds (default 15)
	MetricsHook func(endpoint string, success bool)
}

func (c *Config) defaults() {
	if c.Timeout <= 0 {
		c.Timeout = 15
	}
}
