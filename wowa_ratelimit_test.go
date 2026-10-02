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

// The limiter NewClient builds must gate every Meta host the client sends to.
// Its first rule once named www.threads.net while every request went to
// threadsBaseURL (www.threads.com), so Threads traffic on the shared burner
// session ran unthrottled. The tests above hand-build their own limiter and
// could not see that.
func TestNewClientLimiterGatesEveryMetaHost(t *testing.T) {
	for _, base := range []string{threadsBaseURL, igBaseURL, igWebBaseURL} {
		t.Run(base, func(t *testing.T) {
			c, err := NewClient(Config{})
			if err != nil {
				t.Fatal(err)
			}
			target := base + "/graphql/query"
			// An idle host admits at once and charges one slot. A second
			// Allow inside cdpThrottle would charge two and hold even the
			// first request for the rule's MinDelay.
			first, cancelFirst := context.WithTimeout(context.Background(), 500*time.Millisecond)
			defer cancelFirst()
			if err := c.cdpThrottle(first, target); err != nil {
				t.Fatalf("first request to an idle host should be admitted at once: %v", err)
			}
			// Every rule has a MinDelay of 2s or more, so an immediate second
			// request must be held until the context expires.
			second, cancelSecond := context.WithTimeout(context.Background(), 200*time.Millisecond)
			defer cancelSecond()
			if err := c.cdpThrottle(second, target); err == nil {
				t.Fatalf("second request to %s was admitted at once: no limiter rule matches its host", base)
			}
		})
	}
}

// A host with no rule is never held. An empty rule Domain would be a wildcard
// in go-stealth's matchRule and throttle every host, kkinstagram and CDN
// fetches included.
func TestNewClientLimiterLeavesUnruledHostsAlone(t *testing.T) {
	c, err := NewClient(Config{})
	if err != nil {
		t.Fatal(err)
	}
	target := kkInstagramBase + "/reel/x"
	for i := range 2 {
		if !c.limiter.Allow(target) {
			t.Fatalf("request %d to %s was held: a limiter rule matches a host it should not", i+1, kkInstagramBase)
		}
	}
}

// A stealth-transport request must not wait out a spent budget past the
// client's Timeout. BrowserClient.Do takes no context, and go-stealth's own
// RateLimitMiddleware waits on context.Background(), so such a request sat
// until the window reset.
func TestStealthRateLimitHoldIsBounded(t *testing.T) {
	c, err := NewClient(Config{Timeout: 1})
	if err != nil {
		t.Fatal(err)
	}
	target := threadsBaseURL + "/@instagram"
	// An hour-long backoff: the request must fail without reaching the network.
	c.limiter.MarkRateLimited(target, time.Now().Add(time.Hour))

	done := make(chan error, 1)
	go func() {
		_, _, _, err := c.bc.Do("GET", target, nil, nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("request through a spent budget returned no error")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stealth request held past the client Timeout: the limiter wait ignores every deadline")
	}
}
