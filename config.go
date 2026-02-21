package threads

import (
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

// Config holds configuration for a Threads client.
type Config struct {
	ProxyPool          stealth.ProxyPoolProvider // optional proxy rotation
	Timeout            int                       // request timeout in seconds (default 15)
	MetricsHook        func(endpoint string, success bool)
	LSDRefreshInterval time.Duration // how often to refresh LSD token (default 30min)
}

func (c *Config) defaults() {
	if c.Timeout <= 0 {
		c.Timeout = 15
	}
	if c.LSDRefreshInterval <= 0 {
		c.LSDRefreshInterval = 30 * time.Minute
	}
}
