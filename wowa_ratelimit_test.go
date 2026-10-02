package threads

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
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
		// A network error would also be non-nil; only the limiter's own
		// deadline proves the request was held and then released.
		if !errors.Is(err, context.DeadlineExceeded) || !strings.Contains(err.Error(), "rate limit ") {
			t.Fatalf("want the limiter's bounded-hold error, got %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("stealth request held past the client Timeout: the limiter wait ignores every deadline")
	}
}

// A 429 on the stealth path must back the host off. go-stealth's middleware did
// this only when Retry-After was present; boundedRateLimit also covers a bare
// 429, so a lost backoff would let retries hammer the burner session silently.
func TestBoundedRateLimitBacksOffOn429(t *testing.T) {
	for _, tc := range []struct {
		name   string
		status int
		held   bool
	}{
		{"429 without Retry-After", http.StatusTooManyRequests, true},
		{"200", http.StatusOK, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			// No MinDelay, so only a backoff can hold the second request.
			limiter := ratelimit.NewDomainLimiter(ratelimit.DomainConfig{
				Domain:            "example.com",
				RequestsPerWindow: 10,
				WindowDuration:    time.Minute,
			})
			next := func(*stealth.Request) (*stealth.Response, error) {
				return &stealth.Response{StatusCode: tc.status, Headers: map[string]string{}}, nil
			}
			target := "https://example.com/api/x"
			if _, err := boundedRateLimit(limiter, time.Second)(next)(&stealth.Request{URL: target}); err != nil {
				t.Fatal(err)
			}
			if held := !limiter.Allow(target); held != tc.held {
				t.Fatalf("after a %d, next request held=%v, want %v", tc.status, held, tc.held)
			}
		})
	}
}

func TestRetryAfter(t *testing.T) {
	for _, tc := range []struct {
		header string
		want   time.Duration
	}{
		{"1", time.Second},
		{" 7 ", 7 * time.Second},
		{"", cdpRateLimitCooldown},
		{"-5", cdpRateLimitCooldown},
		{"Wed, 21 Oct 2026 07:28:00 GMT", cdpRateLimitCooldown},
	} {
		if got := retryAfter(tc.header); got != tc.want {
			t.Errorf("retryAfter(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}

// A CDP request must not wait out a spent budget until the caller's deadline.
// After a 429 backs www.instagram.com off for 5 minutes, an uncapped hold ate
// the caller's whole ctx on the CDP attempt and then on every fallback that
// shares the host (embed, SSR), so the embed tier never got to rescue a
// download (go-threads#60).
func TestCDPThrottleHoldIsBoundedByTimeout(t *testing.T) {
	c, err := NewClient(Config{Timeout: 1})
	if err != nil {
		t.Fatal(err)
	}
	target := igWebBaseURL + "/api/v1/media/1/info/"
	c.cdpMarkLimited(target)

	start := time.Now()
	done := make(chan error, 1)
	go func() { done <- c.cdpThrottle(context.Background(), target) }()
	select {
	case err := <-done:
		if !errors.Is(err, errRateLimitHold) || !errors.Is(err, context.DeadlineExceeded) {
			t.Fatalf("want the limiter's bounded-hold error, got %v", err)
		}
		// A cap that fails at once would starve the 2-3 s MinDelay holds.
		if held := time.Since(start); held < 900*time.Millisecond {
			t.Fatalf("hold gave up after %v, want about the 1s Timeout", held)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("CDP request held past the client Timeout: the hold is bounded only by the caller's ctx")
	}
}

// A limiter hold is not retried: the next attempt targets the same host and
// meets the same backoff, so fetchPage's retry loop would only multiply it.
func TestFetchPageDoesNotRetryAHold(t *testing.T) {
	// The throttle runs before any go-wowa call, so this URL is never dialled.
	c, err := NewClient(Config{Timeout: 1, WowaURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	c.cdpMarkLimited(igWebBaseURL + "/")

	_, err = c.fetchPage(context.Background(), "GetInstagramEmbed", igWebBaseURL+"/reel/x/embed/")
	if !errors.Is(err, errRateLimitHold) {
		t.Fatalf("want a limiter-hold error, got %v", err)
	}
	if n := c.cdpThrottled.Load(); n != 1 {
		t.Fatalf("request held %d times, want 1: a hold was retried", n)
	}
}

// After the CDP attempt is held on www.instagram.com, GetInstagramPost goes
// straight to the proxy tier: embed and SSR fetch the same host and would each
// wait out a full hold behind the same backoff (go-threads#60).
func TestGetInstagramPostSkipsHeldHostTiers(t *testing.T) {
	wowa, _, _ := cdpTestServer(t, "/api/v1/chrome/interact", `{"status":200,"body":"{}"}`)
	defer wowa.Close()
	var proxyHits atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		proxyHits.Add(1)
		http.NotFound(w, nil)
	}))
	defer proxy.Close()
	orig := kkInstagramBase
	kkInstagramBase = proxy.URL
	t.Cleanup(func() { kkInstagramBase = orig })

	c, err := NewClient(Config{Timeout: 1, WowaURL: wowa.URL})
	if err != nil {
		t.Fatal(err)
	}
	c.cdpMarkLimited(igWebBaseURL + "/")

	_, err = c.GetInstagramPost(context.Background(), "DbuWxrevxiy")
	if !errors.Is(err, errRateLimitHold) {
		t.Fatalf("want the CDP hold surfaced, got %v", err)
	}
	if n := c.cdpThrottled.Load(); n != 1 {
		t.Fatalf("www.instagram.com held %d times, want 1: a same-host tier ran after the hold", n)
	}
	if proxyHits.Load() != 1 {
		t.Fatalf("proxy tier hit %d times, want 1", proxyHits.Load())
	}
}

// doGraphQL is the other CDP retry loop: a held Threads GraphQL request must
// come back after one hold, not three.
func TestDoGraphQLDoesNotRetryAHold(t *testing.T) {
	c, err := NewClient(Config{Timeout: 1, WowaURL: "http://127.0.0.1:9"})
	if err != nil {
		t.Fatal(err)
	}
	c.lsd, c.lsdAt = "cached", time.Now() // skip the LSD page fetch
	c.cdpMarkLimited(threadsBaseURL + "/graphql/query")

	_, err = c.doGraphQL(context.Background(), "GetThreadLikers", docIDGetThreadLikers, "x", map[string]any{})
	if !errors.Is(err, errRateLimitHold) {
		t.Fatalf("want a limiter-hold error, got %v", err)
	}
	if n := c.cdpThrottled.Load(); n != 1 {
		t.Fatalf("request held %d times, want 1: a hold was retried", n)
	}
}

// The stealth-path loops must not retry a hold either: three holds plus
// backoff would take about 6s against one 1s hold.
func TestDoPrivateGETDoesNotRetryAHold(t *testing.T) {
	c, err := NewClient(Config{Timeout: 1, Token: "t"})
	if err != nil {
		t.Fatal(err)
	}
	c.limiter.MarkRateLimited(igBaseURL+"/", time.Now().Add(time.Hour))

	start := time.Now()
	_, err = c.doPrivateGET(context.Background(), "GetUserFollowers", "/api/v1/friendships/1/followers/", nil)
	if !errors.Is(err, errRateLimitHold) {
		t.Fatalf("want a limiter-hold error, got %v", err)
	}
	if took := time.Since(start); took > 2500*time.Millisecond {
		t.Fatalf("returned after %v: a hold was retried", took)
	}
}
