package threads

const (
	threadsBaseURL    = "https://www.threads.net"
	threadsGQLBaseURL = "https://www.threads.com"
	igBaseURL         = "https://i.instagram.com"
	igAppID           = "238260118697367"

	// GraphQL doc IDs
	docIDGetThreadLikers = "9360915773983802"
	docIDSearchUsers     = "9509001572511267"

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
	"x-csrftoken",
	"x-fb-lsd",
	"x-ig-app-id",
}

// requestHeaders returns the standard headers for a Threads GraphQL POST.
func requestHeaders(lsd string) map[string]string {
	return map[string]string{
		"accept":          "*/*",
		"accept-language": "en-US,en;q=0.9",
		"content-type":    "application/x-www-form-urlencoded",
		"origin":          threadsGQLBaseURL,
		"referer":         threadsGQLBaseURL + "/",
		"sec-fetch-dest":  "empty",
		"sec-fetch-mode":  "cors",
		"sec-fetch-site":  "same-origin",
		"x-fb-lsd":        lsd,
		"x-ig-app-id":     igAppID,
	}
}
