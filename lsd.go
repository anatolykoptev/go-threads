package threads

import (
	"context"
	"fmt"
	"regexp"
	"strconv"
	"time"

	stealth "github.com/anatolykoptev/go-stealth"
)

const lsdTTL = 30 * time.Minute

var lsdRe = regexp.MustCompile(`LSD",\[\],\{"token":"([^"]+)"\}`)

// ensureLSD returns a cached LSD token or fetches a new one.
// Also captures csrftoken and fb_dtsg from response for GraphQL auth.
func (c *Client) ensureLSD(ctx context.Context) (string, error) {
	c.lsdMu.Lock()
	defer c.lsdMu.Unlock()

	if c.lsd != "" && time.Since(c.lsdAt) < lsdTTL {
		return c.lsd, nil
	}

	token, csrf, fbDtsg, err := fetchLSDToken(c.bc)
	if err != nil {
		return "", err
	}
	c.lsd = token
	if csrf != "" {
		c.csrf = csrf
	}
	c.fbDtsg = fbDtsg
	c.lsdAt = time.Now()
	return token, nil
}

var csrfRe = regexp.MustCompile(`csrftoken=([^;]+)`)
var fbDtsgRe = regexp.MustCompile(`"DTSGInitialData",\[\],\{"token":"([^"]+)"`)

// fetchLSDToken fetches the LSD token, csrftoken, and fb_dtsg from a Threads page.
func fetchLSDToken(bc *stealth.BrowserClient) (lsd string, csrf string, fbDtsg string, err error) {
	body, respHeaders, status, reqErr := bc.DoWithHeaderOrder("GET", threadsBaseURL+"/@instagram", pageHeaders, nil, threadsHeaderOrder)
	if reqErr != nil {
		return "", "", "", fmt.Errorf("fetch LSD page: %w", reqErr)
	}
	if status != 200 {
		return "", "", "", fmt.Errorf("fetch LSD page: HTTP %d", status)
	}

	matches := lsdRe.FindSubmatch(body)
	if len(matches) < 2 {
		return "", "", "", fmt.Errorf("LSD token not found in page HTML")
	}
	lsd = string(matches[1])

	// Extract csrftoken from Set-Cookie header
	for _, key := range []string{"set-cookie", "Set-Cookie"} {
		if cookie, ok := respHeaders[key]; ok {
			if m := csrfRe.FindStringSubmatch(cookie); len(m) >= 2 {
				csrf = m[1]
				break
			}
		}
	}

	// Extract fb_dtsg (non-fatal if empty — DTSGInitialData is empty without login)
	if m := fbDtsgRe.FindSubmatch(body); len(m) >= 2 {
		fbDtsg = string(m[1])
	}

	return lsd, csrf, fbDtsg, nil
}

// computeJazoest computes the jazoest parameter from fb_dtsg.
func computeJazoest(fbDtsg string) string {
	sum := 0
	for _, c := range fbDtsg {
		sum += int(c)
	}
	return "2" + strconv.Itoa(sum)
}
