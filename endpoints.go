package threads

const (
	threadsBaseURL = "https://www.threads.net"
	igAppID        = "238260118697367"
)

// threadsHeaderOrder is the header order for Threads requests.
var threadsHeaderOrder = []string{
	"accept",
	"accept-language",
	"content-type",
	"origin",
	"referer",
	"sec-fetch-dest",
	"sec-fetch-mode",
	"sec-fetch-site",
	"user-agent",
	"x-fb-lsd",
	"x-ig-app-id",
}

// requestHeaders returns the standard headers for a Threads GraphQL POST.
// Kept for potential future authenticated GraphQL API use.
func requestHeaders(lsd string) map[string]string {
	return map[string]string{
		"accept":          "*/*",
		"accept-language": "en-US,en;q=0.9",
		"content-type":    "application/x-www-form-urlencoded",
		"origin":          threadsBaseURL,
		"referer":         threadsBaseURL + "/",
		"sec-fetch-dest":  "empty",
		"sec-fetch-mode":  "cors",
		"sec-fetch-site":  "same-origin",
		"x-fb-lsd":        lsd,
		"x-ig-app-id":     igAppID,
	}
}
