# go-threads

[![Go 1.26+](https://img.shields.io/badge/Go-1.26+-00ADD8?logo=go)](go.mod)
[![License: Apache 2.0](https://img.shields.io/badge/License-Apache_2.0-blue.svg)](LICENSE)

A Go client for Threads (Meta) and Instagram posts. It reads public profiles, threads, replies and
likers from the web GraphQL API and from server-rendered pages, can act as a logged-in account
through the Instagram private API (publish, like, follow, search), and can route authenticated web
calls through a real browser session. Requests go out through
[go-stealth](https://github.com/anatolykoptev/go-stealth), which supplies the browser TLS
fingerprint, proxy rotation and rate limiting.

## Install

```sh
go get github.com/anatolykoptev/go-threads@latest
```

Go 1.26 or newer. The only dependency is go-stealth.

## Usage

```go
client, err := threads.NewClient(threads.Config{Timeout: 30})
if err != nil { /* ... */ }

// Public, no login.
user, _ := client.GetUser(ctx, "zuck")
threadsList, _ := client.GetUserThreads(ctx, "zuck", 10)
thread, replies, _ := client.GetThread(ctx, "zuck", "<post code>")

// Logged in (Instagram private API).
authed, _ := threads.NewClient(threads.Config{
    Username: os.Getenv("IG_USERNAME"),
    Password: os.Getenv("IG_PASSWORD"),
})
_ = authed.Login(ctx)
mediaID, _ := authed.PublishThread(ctx, "Hello Threads!")
```

`Config.Token` skips the login with an existing bearer token; `SessionID`, `CSRFToken`, `DSUserID`,
`IGDID` and `MID` carry web session cookies for the authenticated GraphQL path. `ProxyPool` rotates
proxies (any `stealth.ProxyPoolProvider`), `Timeout` is in seconds (default 15) and `MetricsHook`
reports each endpoint's success.

## API

| Group | Methods |
|---|---|
| Public (no login) | `GetUser`, `GetUserByID`, `GetUserThreads`, `GetUserWithThreads`, `GetUserReplies`, `GetThread`, `GetThreadLikers`, `SearchUsers`, `GetAuthorChain` (a thread's author chain in writing order, with completeness metadata) |
| Instagram posts | `GetInstagramPost` (by shortcode; carries views, reshares, video qualities and ordered carousel slides), `GetInstagramUser` |
| Logged in (private API) | `Login`, `IsAuthenticated`, `PublishThread`, `LikeThread`, `UnlikeThread`, `Follow`, `Unfollow`, `GetUserFollowers`, `GetUserFollowing`, `SearchUserPrivate`, `GetThreadByID` |
| Keyword post search | `SearchPosts(ctx, query, SearchPostsOpts{Mode: SearchTop or SearchRecent, Count})`; needs `Config.WowaURL`, there is no fallback |

Results are `*ThreadsUser`, `*Thread` (a list of `Post` items plus `SourceMethod`, which tells you
which tier produced it: `cdp`, `embed`, `ssr` or `proxy`) and `Post` (text, counts, media versions,
author). `ErrUnexpectedShape` means a response parsed but had none of the expected containers,
typically a rotated GraphQL document id; other failures are wrapped with the endpoint and status.

## Browser transport

Set `Config.WowaURL` to the base URL of a browser-automation service that exposes a Chrome
`interact` endpoint, and authenticated and Instagram calls are made as in-page fetches in a
persistent browser session instead of from the client's own TLS stack. Related fields: `Session`
(named session, default `threads-cdp`), `SessionPool` (spread calls over N pages),
`InternalSecret` (sent as `X-Internal-Secret`) and `Proxy` (proxy for the browser fetch).
`GetInstagramPost` tries this path first and falls back to the public ladder (embed, then
server-rendered page, then a third-party video resolver) if it fails. Without `WowaURL` only the
direct path is used.

## Rate limits

Built in, per host, through go-stealth's domain limiter. A request waits at most `Timeout` for a
slot and then fails; a 429 puts the host on cooldown.

| Host | Requests | Window | Min delay |
|---|---|---|---|
| `www.threads.com` | 30 | 15 min | 2 s |
| `i.instagram.com` | 20 | 15 min | 3 s |
| `www.instagram.com` | 20 | 15 min | 3 s |

## Live check

`cmd/livetest` exercises the client against the real services. It reads `THREADS_SESSION_ID`,
`THREADS_CSRF`, `THREADS_DS_USER_ID`, `THREADS_IG_DID`, `THREADS_MID`, `THREADS_WOWA_URL`,
`THREADS_CDP_PROXY` and `INTERNAL_SERVICE_SECRET` from the environment. The library itself reads no
environment variables.

## Status

Pre-1.0 (`v0.10.x`): the API can change in a minor release. Tags are cut by hand, see
[CHANGELOG.md](CHANGELOG.md). Contributions: [CONTRIBUTING.md](CONTRIBUTING.md). Every document is
indexed in [docs/INDEX.md](docs/INDEX.md). Report vulnerabilities as described in
[SECURITY.md](SECURITY.md).

## Licence

Apache-2.0, see [LICENSE](LICENSE).
