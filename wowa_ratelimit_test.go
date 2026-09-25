package threads

import (
	"context"
	"testing"
	"time"

	"github.com/anatolykoptev/go-stealth/ratelimit"
)

// The CDP path must honor the shared domain limiter — without it, looped
// media_download calls hammer Instagram from the authenticated tab and the
// ban lands on the shared cookie jar (go-wowa#92).
func TestCDPThrottleEngages(t *testing.T) {
	c := &Client{limiter: ratelimit.NewDomainLimiter(ratelimit.DomainConfig{
		Domain:            "example.com",
		RequestsPerWindow: 10,
		WindowDuration:    time.Minute,
	})}
	url := "https://example.com/api/x"
	c.cdpMarkLimited(url) // simulate a 429 backoff

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()
	if err := c.cdpThrottle(ctx, url); err == nil {
		t.Fatal("throttled CDP request should fail when ctx expires")
	}
	if n := c.cdpThrottled.Load(); n != 1 {
		t.Fatalf("throttled_total = %d, want 1", n)
	}
}

// After the backoff elapses the same limiter admits again — the gate is a
// delay, not a permanent stop.
func TestCDPThrottleAdmitsAfterCooldown(t *testing.T) {
	c := &Client{limiter: ratelimit.NewDomainLimiter(ratelimit.DomainConfig{
		Domain:            "example.com",
		RequestsPerWindow: 10,
		WindowDuration:    time.Minute,
	})}
	if err := c.cdpThrottle(context.Background(), "https://example.com/api/x"); err != nil {
		t.Fatalf("fresh limiter should admit: %v", err)
	}
}

// A nil limiter (stealth fallback configs) must not gate at all.
func TestCDPThrottleNilLimiter(t *testing.T) {
	c := &Client{}
	if err := c.cdpThrottle(context.Background(), "https://example.com/"); err != nil {
		t.Fatalf("nil limiter should admit: %v", err)
	}
}
