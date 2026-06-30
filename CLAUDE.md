# keel — Project Instructions

**keel** does exactly one thing: it prepares a fresh machine for Ansible. It scans
host keys, creates the admin user (`bofh`) with a passwordless sudoers drop-in, seeds
authorized SSH keys, and writes an Ansible inventory — then hands off to Ansible.
Nothing more. It is a single static Go binary, no runtime.

keel is open source at **github.com/DigitalTolk/keel** (MIT). The Go module lives at the
repository root (module path `github.com/DigitalTolk/keel`); CI is GitHub Actions
(`.github/workflows/`) and releases ship via GoReleaser to GitHub Releases. It is a Go
reimplementation of the `know-hosts.sh` script from github.com/lifeofguenter/systools.

**Scope is deliberately narrow.** keel is *only* a server-bootstrap tool. Do not add
backups, cloud/AWS operations, VM creation, or Jenkins/MySQL maintenance — those were
intentionally removed. If a feature is not part of bootstrapping a host for Ansible, it
does not belong here.

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
- If a line is **genuinely unreachable** (a raw TTY read, a network failure that
  can't be induced) leave it and say so explicitly — do **not** write assertion-free
  tests to touch lines (coverage gaming is forbidden). Prefer making code testable
  (see DI seams below) over faking coverage.
- The GitHub Actions test workflow runs the suite with the race detector and fails
  the build if coverage drops below the floor.

### 3. Design for testability (dependency injection)

Hard-to-test code is a design smell — fix the design, don't skip the test. The
established pattern: depend on **interfaces / function seams**, default them to the
real implementation, and substitute fakes in tests. Existing seams:

- `app` struct seams in `internal/cli`: an SSH dialer, a host-key scanner, and a
  password prompt — so command bodies run in tests without a network, TTY, or live
  SSH server.
- `bootstrap.Executor` — the remote-command interface the `Provisioner` runs over
  (`ssh.Client` satisfies it; tests use a fake).
- `ssh` is tested against an in-process `crypto/ssh` server.

Logging is the standard library **`log/slog`** (structured; `--log-format text|json`
selects the handler). There is no custom logger.

When you add something that touches the network, the filesystem in a hard way, or
time — route it through a seam so it can be faked.

---

## Architecture & conventions

- **Layout:** `cmd/keel` (entrypoint) → `internal/cli` (cobra tree: `root.go` +
  `bootstrap.go`) → the `bootstrap` domain package + shared primitives (`ssh`,
  `config`, `version`). Logging is the standard library `log/slog`. Keep each
  package small and single-purpose.
- **Native, no shelling out.** SSH exec, host-key scanning, and the jump-host tunnel
  are implemented in Go via **`golang.org/x/crypto/ssh`** (replacing the original
  script's `sshpass` / `ssh` / `ssh-keyscan`). keel shells out to nothing — there is
  no `os/exec` in the tree, and it must stay that way. Use a well-supported library
  instead of an external binary.
- **CLI:** two top-level commands — `keel bootstrap HOST...` (provision) and
  `keel known-hosts HOST...` (scan host keys). No `bootstrap` sub-grouping. Every
  command has a clear `Short`/usage.
- **Config precedence:** `flags > environment > config file > defaults`. Flags are
  applied by the cli layer; `config` handles defaults, file, and environment. Preserve
  the legacy env-var names (`SSH_USER`, `SSH_PORT`, `SSH_JUMP_HOST`, `KEEL_LOG_FORMAT`)
  as fallbacks. Defaults: ssh user `bofh`, port 22.
- **Secrets:** the bootstrap/sudo password is supplied interactively (`--ask-pass`) or
  via `KEEL_SSH_PASSWORD`; never inline it. Never place a password in `argv` — remote
  commands are base64-encoded over SSH so secrets and shell metacharacters never hit
  the visible command line. Never log a secret.
- **Errors:** return wrapped `error`s with context (which host, which step); the root
  command prints one clean line to stderr and exits non-zero. No silent failures.
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

# cross-platform release build
goreleaser build --snapshot --clean
```

---

## Definition of done (every change)

1. Behavior driven by a test that was watched to fail first.
2. `go test ./... -race` green; `go vet ./...` clean; `gofmt`'d.
3. Coverage across `internal/...` still **≥ 95%** (verified, not assumed).
4. No `os/exec` and no new external-binary dependency — SSH stays native via
   `golang.org/x/crypto/ssh`.

## Do not

- Skip TDD "just this once", or write tests after code and call it TDD.
- Add assertion-free tests to inflate coverage.
- Add features outside server bootstrap (backups, cloud ops, VMs, DB/Jenkins admin).
- Shell out to an external binary or introduce `os/exec`.
- Put PHP/Laravel/Sail conventions here.
