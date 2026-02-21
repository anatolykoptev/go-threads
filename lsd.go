package threads

import (
	"fmt"
	"regexp"

	stealth "github.com/anatolykoptev/go-stealth"
)

var lsdRe = regexp.MustCompile(`LSD",\[\],\{"token":"([^"]+)"\}`)

// fetchLSDToken fetches the LSD token from the Threads homepage HTML.
func fetchLSDToken(bc *stealth.BrowserClient) (string, error) {
	headers := map[string]string{
		"accept":          "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"accept-language": "en-US,en;q=0.9",
		"sec-fetch-dest":  "document",
		"sec-fetch-mode":  "navigate",
		"sec-fetch-site":  "none",
	}
	body, _, status, err := bc.DoWithHeaderOrder("GET", threadsBaseURL+"/@instagram", headers, nil, threadsHeaderOrder)
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
