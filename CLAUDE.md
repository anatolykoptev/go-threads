# CLAUDE.md

Notes for coding agents working in go-threads: a Go library (`package threads`, module
`github.com/anatolykoptev/go-threads`) that reads Threads and Instagram posts and acts as a logged-in
account, on top of go-stealth. Every document is indexed in [docs/INDEX.md](docs/INDEX.md).

## Layout

Single package at the root. `client.go` (Client, `NewClient`, rate-limit wiring, page fetch with
retry), `config.go`, `endpoints.go` (base URLs, app ids, GraphQL doc ids, private API paths),
`graphql.go` and `parsers.go` (public API and SSR parsing), `lsd.go` (LSD token cache), `auth.go`
(login with password encryption), `private.go` (publish, like, follow, lists), `search.go`
(`SearchPosts`), `chain.go` (author chain), `instagram.go` (post ladder), `wowa.go` (browser
transport), `types.go`, `errors.go`. `cmd/livetest` is a manual live check, not part of the gate.
`testdata/` holds real captured pages and responses used as parser fixtures.

## Gate

CI (`.github/workflows/preflight.yml`, job `preflight`, `ubuntu-24.04-arm`, Go `stable`) runs
`make preflight`: gofmt clean (vendor excluded), `go vet ./...`, `go build ./...`,
`go test -count=1 ./...`, all with `GOWORK=off`. There is no `-race` and no linter in the gate. A
docs-only PR (Markdown, `docs/`, licence, issue and PR templates) skips the `preflight` job through
the `docs-scope` job in the same workflow; that block is canonical across the fleet and compared
byte for byte, so do not edit it here. The default branch is `master`.

## Invariants that are easy to break

- **Limiter hosts come from the request base-URL constants** (`limiterHost(threadsBaseURL)`), never
  hand-typed. A typed `www.threads.net` once matched nothing and Threads traffic ran unthrottled,
  because every request goes to `www.threads.com`. `limiterHost` panics on an empty host because an
  empty domain is a wildcard rule in go-stealth.
- **A rate-limit hold is bounded and never retried.** `boundedRateLimit` waits at most `Timeout`,
  then fails with `errRateLimitHold`; the CDP path charges one limiter slot per request and caps
  its hold at `Timeout`. Code that sees `errRateLimitHold` must return, not loop. Holding `lsdMu`
  through an unbounded wait stalled every caller before this was fixed.
- **GraphQL document ids and app ids rot.** They live in `endpoints.go`; a 400 on a known query or
  `ErrUnexpectedShape` usually means a rotated id or a changed response shape, not a client bug.
  Fix the id or the parser, with a captured fixture in `testdata/`.
- **`SearchPosts` has no fallback.** It needs `Config.WowaURL`; do not add a private-API path that
  pretends to be keyword search.
- **`GetInstagramPost` is a ladder:** browser transport first when configured, then embed, server
  rendered page and the third-party video resolver. `Thread.SourceMethod` must name the tier that
  produced the result, because tiers differ in which metrics they carry.
- **Anonymous and logged-in flows use different profiles.** Anonymous Threads fetches are routed
  through the proxy; logged-in flows stay on the default profile (`wowa.go`). Do not mix them.
- **Credentials and session cookies are never logged or put in errors.** `Login` encrypts the
  password with the key the server hands out; keep `Username`, `Password`, `Token`, `SessionID`,
  `CSRFToken` and `InternalSecret` out of `slog` fields and error strings.
- **Fixtures are real captured pages.** `testdata/embed_reel_real.html` and `lsd_test.go` carry
  public, per-page anti-CSRF tokens, listed in `.gitleaksignore`; do not add real user data or
  session values to fixtures.
- **Releases are tagged by hand** (`vX.Y.Z`, no release automation); keep `CHANGELOG.md` current in
  the same change.
- **This repository is public.** No hostnames of private services, IP addresses, operator paths,
  account names, tokens or personal data in any tracked file, tests and fixtures included.
