GO ?= go

.PHONY: preflight build test vet fmt-check clean

## preflight: pre-merge gate (gofmt + vet + build + test). Mirrors the fleet
## convention used by dozor / go-media / vaelor. GOWORK=off is set per-command
## so the gate is hermetic — a stray parent go.work on a dev box (which lists
## modules absent from this checkout) cannot break the build. A fresh CI clone
## has no go.work, so GOWORK=off is a no-op there; set for determinism.
preflight: fmt-check vet build test
	@echo "==> preflight: all gates passed"

## fmt-check: verify code is gofmt-clean (no diff). Fails on any unformatted file.
.PHONY: fmt-check
fmt-check:
	@echo "==> gofmt -l ."
	@dirty=$$(gofmt -l . | grep -v '^vendor/' || true); \
	  if [ -n "$$dirty" ]; then \
	    echo "FAIL: gofmt -- the following files are not formatted (run: gofmt -w <file>):"; \
	    echo "$$dirty"; \
	    exit 1; \
	  fi

## vet: go vet ./...
.PHONY: vet
vet:
	@echo "==> go vet ./..."
	@GOWORK=off $(GO) vet ./...

## build: go build ./...
.PHONY: build
build:
	@echo "==> go build ./..."
	@GOWORK=off $(GO) build ./...

## test: go test -count=1 ./...
.PHONY: test
test:
	@echo "==> go test -count=1 ./..."
	@GOWORK=off $(GO) test -count=1 ./...

clean:
	@GOWORK=off $(GO) clean -cache -testcache
