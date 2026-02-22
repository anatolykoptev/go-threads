package threads

const (
	threadsBaseURL = "https://www.threads.net"
	igBaseURL      = "https://i.instagram.com"
	igWebBaseURL   = "https://www.instagram.com"
	igAppID        = "238260118697367"

	// GraphQL doc IDs
	docIDGetThreadLikers = "9360915773983802"

	// Private API paths
	pathPublishText = "/api/v1/media/configure_text_only_post/"
	pathLike        = "/api/v1/media/%s_%s/like/"
	pathUnlike      = "/api/v1/media/%s_%s/unlike/"
	pathFollow      = "/api/v1/friendships/create/%s/"
	pathUnfollow    = "/api/v1/friendships/destroy/%s/"
	pathFollowers   = "/api/v1/friendships/%s/followers/"
	pathFollowing   = "/api/v1/friendships/%s/following/"
	pathSearchUser  = "/api/v1/users/search/"
	pathThreadByID  = "/api/v1/text_feed/%s/replies/"

	// Private API auth
	pathEncryptionSync = "/api/v1/qe/sync/"
	pathBloksLogin     = "/api/v1/bloks/apps/com.bloks.www.bloks.caa.login.async.send_login_request/"

	barcelonaUA = "Barcelona 289.0.0.77.109 Android"
)

// threadsHeaderOrder is the header order for Threads requests.
var threadsHeaderOrder = []string{
	"accept",
	"accept-language",
	"content-type",
	"cookie",
	"origin",
	"referer",
	"sec-fetch-dest",
	"sec-fetch-mode",
	"sec-fetch-site",
	"user-agent",
	"x-asbd-id",
	"x-csrftoken",
	"x-fb-friendly-name",
	"x-fb-lsd",
	"x-ig-app-id",
}

// requestHeaders returns the standard headers for a Threads GraphQL POST.
func requestHeaders(lsd, friendlyName string) map[string]string {
	return map[string]string{
		"accept":             "*/*",
		"accept-language":    "en-US,en;q=0.9",
		"content-type":      "application/x-www-form-urlencoded",
		"origin":            threadsBaseURL,
		"referer":           threadsBaseURL + "/",
		"sec-fetch-dest":    "empty",
		"sec-fetch-mode":    "cors",
		"sec-fetch-site":    "same-origin",
		"x-asbd-id":         "129477",
		"x-fb-friendly-name": friendlyName,
		"x-fb-lsd":          lsd,
		"x-ig-app-id":       igAppID,
	}
}
