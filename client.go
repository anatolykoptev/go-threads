package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"regexp"
	"strings"
	"sync"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

const maxRetries = 3

// Client is the Threads scraping client.
type Client struct {
	bc    *stealth.BrowserClient
	lsd   string
	lsdMu sync.RWMutex
	lsdAt time.Time
	cfg   Config
}

// NewClient creates a new Threads client.
// Fetches an initial LSD token before returning.
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

	c := &Client{
		bc:  bc,
		cfg: cfg,
	}

	if err := c.refreshLSD(); err != nil {
		return nil, fmt.Errorf("initial LSD token: %w", err)
	}

	return c, nil
}

// refreshLSD fetches a fresh LSD token.
func (c *Client) refreshLSD() error {
	token, err := fetchLSDToken(c.bc)
	if err != nil {
		return err
	}
	c.lsdMu.Lock()
	c.lsd = token
	c.lsdAt = time.Now()
	c.lsdMu.Unlock()
	return nil
}

// ensureLSD refreshes the LSD token if it's stale.
func (c *Client) ensureLSD() error {
	c.lsdMu.RLock()
	stale := c.lsd == "" || time.Since(c.lsdAt) > c.cfg.LSDRefreshInterval
	c.lsdMu.RUnlock()
	if stale {
		return c.refreshLSD()
	}
	return nil
}

// getLSD returns the current LSD token.
func (c *Client) getLSD() string {
	c.lsdMu.RLock()
	defer c.lsdMu.RUnlock()
	return c.lsd
}

// doPost executes a POST to the Threads GraphQL API with retry and error recovery.
func (c *Client) doPost(ctx context.Context, endpoint, docID string, variables map[string]any) ([]byte, error) {
	if err := stealth.DefaultJitter.Sleep(ctx); err != nil {
		return nil, err
	}

	if err := c.ensureLSD(); err != nil {
		return nil, fmt.Errorf("ensure LSD: %w", err)
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

		varsJSON, err := json.Marshal(variables)
		if err != nil {
			return nil, fmt.Errorf("marshal variables: %w", err)
		}

		lsd := c.getLSD()
		form := url.Values{}
		form.Set("lsd", lsd)
		form.Set("doc_id", docID)
		form.Set("variables", string(varsJSON))

		headers := requestHeaders(lsd)
		body, _, status, err := c.bc.DoWithHeaderOrder(
			"POST", graphqlURL, headers,
			strings.NewReader(form.Encode()),
			threadsHeaderOrder,
		)
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
			// Refresh LSD and retry once
			slog.Warn("threads: 403, refreshing LSD", slog.String("endpoint", endpoint))
			if refreshErr := c.refreshLSD(); refreshErr != nil {
				return nil, &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("403 + LSD refresh failed: %v", refreshErr)}
			}

			// Retry with fresh LSD
			lsd = c.getLSD()
			form.Set("lsd", lsd)
			headers = requestHeaders(lsd)
			body2, _, status2, err2 := c.bc.DoWithHeaderOrder(
				"POST", graphqlURL, headers,
				strings.NewReader(form.Encode()),
				threadsHeaderOrder,
			)
			if err2 != nil {
				return nil, fmt.Errorf("403 retry: %w", err2)
			}
			if classifyHTTPStatus(status2) != errNone {
				return nil, &APIError{Status: status2, Class: classifyHTTPStatus(status2), Message: "403 retry failed (likely IP ban)"}
			}
			c.recordMetrics(endpoint, true)
			return body2, nil

		case errServerError:
			c.recordMetrics(endpoint, false)
			lastErr = &APIError{Status: status, Class: errClass, Message: fmt.Sprintf("server error: %s", truncateBytes(body, 200))}
			slog.Warn("threads: server error", slog.String("endpoint", endpoint), slog.Int("status", status))
			continue

		default:
			c.recordMetrics(endpoint, false)
			return nil, &APIError{Status: status, Class: errClass, Message: truncateBytes(body, 200)}
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
func (c *Client) resolveUsername(ctx context.Context, username string) (string, error) {
	if err := stealth.DefaultJitter.Sleep(ctx); err != nil {
		return "", err
	}

	profileURL := threadsBaseURL + "/@" + username
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"accept-language": "en-US,en;q=0.9",
		"sec-fetch-dest":  "document",
		"sec-fetch-mode":  "navigate",
		"sec-fetch-site":  "none",
	}

	body, _, status, err := c.bc.DoWithHeaderOrder("GET", profileURL, headers, nil, threadsHeaderOrder)
	if err != nil {
		return "", fmt.Errorf("resolve username %q: %w", username, err)
	}
	if status != 200 {
		return "", fmt.Errorf("resolve username %q: HTTP %d", username, status)
	}

	matches := userIDRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("user_id not found in page for @%s", username)
	}
	return string(matches[1]), nil
}

func truncateBytes(b []byte, n int) string {
	if len(b) <= n {
		return string(b)
	}
	return string(b[:n]) + "..."
}
