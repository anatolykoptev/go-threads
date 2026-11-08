# Changelog

All notable changes to go-threads. The format follows [Keep a Changelog](https://keepachangelog.com/en/1.1.0/).
Tags are cut by hand and the dates are those of the tagged commits.

## [Unreleased]

### Added
- Repository documents: CLAUDE.md, CONTRIBUTING.md, SECURITY.md, CODE_OF_CONDUCT.md, issue and
  pull-request templates, `docs/INDEX.md`, this changelog.

### Fixed
- README: the licence was stated as MIT (it is Apache-2.0), the rate-limit table named the old
  Threads host, and the type and method lists were stale.

## [0.10.4] - 2026-10-07
### Fixed
- Anonymous Threads fetches go through the proxy; logged-in flows keep the default profile.

## [0.10.3] - 2026-10-02
### Fixed
- The browser-transport rate-limit hold is capped at the client timeout and is never retried.

## [0.10.2] - 2026-10-01
### Fixed
- Threads traffic is throttled (the limiter host was wrong), one slot is charged per browser request,
  and the hold on the direct path is bounded.

## [0.10.1] - 2026-09-28
### Fixed
- `GetThread` parses the post-page shape with `media` and `direct_replies`.

## [0.10.0] - 2026-09-28
### Added
- `SearchPosts`: keyword post search over the browser transport.
- Session pool for browser calls (`Config.SessionPool`, `SessionPoolSize`).

### Fixed
- The browser fetch path is gated on the shared domain limiter.

## [0.9.1] - 2026-07-25
### Fixed
- Author chain cross-checks the author's listing when the chain tail cannot be proven.

## [0.9.0] - 2026-07-25
### Added
- `GetAuthorChain`: the full author chain as ordered text. A `preflight` CI gate.

## [0.8.0] - 2026-07-24
### Added
- Ordered `CarouselItems` with per-slide type and candidates on Instagram posts.

## [0.7.1] - 2026-07-24
### Fixed
- A numeric `is_dash_eligible` is tolerated in authenticated browser responses.

## [0.7.0] - 2026-07-24
### Added
- Video DASH manifest, number of qualities and DASH eligibility on `Post`.

## [0.6.0] - 2026-07-24
### Fixed
- The modern embed context JSON is parsed and the public fallback ladder is reordered embed-first, so
  downloads survive a browser-transport failure.

## [0.5.0] - 2026-07-24
### Added
- Challenge detector, session guard and proxy plumbing for the browser transport.

## [0.4.1] - 2026-07-24
### Fixed
- Instagram falls back to the legacy chain when the browser transport fails, and reports the real error.

## [0.4.0] - 2026-07-24
### Added
- View and reshare engagement counts on Instagram and Threads posts.

## [0.3.0] - 2026-07-22
### Added
- Browser-evaluate transport for authenticated Threads and Instagram calls (off by default).
- Apache-2.0 licence and a README; go-stealth bumps.

## [0.2.0] - 2026-02-22
### Added
- GraphQL API, Instagram private API, cookie-based GraphQL auth on the `threads.com` endpoint,
  Instagram post reading with an embed fallback.

## [0.1.0] - 2026-02-21
### Added
- First release: public Threads reads from server-rendered pages.
