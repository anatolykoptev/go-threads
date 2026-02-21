package threads

import (
	"context"
	"fmt"
	"regexp"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

const lsdTTL = 30 * time.Minute

var lsdRe = regexp.MustCompile(`LSD",\[\],\{"token":"([^"]+)"\}`)

// ensureLSD returns a cached LSD token or fetches a new one.
func (c *Client) ensureLSD(ctx context.Context) (string, error) {
	c.lsdMu.Lock()
	defer c.lsdMu.Unlock()

	if c.lsd != "" && time.Since(c.lsdAt) < lsdTTL {
		return c.lsd, nil
	}

	token, err := fetchLSDToken(c.bc)
	if err != nil {
		return "", err
	}
	c.lsd = token
	c.lsdAt = time.Now()
	return token, nil
}

// fetchLSDToken fetches the LSD token from the Threads homepage HTML.
func fetchLSDToken(bc *stealth.BrowserClient) (string, error) {
	body, _, status, err := bc.DoWithHeaderOrder("GET", threadsBaseURL+"/@instagram", pageHeaders, nil, threadsHeaderOrder)
	if err != nil {
		return "", fmt.Errorf("fetch LSD page: %w", err)
	}
	if status != 200 {
		return "", fmt.Errorf("fetch LSD page: HTTP %d", status)
	}

	matches := lsdRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", fmt.Errorf("LSD token not found in page HTML")
	}
	return string(matches[1]), nil
}
