# keel — Project Instructions

**keel** is the company-wide tool for the first step in server setup: it prepares a
fresh machine (admin user, host keys, sudoers, SSH keys, Ansible inventory) and runs
the recurring ops around it (backups to local/S3/B2, AWS security-group updates, VM
creation, Jenkins/MySQL maintenance). It is a single static Go binary, no runtime.

keel is open source at **github.com/DigitalTolk/keel** (MIT). The Go module lives at the
repository root (module path `github.com/DigitalTolk/keel`); CI is GitHub Actions
(`.github/workflows/`) and releases ship via GoReleaser to GitHub Releases. It is a Go
reimplementation of the shell scripts at github.com/lifeofguenter/systools.

---

## Non-negotiable rules

### 1. Test-Driven Development (mandatory)

**No production code without a failing test first.** Follow red → green → refactor:

1. **RED** — write one focused test for the next behavior. Run it. **Watch it fail
   for the right reason** (feature missing, not a typo). A test that passes
   immediately proves nothing — delete it and start over.
2. **GREEN** — write the minimum code to pass. Run the test; confirm it passes and
   no others broke. Output must be pristine (no warnings).
3. **REFACTOR** — clean up with tests green. Don't add behavior.

This applies to features, bug fixes, and refactors. Bug fixes start with a failing
test that reproduces the bug. Do not write tests _after_ implementation and call it
TDD — tests-after answer "what does this do", tests-first answer "what should it do".

### 2. Minimum 95% statement coverage

Coverage across `internal/...` must stay **≥ 95%**. Check before considering work
complete:

```bash
go test ./internal/... -coverpkg=./internal/... -coverprofile=cover.out
go tool cover -func=cover.out | tail -1        # must read >= 95.0%
go tool cover -func=cover.out | awk '$3 != "100.0%"'   # see what's left
```

- New code must land with the tests that keep coverage ≥ 95%. Adding a command
  without raising its package's coverage is incomplete work.
- If a line is **genuinely unreachable** (a raw TTY read, a stdlib failure that
  can't be induced, an on-disk-profile branch), leave it and say so explicitly —
  do **not** write assertion-free tests to touch lines (coverage gaming is
  forbidden). Prefer making code testable (see DI seams below) over faking coverage.
- The cloud (`internal/cloud/aws`) integration tests are gated by
  `KEEL_S3_TEST_ENDPOINT`; they skip locally and run against MinIO in CI. The 95%
  figure is measured with that endpoint set (the GitHub Actions test workflow starts
  MinIO and sets that endpoint; it also fails the build if coverage drops below 95%).

### 3. Design for testability (dependency injection)

Hard-to-test code is a design smell — fix the design, don't skip the test. The
established pattern: depend on **interfaces / function seams**, default them to the
real implementation, and substitute fakes in tests. Existing seams:

- `runner.Runner` — external-command execution (faked to assert argv / inject output).
- `backup.Destination` — storage backend (`LocalDestination` is real and testable;
  `cloud/aws` implements the same interface for S3/B2).
- `app` struct seams in `internal/cli`: a runner factory, a tool-presence check, an
  SSH dialer, a host-key scanner, a password prompt, a public-IP fetcher, a
  security-group client factory, and a clock — so command bodies run in tests
  without network, TTY, installed tools, or real services.
- `log` uses injectable writers + clock; `ssh` is tested against an in-process
  `crypto/ssh` server.

When you add a command that touches the network, the filesystem in a hard way, an
external binary, or time — route it through a seam so it can be faked.

---

## Architecture & conventions

- **Layout:** `cmd/keel` (entrypoint) → `internal/cli` (cobra tree, one file per
  domain) → domain packages (`bootstrap`, `backup`, `cloud/aws`, `jenkins`,
  `mysqladmin`, `vbox`) + shared primitives (`ssh`, `runner`, `config`, `log`,
  `version`). Keep each package small and single-purpose.
- **Native where it counts:** reimplement SSH exec, host-key scanning, AWS/B2 access
  (S3 SDK serves both), EC2 security groups, and archiving (`archive/tar` +
  `compress/gzip`) in Go. Shell out **only** to tools that can't reasonably be
  reimplemented (`mysqldump`, `mysql`, `VBoxManage`, `rsync`, `lftp`), always via
  `runner` and behind the `requireTools` seam (a dependency check) first.
- **CLI:** domain-grouped subcommands (`keel backup mysql`, `keel bootstrap run`).
  Every command has a clear `Short`/usage.
- **Config precedence:** `flags > environment > config file > defaults`. Preserve the
  legacy env-var names (`MYSQL_HOST`, `S3_BUCKET`, `B2_*`, `NUM_BACKUPS`, `SSH_USER`,
  …) as fallbacks. Named jobs live in the config file and run via `keel backup run <name>`.
- **Secrets:** never inline them in config; reference via `password_env` /
  `password_file` / interactive prompt. Never place a password in `argv` (use a
  `--defaults-extra-file` or env). Never log a secret.
- **Errors:** return wrapped `error`s with context (which host, which DB, which
  command); the root command prints one clean line to stderr and exits non-zero.
  No silent failures — every shelled-out non-zero exit is wrapped.
- **Go style:** explicit return types, constructor injection, `gofmt`/`go vet` clean,
  small focused files. Match the surrounding code.

---

## Commands

```bash
go test ./... -race                 # run all tests (race detector on)
go vet ./...                        # static checks
go build ./cmd/keel                 # build the binary
go test ./internal/<pkg>/ -run TestName -v   # focused test run

# coverage (see rule 2)
go test ./internal/... -coverpkg=./internal/... -coverprofile=cover.out
go tool cover -func=cover.out | tail -1

# cloud integration tests against MinIO (optional, local)
docker run -d -p 9000:9000 -e MINIO_ROOT_USER=minioadmin -e MINIO_ROOT_PASSWORD=minioadmin minio/minio server /data
KEEL_S3_TEST_ENDPOINT=http://localhost:9000 KEEL_S3_TEST_KEY=minioadmin KEEL_S3_TEST_SECRET=minioadmin go test ./internal/cloud/aws/

# cross-platform release build
goreleaser build --snapshot --clean
```

---

## Definition of done (every change)

1. Behavior driven by a test that was watched to fail first.
2. `go test ./... -race` green; `go vet ./...` clean; `gofmt`'d.
3. Coverage across `internal/...` still **≥ 95%** (verified, not assumed).
4. New external-tool dependencies guarded by the `requireTools` seam.

## Do not

- Skip TDD "just this once", or write tests after code and call it TDD.
- Add assertion-free tests to inflate coverage.
- Put PHP/Laravel/Sail conventions here
