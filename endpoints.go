package threads

const (
	threadsBaseURL = "https://www.threads.net"
	graphqlURL     = threadsBaseURL + "/api/graphql"
	igAppID        = "238260118697367"
)

// Doc IDs for Threads GraphQL persisted queries.
const (
	docIDUserProfile  = "23996318473300828"
	docIDUserThreads  = "6232751443445612"
	docIDUserReplies  = "6307072669391286"
	docIDSingleThread = "5587632691339264"
	docIDThreadLikers = "9360915773983802"
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
