package threads

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strconv"
	"time"
)

// --- Write operations ---

// PublishThread publishes a text-only thread post. Returns the media ID.
func (c *Client) PublishThread(ctx context.Context, text string) (string, error) {
	c.authMu.RLock()
	userID := c.userID
	c.authMu.RUnlock()

	form := url.Values{}
	form.Set("publish_mode", "text_post")
	form.Set("text_post_app_info", `{"reply_control":0}`)
	form.Set("timezone_offset", "0")
	form.Set("source_type", "4")
	form.Set("caption", text)
	form.Set("upload_id", strconv.FormatInt(time.Now().UnixMilli(), 10))
	form.Set("device_id", generateDeviceID())
	if userID != "" {
		form.Set("_uid", userID)
	}

	body, err := c.doPrivateAPI(ctx, "PublishThread", pathPublishText, form)
	if err != nil {
		return "", fmt.Errorf("PublishThread: %w", err)
	}

	var resp struct {
		Media struct {
			ID string `json:"id"`
			Pk int64  `json:"pk"`
		} `json:"media"`
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return "", fmt.Errorf("PublishThread: unmarshal: %w", err)
	}
	if resp.Status != "ok" {
		return "", fmt.Errorf("PublishThread: status=%q body=%s", resp.Status, truncateBytes(body, 300))
	}

	mediaID := resp.Media.ID
	if mediaID == "" {
		mediaID = strconv.FormatInt(resp.Media.Pk, 10)
	}
	return mediaID, nil
}

// LikeThread likes a thread post.
func (c *Client) LikeThread(ctx context.Context, threadID string) error {
	c.authMu.RLock()
	userID := c.userID
	c.authMu.RUnlock()

	path := fmt.Sprintf(pathLike, threadID, userID)
	form := url.Values{}
	form.Set("media_id", threadID)

	body, err := c.doPrivateAPI(ctx, "LikeThread", path, form)
	if err != nil {
		return fmt.Errorf("LikeThread: %w", err)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("LikeThread: unmarshal: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("LikeThread: status=%q body=%s", resp.Status, truncateBytes(body, 200))
	}
	return nil
}

// UnlikeThread unlikes a thread post.
func (c *Client) UnlikeThread(ctx context.Context, threadID string) error {
	c.authMu.RLock()
	userID := c.userID
	c.authMu.RUnlock()

	path := fmt.Sprintf(pathUnlike, threadID, userID)
	form := url.Values{}
	form.Set("media_id", threadID)

	body, err := c.doPrivateAPI(ctx, "UnlikeThread", path, form)
	if err != nil {
		return fmt.Errorf("UnlikeThread: %w", err)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("UnlikeThread: unmarshal: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("UnlikeThread: status=%q body=%s", resp.Status, truncateBytes(body, 200))
	}
	return nil
}

// Follow follows a user by their user ID.
func (c *Client) Follow(ctx context.Context, targetUserID string) error {
	path := fmt.Sprintf(pathFollow, targetUserID)
	form := url.Values{}
	form.Set("user_id", targetUserID)

	body, err := c.doPrivateAPI(ctx, "Follow", path, form)
	if err != nil {
		return fmt.Errorf("Follow: %w", err)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("Follow: unmarshal: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("Follow: status=%q body=%s", resp.Status, truncateBytes(body, 200))
	}
	return nil
}

// Unfollow unfollows a user by their user ID.
func (c *Client) Unfollow(ctx context.Context, targetUserID string) error {
	path := fmt.Sprintf(pathUnfollow, targetUserID)
	form := url.Values{}
	form.Set("user_id", targetUserID)

	body, err := c.doPrivateAPI(ctx, "Unfollow", path, form)
	if err != nil {
		return fmt.Errorf("Unfollow: %w", err)
	}

	var resp struct {
		Status string `json:"status"`
	}
	if err := json.Unmarshal(body, &resp); err != nil {
		return fmt.Errorf("Unfollow: unmarshal: %w", err)
	}
	if resp.Status != "ok" {
		return fmt.Errorf("Unfollow: status=%q body=%s", resp.Status, truncateBytes(body, 200))
	}
	return nil
}

// --- Read operations ---

// GetUserFollowers fetches followers of a user by their user ID.
func (c *Client) GetUserFollowers(ctx context.Context, userID string, count int) ([]*ThreadsUser, error) {
	path := fmt.Sprintf(pathFollowers, userID)
	params := url.Values{}
	if count > 0 {
		params.Set("count", strconv.Itoa(count))
	}

	body, err := c.doPrivateGET(ctx, "GetUserFollowers", path, params)
	if err != nil {
		return nil, fmt.Errorf("GetUserFollowers: %w", err)
	}

	users, _, err := parsePrivateUserList(body)
	if err != nil {
		return nil, fmt.Errorf("GetUserFollowers: %w", err)
	}
	if count > 0 && len(users) > count {
		users = users[:count]
	}
	return users, nil
}

// GetUserFollowing fetches users that a user follows by their user ID.
func (c *Client) GetUserFollowing(ctx context.Context, userID string, count int) ([]*ThreadsUser, error) {
	path := fmt.Sprintf(pathFollowing, userID)
	params := url.Values{}
	if count > 0 {
		params.Set("count", strconv.Itoa(count))
	}

	body, err := c.doPrivateGET(ctx, "GetUserFollowing", path, params)
	if err != nil {
		return nil, fmt.Errorf("GetUserFollowing: %w", err)
	}

	users, _, err := parsePrivateUserList(body)
	if err != nil {
		return nil, fmt.Errorf("GetUserFollowing: %w", err)
	}
	if count > 0 && len(users) > count {
		users = users[:count]
	}
	return users, nil
}

// SearchUserPrivate searches for users using the Private API (requires auth).
func (c *Client) SearchUserPrivate(ctx context.Context, query string) ([]*ThreadsUser, error) {
	params := url.Values{}
	params.Set("q", query)
	params.Set("count", "30")

	body, err := c.doPrivateGET(ctx, "SearchUserPrivate", pathSearchUser, params)
	if err != nil {
		return nil, fmt.Errorf("SearchUserPrivate: %w", err)
	}

	users, _, err := parsePrivateUserList(body)
	if err != nil {
		return nil, fmt.Errorf("SearchUserPrivate: %w", err)
	}
	return users, nil
}

// GetThreadByID fetches a thread and its replies by thread ID using the Private API.
func (c *Client) GetThreadByID(ctx context.Context, threadID string) (*Thread, []*Thread, error) {
	path := fmt.Sprintf(pathThreadByID, threadID)

	body, err := c.doPrivateGET(ctx, "GetThreadByID", path, nil)
	if err != nil {
		return nil, nil, fmt.Errorf("GetThreadByID: %w", err)
	}

	main, replies, err := parsePrivateThread(body)
	if err != nil {
		return nil, nil, fmt.Errorf("GetThreadByID: %w", err)
	}
	return main, replies, nil
}
