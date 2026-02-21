package threads

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
	"github.com/anatolykoptev/go-stealth/ratelimit"
)

const maxRetries = 3

// Client is the Threads scraping client.
type Client struct {
	bc  *stealth.BrowserClient
	cfg Config

	// LSD token state
	lsd   string
	csrf  string
	lsdMu sync.Mutex
	lsdAt time.Time

	// Auth state (Private API)
	token  string // "IGT:2:<token>"
	userID string // logged-in user ID
	authMu sync.RWMutex
}

// NewClient creates a new Threads client.
func NewClient(cfg Config) (*Client, error) {
	cfg.defaults()

	var opts []stealth.ClientOption
	if cfg.Timeout > 0 {
		opts = append(opts, stealth.WithTimeout(cfg.Timeout))
	}
	if cfg.ProxyPool != nil {
		opts = append(opts, stealth.WithProxyPool(cfg.ProxyPool))
	}
	opts = append(opts, stealth.WithFollowRedirects())
	opts = append(opts, stealth.WithHeaderOrder(threadsHeaderOrder))

	bc, err := stealth.NewClient(opts...)
	if err != nil {
		return nil, fmt.Errorf("stealth client: %w", err)
	}

	// Add go-stealth middleware
	limiter := ratelimit.NewDomainLimiter(
		ratelimit.DomainConfig{
			Domain:            "www.threads.net",
			RequestsPerWindow: 30,
			WindowDuration:    15 * time.Minute,
			MinDelay:          2 * time.Second,
		},
		ratelimit.DomainConfig{
			Domain:            "i.instagram.com",
			RequestsPerWindow: 20,
			WindowDuration:    15 * time.Minute,
			MinDelay:          3 * time.Second,
		},
	)
	bc.Use(
		stealth.RateLimitMiddleware(limiter),
		stealth.ClientHintsMiddleware,
	)

	c := &Client{bc: bc, cfg: cfg}
	if cfg.Token != "" {
		c.token = cfg.Token
	}
	if cfg.CSRFToken != "" {
		c.csrf = cfg.CSRFToken
	}
	return c, nil
}

// IsAuthenticated returns true if the client has a valid auth token.
func (c *Client) IsAuthenticated() bool {
	c.authMu.RLock()
	defer c.authMu.RUnlock()
	return c.token != ""
}

// pageHeaders returns standard headers for fetching a Threads page.
var pageHeaders = map[string]string{
	"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"accept-language": "en-US,en;q=0.9",
	"sec-fetch-dest":  "document",
	"sec-fetch-mode":  "navigate",
	"sec-fetch-site":  "none",
}

// isLoginRedirect checks if a 200 response body actually contains a login redirect.
func isLoginRedirect(body []byte) bool {
	return bytes.Contains(body, []byte("/accounts/login")) ||
		bytes.Contains(body, []byte(`"require_login":true`))
}

// fetchPage fetches a Threads page with retry and backoff.
func (c *Client) fetchPage(ctx context.Context, endpoint, pageURL string) ([]byte, error) {
	if err := stealth.DefaultJitter.Sleep(ctx); err != nil {
		return nil, err
	}

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := stealth.DefaultBackoff.Duration(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		body, _, status, err := c.bc.DoWithHeaderOrder("GET", pageURL, pageHeaders, nil, threadsHeaderOrder)
		if err != nil {
			lastErr = err
			continue
		}

		errClass := classifyHTTPStatus(status)
		switch errClass {
		case errNone:
			if isLoginRedirect(body) {
				lastErr = &APIError{Status: status, Class: errLoginRedirect, Message: "login redirect detected"}
				slog.Warn("threads: login redirect", slog.String("endpoint", endpoint), slog.Int("attempt", attempt+1))
				continue
			}
			c.recordMetrics(endpoint, true)
			return body, nil
		case errRateLimited:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "rate limited"}
			slog.Warn("threads: 429 rate limited", slog.String("endpoint", endpoint), slog.Int("attempt", attempt+1))
			continue
		case errForbidden:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "forbidden (likely IP ban)"}
			continue
		case errServerError:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "server error"}
			slog.Warn("threads: server error", slog.String("endpoint", endpoint), slog.Int("status", status))
			continue
		default:
			c.recordMetrics(endpoint, false)
			return nil, &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("HTTP %d", status)}
		}
	}

	if lastErr != nil {
		return nil, fmt.Errorf("%s failed after %d attempts: %w", endpoint, maxRetries, lastErr)
	}
	return nil, fmt.Errorf("%s failed after %d attempts", endpoint, maxRetries)
}

// recordMetrics calls the metrics hook if configured.
func (c *Client) recordMetrics(endpoint string, success bool) {
	if c.cfg.MetricsHook != nil {
		c.cfg.MetricsHook(endpoint, success)
	}
}

var userIDRe = regexp.MustCompile(`"user_id":"(\d+)"`)

// resolveUsername fetches the userID for a given username by scraping the profile page.
// Returns (userID, rawHTML, error) — HTML is returned for reuse by callers.
func (c *Client) resolveUsername(ctx context.Context, username string) (string, []byte, error) {
	profileURL := threadsBaseURL + "/@" + username
	body, err := c.fetchPage(ctx, "ResolveUsername", profileURL)
	if err != nil {
		return "", nil, fmt.Errorf("resolve username %q: %w", username, err)
	}

	matches := userIDRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", nil, fmt.Errorf("user_id not found in page for @%s", username)
	}
	return string(matches[1]), body, nil
}

// doGraphQL sends a GraphQL POST to the Threads API.
func (c *Client) doGraphQL(ctx context.Context, endpoint, docID string, variables map[string]any) ([]byte, error) {
	lsd, err := c.ensureLSD(ctx)
	if err != nil {
		return nil, fmt.Errorf("%s: LSD: %w", endpoint, err)
	}

	varsJSON, err := json.Marshal(variables)
	if err != nil {
		return nil, fmt.Errorf("%s: marshal variables: %w", endpoint, err)
	}

	form := url.Values{}
	form.Set("lsd", lsd)
	form.Set("doc_id", docID)
	form.Set("variables", string(varsJSON))
	bodyStr := form.Encode()

	headers := requestHeaders(lsd)
	if cookies := c.buildCookieHeader(); cookies != "" {
		headers["cookie"] = cookies
		if c.csrf != "" {
			headers["x-csrftoken"] = c.csrf
		}
	}
	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := stealth.DefaultBackoff.Duration(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		respBody, _, status, doErr := c.bc.DoWithHeaderOrder(
			"POST",
			threadsGQLBaseURL+"/api/graphql",
			headers,
			strings.NewReader(bodyStr),
			threadsHeaderOrder,
		)
		if doErr != nil {
			lastErr = doErr
			continue
		}

		errClass := classifyHTTPStatus(status)
		switch errClass {
		case errNone:
			c.recordMetrics(endpoint, true)
			return respBody, nil
		case errForbidden:
			// Clear LSD to force refresh on next attempt
			c.lsdMu.Lock()
			c.lsd = ""
			c.lsdMu.Unlock()
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "forbidden (stale LSD?)"}

			// Re-fetch LSD for retry
			newLSD, lsdErr := c.ensureLSD(ctx)
			if lsdErr == nil {
				headers = requestHeaders(newLSD)
				form.Set("lsd", newLSD)
				bodyStr = form.Encode()
			}
			continue
		case errRateLimited:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "rate limited"}
			continue
		case errServerError:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "server error"}
			continue
		default:
			c.recordMetrics(endpoint, false)
			return nil, &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("HTTP %d", status)}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("%s failed after %d attempts: %w", endpoint, maxRetries, lastErr)
	}
	return nil, fmt.Errorf("%s failed after %d attempts", endpoint, maxRetries)
}

// privateAPIHeaders returns headers for Instagram Private API requests.
func privateAPIHeaders(token string) map[string]string {
	return map[string]string{
		"Authorization":    "Bearer " + token,
		"User-Agent":       barcelonaUA,
		"Content-Type":     "application/x-www-form-urlencoded",
		"X-IG-App-ID":      igAppID,
		"Accept":           "*/*",
		"Accept-Language":  "en-US,en;q=0.9",
		"X-Bloks-Is-Layout-RTL":       "false",
		"X-Bloks-Version-Id":          "5f56efad68e1edec7801f630b5c122704ec5378adbee6609a448f105f34a9c73",
		"X-IG-WWW-Claim":              "0",
		"X-IG-Connection-Type":        "WIFI",
		"X-IG-Capabilities":           "3brTvx0=",
		"X-IG-App-Locale":             "en_US",
		"X-IG-Device-Locale":          "en_US",
		"X-IG-Mapped-Locale":          "en_US",
		"X-IG-Android-ID":             "android-1234567890",
		"X-IG-Connection-Speed":       "-1kbps",
		"X-IG-Bandwidth-Speed-KBPS":   "1000.000",
		"X-IG-Bandwidth-TotalBytes-B": "0",
		"X-IG-Bandwidth-TotalTime-MS": "0",
	}
}

// doPrivateAPI sends a POST to the Instagram Private API.
func (c *Client) doPrivateAPI(ctx context.Context, endpoint, path string, form url.Values) ([]byte, error) {
	c.authMu.RLock()
	token := c.token
	c.authMu.RUnlock()
	if token == "" {
		return nil, fmt.Errorf("%s: not authenticated", endpoint)
	}

	headers := privateAPIHeaders(token)
	signedBody := "SIGNATURE." + form.Encode()

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := stealth.DefaultBackoff.Duration(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		body, _, status, err := c.bc.Do("POST", igBaseURL+path, headers, strings.NewReader(signedBody))
		if err != nil {
			lastErr = err
			continue
		}

		errClass := classifyHTTPStatus(status)
		switch errClass {
		case errNone:
			c.recordMetrics(endpoint, true)
			return body, nil
		case errRateLimited:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "rate limited"}
			continue
		case errForbidden:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "forbidden"}
			continue
		case errServerError:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "server error"}
			continue
		default:
			c.recordMetrics(endpoint, false)
			return nil, &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("HTTP %d", status)}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("%s failed after %d attempts: %w", endpoint, maxRetries, lastErr)
	}
	return nil, fmt.Errorf("%s failed after %d attempts", endpoint, maxRetries)
}

// doPrivateGET sends a GET to the Instagram Private API.
func (c *Client) doPrivateGET(ctx context.Context, endpoint, path string, params url.Values) ([]byte, error) {
	c.authMu.RLock()
	token := c.token
	c.authMu.RUnlock()
	if token == "" {
		return nil, fmt.Errorf("%s: not authenticated", endpoint)
	}

	headers := privateAPIHeaders(token)
	fullURL := igBaseURL + path
	if len(params) > 0 {
		fullURL += "?" + params.Encode()
	}

	var lastErr error
	for attempt := range maxRetries {
		if attempt > 0 {
			delay := stealth.DefaultBackoff.Duration(attempt)
			select {
			case <-time.After(delay):
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}

		body, _, status, err := c.bc.Do("GET", fullURL, headers, nil)
		if err != nil {
			lastErr = err
			continue
		}

		errClass := classifyHTTPStatus(status)
		switch errClass {
		case errNone:
			c.recordMetrics(endpoint, true)
			return body, nil
		case errRateLimited:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "rate limited"}
			continue
		case errForbidden:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "forbidden"}
			continue
		case errServerError:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: "server error"}
			continue
		default:
			c.recordMetrics(endpoint, false)
			return nil, &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("HTTP %d", status)}
		}
	}
	if lastErr != nil {
		return nil, fmt.Errorf("%s failed after %d attempts: %w", endpoint, maxRetries, lastErr)
	}
	return nil, fmt.Errorf("%s failed after %d attempts", endpoint, maxRetries)
}

// buildCookieHeader constructs the Cookie header from all configured cookies.
func (c *Client) buildCookieHeader() string {
	var parts []string
	if c.cfg.SessionID != "" {
		parts = append(parts, "sessionid="+c.cfg.SessionID)
	}
	if c.cfg.DSUserID != "" {
		parts = append(parts, "ds_user_id="+c.cfg.DSUserID)
	}
	if c.csrf != "" {
		parts = append(parts, "csrftoken="+c.csrf)
	}
	if c.cfg.IGDID != "" {
		parts = append(parts, "ig_did="+c.cfg.IGDID)
	}
	if c.cfg.MID != "" {
		parts = append(parts, "mid="+c.cfg.MID)
	}
	if len(parts) == 0 {
		return ""
	}
	return strings.Join(parts, "; ")
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}

// Do exposes the underlying BrowserClient Do for use by auth flows.
func (c *Client) do(method, urlStr string, headers map[string]string, body io.Reader) ([]byte, map[string]string, int, error) {
	return c.bc.Do(method, urlStr, headers, body)
}
