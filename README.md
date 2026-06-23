<p align="center">
  <img src="assets/banner.svg" alt="keel" width="100%">
</p>

The first step in server setup. **keel** prepares a fresh machine — creates the admin user, host keys, sudoers, and SSH keys, then hands off to Ansible — and runs the recurring ops around it (backups, security-group updates, VM creation). One static binary, no runtime.

## Install & use

```sh
curl -fsSL https://raw.githubusercontent.com/DigitalTolk/keel/main/install.sh | bash
```

Picks the right build for your OS/arch (Linux & macOS, amd64 & arm64), verifies its checksum against the GitHub release, and installs `keel` to `/usr/local/bin` (or `~/.local/bin` without root). Pin a version with `KEEL_VERSION=v1.2.0`.

Or, with a Go toolchain:

```sh
go install github.com/DigitalTolk/keel/cmd/keel@latest
```

Then:

```sh
keel --version
keel <command> --help      # every command and flag is self-documented
```

Configuration resolves as **flags → environment → file**. The file lives at `./keel.yaml`, `~/.config/keel/config.yaml`, or `/etc/keel/config.yaml` and holds defaults plus named backup jobs; legacy env vars (`MYSQL_HOST`, `S3_BUCKET`, `B2_*`, …) still work. Secrets are referenced, never inlined — via `password_env`, `password_file`, or an interactive prompt.

## Commands

**bootstrap** — prepare a host for Ansible

```sh
keel bootstrap known-hosts HOST...     # scan SSH host keys into ~/.ssh/known_hosts
keel bootstrap run HOST...             # create admin user + sudoers + keys, write an inventory
```

**backup** — create & rotate backups to local, S3, or Backblaze B2

```sh
keel backup mysql --db app --dest b2 --bucket b      # one database, or --all-databases
keel backup jenkins --home /var/lib/jenkins          # archive a Jenkins home
keel backup rsync --source-host h --source-path /d   # pull over SSH, then archive
keel backup sftp  --source-host h --source-path /d   # mirror over SFTP, then archive
keel backup purge --dest b2 --bucket b --keep 7      # rotate an existing prefix
keel backup run JOB                                  # run a named job from the config file
```

**aws**

```sh
keel aws sg-ingress -l sg-123 -p 22    # allow this host's current public IP, revoke the previous one
```

**vbox**

```sh
keel vbox create -b /vms -c 4 -m 4096 -l 10.0.0.5:3390 -n web1 -s 20480
```

**jenkins**

```sh
keel jenkins batch-edit --root /var/lib/jenkins OLD NEW   # replace a string across config.xml files
```

**mysql**

```sh
keel mysql to-innodb --host db1 --db app   # convert every table to the InnoDB engine
```

## License

MIT — see [LICENSE](LICENSE).

---

<sub>keel is based on [lifeofguenter/systools](https://github.com/lifeofguenter/systools).</sub>
<br><sub>🤖 Reimplemented in Go with [Claude Code](https://claude.com/claude-code).</sub>
