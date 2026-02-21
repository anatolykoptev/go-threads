package threads

import (
	"context"
	"fmt"
)

// GetUser fetches a user profile by username (resolves to userID internally).
func (c *Client) GetUser(ctx context.Context, username string) (*ThreadsUser, error) {
	userID, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("GetUser: %w", err)
	}
	return c.GetUserByID(ctx, userID)
}

// GetUserByID fetches a user profile by numeric userID.
func (c *Client) GetUserByID(ctx context.Context, userID string) (*ThreadsUser, error) {
	variables := map[string]any{"userID": userID}
	body, err := c.doPost(ctx, "GetUser", docIDUserProfile, variables)
	if err != nil {
		return nil, fmt.Errorf("GetUserByID: %w", err)
	}
	return parseUser(body)
}

// GetUserThreads fetches recent threads by username.
func (c *Client) GetUserThreads(ctx context.Context, username string, count int) ([]*Thread, error) {
	userID, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("GetUserThreads: %w", err)
	}
	return c.GetUserThreadsByID(ctx, userID, count)
}

// GetUserThreadsByID fetches recent threads by numeric userID.
func (c *Client) GetUserThreadsByID(ctx context.Context, userID string, count int) ([]*Thread, error) {
	variables := map[string]any{"userID": userID}
	body, err := c.doPost(ctx, "GetUserThreads", docIDUserThreads, variables)
	if err != nil {
		return nil, fmt.Errorf("GetUserThreadsByID: %w", err)
	}
	threads, err := parseUserThreads(body)
	if err != nil {
		return nil, err
	}
	if count > 0 && len(threads) > count {
		threads = threads[:count]
	}
	return threads, nil
}

// GetUserReplies fetches recent replies by username.
func (c *Client) GetUserReplies(ctx context.Context, username string, count int) ([]*Thread, error) {
	userID, err := c.resolveUsername(ctx, username)
	if err != nil {
		return nil, fmt.Errorf("GetUserReplies: %w", err)
	}
	variables := map[string]any{"userID": userID}
	body, err := c.doPost(ctx, "GetUserReplies", docIDUserReplies, variables)
	if err != nil {
		return nil, fmt.Errorf("GetUserReplies: %w", err)
	}
	threads, err := parseUserThreads(body)
	if err != nil {
		return nil, err
	}
	if count > 0 && len(threads) > count {
		threads = threads[:count]
	}
	return threads, nil
}

// GetThread fetches a single thread and its replies.
// Returns (original thread, replies, error).
func (c *Client) GetThread(ctx context.Context, postID string) (*Thread, []*Thread, error) {
	variables := map[string]any{"postID": postID}
	body, err := c.doPost(ctx, "GetThread", docIDSingleThread, variables)
	if err != nil {
		return nil, nil, fmt.Errorf("GetThread: %w", err)
	}
	return parseThread(body)
}

// GetThreadLikers fetches users who liked a thread.
func (c *Client) GetThreadLikers(ctx context.Context, mediaID string, count int) ([]*ThreadsUser, error) {
	variables := map[string]any{"mediaID": mediaID}
	body, err := c.doPost(ctx, "GetThreadLikers", docIDThreadLikers, variables)
	if err != nil {
		return nil, fmt.Errorf("GetThreadLikers: %w", err)
	}
	users, err := parseLikers(body)
	if err != nil {
		return nil, err
	}
	if count > 0 && len(users) > count {
		users = users[:count]
	}
	return users, nil
}
