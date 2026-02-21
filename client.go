package threads

import (
	"context"
	"fmt"
	"log/slog"
	"regexp"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

const maxRetries = 3

// Client is the Threads scraping client.
type Client struct {
	bc  *stealth.BrowserClient
	cfg Config
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

	return &Client{bc: bc, cfg: cfg}, nil
}

// pageHeaders returns standard headers for fetching a Threads page.
var pageHeaders = map[string]string{
	"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
	"accept-language": "en-US,en;q=0.9",
	"sec-fetch-dest":  "document",
	"sec-fetch-mode":  "navigate",
	"sec-fetch-site":  "none",
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

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
