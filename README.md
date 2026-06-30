<p align="center">
  <img src="assets/banner.svg" alt="keel" width="100%">
</p>

<p align="center">
  <a href="https://github.com/DigitalTolk/keel/actions/workflows/ci.yml"><img src="https://github.com/DigitalTolk/keel/actions/workflows/ci.yml/badge.svg" alt="CI"></a>
  <a href="https://codecov.io/gh/DigitalTolk/keel"><img src="https://codecov.io/gh/DigitalTolk/keel/branch/main/graph/badge.svg" alt="coverage"></a>
  <a href="https://goreportcard.com/report/github.com/DigitalTolk/keel"><img src="https://goreportcard.com/badge/github.com/DigitalTolk/keel" alt="Go Report Card"></a>
  <a href="https://github.com/DigitalTolk/keel/releases"><img src="https://img.shields.io/github/v/release/DigitalTolk/keel" alt="release"></a>
  <a href="LICENSE"><img src="https://img.shields.io/github/license/DigitalTolk/keel" alt="license"></a>
</p>

The first step in server setup. **keel** prepares a fresh machine for Ansible — scans host keys, creates the admin user with a passwordless sudoers drop-in, seeds SSH keys, and writes an inventory — then hands off to Ansible. That is all it does. One static binary, no runtime. SSH is native (`golang.org/x/crypto/ssh`); nothing is shelled out.

## Install & use

```sh
curl -fsSL https://raw.githubusercontent.com/DigitalTolk/keel/main/install.sh | bash
```

Picks the right build for your OS/arch (Linux & macOS, amd64 & arm64), verifies its checksum against the GitHub release, and installs `keel` to `/usr/local/bin` (or `~/.local/bin` without root). Pin a version with `KEEL_VERSION=v1.2.0`.

Or with Homebrew (macOS & Linux):

```sh
brew install DigitalTolk/tools/keel
```

Or, with a Go toolchain:

```sh
go install github.com/DigitalTolk/keel/cmd/keel@latest
```

Then:

```sh
keel --version
keel <command> --help      # every command and flag is self-documented
```

Configuration resolves as **flags → environment → file**. The file lives at `./keel.yaml`, `~/.config/keel/config.yaml`, or `/etc/keel/config.yaml` and holds SSH defaults (user, port, jump host); the legacy `SSH_USER` / `SSH_PORT` / `SSH_JUMP_HOST` env vars still work. The bootstrap password is never inlined — supply it interactively with `--ask-pass` or via `KEEL_SSH_PASSWORD`.

## Commands

```sh
keel known-hosts HOST...     # scan SSH host keys into ~/.ssh/known_hosts
keel bootstrap HOST...       # create admin user + sudoers + keys, write an inventory
```

That is the entire surface: keel is only a server-bootstrap tool. Use `keel bootstrap --help` for every flag (`--user`, `--port`, `--jump`, `--ask-pass`, `--identity`, `--pubkey`, `--pubkey-file`, `--inventory`, `--admin-user`).

## License

MIT — see [LICENSE](LICENSE).

---

<sub>keel is based on [lifeofguenter/systools](https://github.com/lifeofguenter/systools).</sub>
<br><sub>🤖 Reimplemented in Go with [Claude Code](https://claude.com/claude-code).</sub>
