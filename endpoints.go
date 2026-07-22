package threads

const (
	threadsBaseURL = "https://www.threads.com"
	igBaseURL      = "https://i.instagram.com"
	igWebBaseURL   = "https://www.instagram.com"
	igAppID        = "238260118697367" // Threads/Barcelona web app id
	igWebAppID     = "936619743392459" // Instagram web app id
	xAsbdID        = "359341"          // Threads GraphQL x-asbd-id value
	igWebXAsbdID   = "129477"          // Instagram web x-asbd-id value

	// Threads GraphQL doc IDs (www.threads.com/graphql/query)
	docIDUserProfile     = "23996318473300828"
	docIDUserThreads     = "6232751443445612"
	docIDUserReplies     = "6307072669391286"
	docIDSingleThread    = "5587632691339264"
	docIDGetThreadLikers = "9360915773983802"
	docIDSearchUsers     = "27238810212443285"

	// Private API paths (mobile / i.instagram.com)
	pathPublishText = "/api/v1/media/configure_text_only_post/"
	pathLike        = "/api/v1/media/%s_%s/like/"
	pathUnlike      = "/api/v1/media/%s_%s/unlike/"
	pathFollow      = "/api/v1/friendships/create/%s/"
	pathUnfollow    = "/api/v1/friendships/destroy/%s/"
	pathFollowers   = "/api/v1/friendships/%s/followers/"
	pathFollowing   = "/api/v1/friendships/%s/following/"
	pathSearchUser  = "/api/v1/users/search/"
	pathThreadByID  = "/api/v1/text_feed/%s/replies/"

	// Instagram web endpoints (same-origin from a www.instagram.com tab).
	// The exact web publish endpoint/doc_id is not yet confirmed — see doCDP.
	igWebLikePath     = "/api/v1/web/likes/%s/like/"
	igWebUnlikePath   = "/api/v1/web/likes/%s/unlike/"
	igWebFollowPath   = "/api/v1/web/friendships/%s/follow/"
	igWebUnfollowPath = "/api/v1/web/friendships/%s/unfollow/"

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
		"content-type":       "application/x-www-form-urlencoded",
		"origin":             threadsBaseURL,
		"referer":            threadsBaseURL + "/",
		"sec-fetch-dest":     "empty",
		"sec-fetch-mode":     "cors",
		"sec-fetch-site":     "same-origin",
		"x-asbd-id":          xAsbdID,
		"x-fb-friendly-name": friendlyName,
		"x-fb-lsd":           lsd,
		"x-ig-app-id":        igAppID,
	}
}
