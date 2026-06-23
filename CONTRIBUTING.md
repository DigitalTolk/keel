# Contributing to keel

Thanks for your interest in improving keel.

## Prerequisites

- Go (the version pinned in [`go.mod`](go.mod))
- Docker — only for the cloud integration tests (an S3-compatible endpoint)
- Optional: `shellcheck` (lints `install.sh`), `goreleaser` (release builds)

## Development loop

```sh
go test ./... -race        # run the suite with the race detector
go vet ./...               # static checks
gofmt -w .                 # format
go build ./cmd/keel        # build the binary
```

The `internal/cloud/aws` integration tests are skipped unless an S3-compatible
endpoint is provided. To run them against MinIO locally:

```sh
docker run -d -p 9000:9000 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data
KEEL_S3_TEST_ENDPOINT=http://localhost:9000 KEEL_S3_TEST_KEY=minioadmin KEEL_S3_TEST_SECRET=minioadmin \
  go test ./internal/... -coverpkg=./internal/... -coverprofile=cover.out
go tool cover -func=cover.out | tail -1
```

## Expectations for a change

- **Test-driven.** Write a failing test first, watch it fail for the right
  reason, then make it pass. Bug fixes start with a test that reproduces the bug.
- **Coverage.** Keep statement coverage across `internal/...` high (target 95%);
  CI fails below a 94% floor. Don't add assertion-free tests to inflate coverage —
  make the code testable instead (depend on the existing interface/function seams).
- **Green and clean.** `go test ./... -race`, `go vet ./...`, and `gofmt` must all
  pass before you open a PR.
- **Native where it counts.** Reimplement in Go rather than shelling out, except
  for tools that can't reasonably be reimplemented (`mysqldump`, `mysql`,
  `VBoxManage`, `rsync`, `lftp`) — those go through the `runner` seam with a
  dependency check.

See [`CLAUDE.md`](CLAUDE.md) for the full architecture and conventions.

## Pull requests

- Keep PRs focused on one change.
- Describe what changed and why; link any related issue.
- Make sure CI is green.

By contributing, you agree your contributions are licensed under the project's
[MIT License](LICENSE).
