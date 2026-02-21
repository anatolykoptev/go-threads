package threads

import (
	"fmt"
	"regexp"

	stealth "github.com/anatolykoptev/go-stealth"
)

var lsdRe = regexp.MustCompile(`LSD",\[\],\{"token":"([^"]+)"\}`)

// fetchLSDToken fetches the LSD token from the Threads homepage HTML.
// Currently unused — the SSR approach doesn't need it — but kept for
// potential future GraphQL API use if Meta re-enables unauthenticated access.
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
