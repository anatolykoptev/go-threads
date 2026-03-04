# go-threads

[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](go.mod)
[![License: MIT](https://img.shields.io/badge/License-MIT-yellow.svg)](LICENSE)

Threads.net (Meta) scraping library for Go — public GraphQL API + Instagram Private API, LSD token auth, SSR parsing, and stealth TLS fingerprinting.

Built on [go-stealth](https://github.com/anatolykoptev/go-stealth) for TLS fingerprinting and rate limiting.

## Features

- **Public API** — user profiles, threads, replies, likers (no auth required)
- **Private API** — publish, like, follow, search (Instagram credentials)
- **LSD Token** — auto-fetched CSRF token with 30-min TTL cache
- **SSR Parsing** — extracts pre-rendered data from HTML (no JS execution)
- **Instagram Login** — RSA + AES-GCM password encryption (mimics official client)
- **Instagram Posts** — 3-method fallback (proxy, embed, SSR)
- **Rate Limiting** — built-in per-domain limits via go-stealth middleware

## Install

```bash
go get github.com/anatolykoptev/go-threads
```

## Quick Start

```go
client, _ := threads.NewClient(threads.Config{Timeout: 30})

// Public (anonymous)
user, _ := client.GetUser(ctx, "zuck")
posts, _ := client.GetUserThreads(ctx, "zuck", 10)
thread, replies, _ := client.GetThread(ctx, "zuck", "post_code")

// Authenticated
client, _ := threads.NewClient(threads.Config{
    Username: "myuser",
    Password: "mypass",
})
client.Login(ctx)
mediaID, _ := client.PublishThread(ctx, "Hello Threads!")
client.LikeThread(ctx, "thread_id")
client.Follow(ctx, "user_id")
```

## API

### Public (Anonymous)

| Method | Description |
|--------|-------------|
| `GetUser` | User profile by username |
| `GetUserThreads` | User's threads with pagination |
| `GetUserReplies` | User's replies |
| `GetThread` | Single thread + reply tree by post code |
| `GetThreadLikers` | Users who liked a thread |
| `GetInstagramPost` | Scrape Instagram post (3-method fallback) |

### Authenticated (Instagram Private API)

| Method | Description |
|--------|-------------|
| `Login` | Authenticate with username/password |
| `PublishThread` | Post a text thread |
| `LikeThread` / `UnlikeThread` | Like/unlike |
| `Follow` / `Unfollow` | Follow/unfollow |
| `GetUserFollowers` / `GetUserFollowing` | Follower/following lists |
| `SearchUsers` | Search by query |

## Types

```go
type ThreadsUser struct {
    ID, Username, FullName, Bio string
    FollowerCount, FollowingCount, ThreadCount int
    IsVerified, IsPrivate bool
    BioLinks []BioLink
}

type Post struct {
    ID, Code, Text string
    CreatedAt      time.Time
    LikeCount, ReplyCount int
    Author         ThreadsUser
    Images, Videos []MediaVersion
}
```

## Rate Limits

| Domain | Requests | Window | Min Delay |
|--------|----------|--------|-----------|
| `www.threads.net` | 30 | 15 min | 2s |
| `i.instagram.com` | 20 | 15 min | 3s |
| `www.instagram.com` | 20 | 15 min | 3s |

## License

MIT
