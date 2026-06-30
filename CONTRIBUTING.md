# Contributing to keel

Thanks for your interest in improving keel.

## Prerequisites

- Go (the version pinned in [`go.mod`](go.mod))
- Optional: `shellcheck` (lints `install.sh`), `goreleaser` (release builds)

## Development loop

```sh
go test ./... -race        # run the suite with the race detector
go vet ./...               # static checks
gofmt -w .                 # format
go build ./cmd/keel        # build the binary

go test ./internal/... -coverpkg=./internal/... -coverprofile=cover.out
go tool cover -func=cover.out | tail -1
```

Unit tests need no external services — keel shells out to nothing, and SSH is
exercised in-process against a `crypto/ssh` test server.

End-to-end tests provision a **real** sshd container. They are build-tagged `e2e` and
gated by `KEEL_E2E_SSH_ADDR`, so they are skipped by the normal run:

```sh
docker build -t keel-sshd -f test/e2e/Dockerfile test/e2e
docker run -d --name keel-sshd -p 2222:22 keel-sshd
KEEL_E2E_SSH_ADDR=127.0.0.1:2222 KEEL_E2E_SSH_USER=root KEEL_E2E_SSH_PASSWORD=keelpass \
  go test -tags e2e -v ./e2e/...
docker rm -f keel-sshd
```

## Expectations for a change

- **Test-driven.** Write a failing test first, watch it fail for the right
  reason, then make it pass. Bug fixes start with a test that reproduces the bug.
- **Coverage.** Keep statement coverage across `internal/...` high (target 95%);
  CI fails below a 94% floor. Don't add assertion-free tests to inflate coverage —
  make the code testable instead (depend on the existing interface/function seams).
- **Green and clean.** `go test ./... -race`, `go vet ./...`, and `gofmt` must all
  pass before you open a PR.
- **Native, no shelling out.** keel uses `golang.org/x/crypto/ssh` for all SSH work
  and shells out to nothing — there is no `os/exec` in the tree. Keep it that way.
- **Stay in scope.** keel only bootstraps hosts for Ansible. Don't add unrelated
  features.

See [`CLAUDE.md`](CLAUDE.md) for the full architecture and conventions.

## Pull requests

- Keep PRs focused on one change.
- Describe what changed and why; link any related issue.
- Make sure CI is green.

By contributing, you agree your contributions are licensed under the project's
[MIT License](LICENSE).
