# Contributing to go-threads

go-threads reads and drives a third-party web product whose responses change without notice, so most
fixes are parser or identifier updates. Open an issue before changing a public method, a default or
the rate limits. Report vulnerabilities privately, see [SECURITY.md](SECURITY.md).

## Development

You need Go 1.26.3 or newer (the `go` line in `go.mod`).

```sh
make preflight   # the CI gate: gofmt check, go vet ./..., go build ./..., go test -count=1 ./...
make test        # tests only
```

CI runs `make preflight` on `ubuntu-24.04-arm` for every pull request (workflow `preflight`), with
the current stable Go. If it passes locally it passes there. A pull request that changes only
documents skips that job. `cmd/livetest` hits the real services with your own session values and is
not part of the gate; run it only when you changed a transport.

## Tests

Parsers are tested against captured pages and responses in `testdata/`. A fix for a changed response
shape comes with the new capture, stripped of anything personal, and a test that fails without the
fix. Tests must not need the network or credentials.

## Commits

Conventional Commits, as in the history: `feat:`, `fix:`, `deps:`, `ci:`, `docs:`, `chore:`, with a
scope when it helps (`fix(ratelimit): ...`). Releases are tagged by hand and there is no release
automation, so add the entry to [CHANGELOG.md](CHANGELOG.md) in the same change.

## Pull requests

Describe what changed, why, and how it was tested. Keep the repository free of hostnames, IP
addresses, operator paths, account names, tokens and session values, fixtures included. Update
[docs/INDEX.md](docs/INDEX.md) when you add or rename a document. By contributing you agree your
work is licensed under Apache-2.0, see [LICENSE](LICENSE).
