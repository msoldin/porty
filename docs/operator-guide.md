# Porty operator guide

Porty runs on one Linux server and manages the Docker daemon with the same effective authority as a Docker administrator. Keep it on a trusted network or place it behind an authenticated TLS reverse proxy.

## Host installation

Install Docker Engine with the Compose plugin and Git. Build the frontend and binary as an unprivileged build user:

```sh
npm --prefix web ci
npm --prefix web run build
GOTOOLCHAIN=local go build -trimpath -o porty ./cmd/porty
```

Create the service account, install the binary and configuration, and enable the unit:

```sh
sudo useradd --system --home-dir /var/lib/porty --shell /usr/sbin/nologin --groups docker porty
sudo install -o root -g root -m 0755 porty /usr/local/bin/porty
sudo install -d -o root -g porty -m 0750 /etc/porty
sudo install -o root -g porty -m 0640 deploy/porty.example.yaml /etc/porty/config.yaml
sudo install -o root -g root -m 0644 deploy/systemd/porty.service /etc/systemd/system/porty.service
sudo systemctl daemon-reload
sudo systemctl enable --now porty
```

The service creates `/var/lib/porty` with mode `0700`. Porty tightens its configured data directory to `0700` before opening the database and refuses to use the filesystem root. The service account needs membership in the Docker socket's group. This grants Docker-equivalent host privileges.

## Browser authentication and TLS proxies

Porty issues a signed access JWT in the `HttpOnly` `porty_session` cookie for 15 minutes. A separate `HttpOnly` `porty_refresh` cookie is scoped to `/api/v1/session`; each refresh rotates its random token while retaining the original seven-day login expiry. The readable `porty_csrf` cookie must match the request header for mutations and refresh. Keep the browser on one origin. Logout revokes the refresh family and clears the cookies, but an access JWT issued before logout can remain valid for at most 15 minutes.

When a TLS reverse proxy forwards HTTP to Porty's loopback listener, set `server.public_url` to its exact external origin, such as `https://porty.example.com`. Porty uses it for Origin checks and Secure cookies. Do not rely on `Forwarded` or `X-Forwarded-*` headers; Porty ignores them. Direct TLS and loopback HTTP use the request origin when `server.public_url` is empty.

## Repository setup

Repository setup is mandatory after administrator registration. Login resumes the setup screen until the repository is ready; stack, editor, deployment, and repository-operation APIs remain unavailable during that time. Porty always operates on `<data-dir>/repository` (`/var/lib/porty/repository` with the installation above). The browser cannot select another server path.

The setup screen offers three modes:

- **Create local repository** initializes the fixed directory without creating `origin`. The branch and repository-local Git author name/email are editable; `main`, `Porty`, and `porty@localhost` are only defaults.
- **Use remote repository** first inspects an HTTPS or `ssh://` remote. Porty selects its symbolic default branch when advertised, selects a sole branch automatically, or asks the administrator to choose. An empty remote starts an editable branch named `main` by default and can receive its first commit later.
- **Use mounted repository** adopts a safe Git worktree already mounted at the fixed directory. A detached `HEAD` must be changed to a branch before adoption. If `origin` exists, the administrator explicitly chooses whether Porty manages it or leaves the repository local-only.

A local-only repository fully satisfies setup and supports stack files, status, history, and commits. **Settings → Repository remote** can later add, replace, update authentication for, or remove Porty's managed `origin`. Removing it preserves the ready lifecycle, local branch, commits, files, and stacks; fetch, pull, and push remain unavailable without a managed remote.

Remote authentication is explicit: public/no authentication, HTTPS username plus secret, or fixed mounted SSH files. HTTPS secrets are write-only: the browser clears them after each attempt, APIs never return them, and Porty stores them only in its mode-restricted database for non-interactive askpass use.

SSH mode reads only these server-managed files:

- `<data-dir>/ssh/id`
- `<data-dir>/ssh/known_hosts`

Create them as regular files owned by the Porty service account. Set the private key to mode `0600` or stricter; `known_hosts` must not be group- or world-writable. Porty requires both files, strict host verification, batch mode, and the mounted identity. It does not use an SSH agent or user home configuration.

Only HTTPS and `ssh://` remotes are accepted by the Git adapter. Porty disables interactive prompts, global/system Git configuration, hooks, pagers, external diffs, filters, submodules, and credential helpers when it invokes Git.

During an upgrade, a registered installation with a safe existing repository at the fixed path is reconciled automatically. Porty imports its checked-out branch and repository-local author identity when present, applies the approved author defaults only when identity is absent, validates any existing configuration, and marks it ready. Unsafe, detached, or incomplete repositories remain in the setup lifecycle for operator action.

If the administrator password is lost, stop the service and run the offline reset command. Supplying the password through the environment keeps it out of the process argument list:

```sh
sudo systemctl stop porty
sudo -u porty env PORTY_RESET_PASSWORD='a new long password' /usr/local/bin/porty reset-password --config /etc/porty/config.yaml
sudo systemctl start porty
```

The reset changes the password, revokes refresh tokens on every device, and rotates the SQLite signing key in one transaction. Existing access JWTs become invalid immediately. Browser password changes have the same effect and also close active WebSocket connections before the response returns; WebSockets also close when their access JWT expires. Run the offline reset while the service is stopped, as shown above.

## Backup and restore

Stop Porty so the SQLite database and repository are captured at one point in time. Docker workloads continue running.

```sh
sudo systemctl stop porty
sudo tar --numeric-owner --xattrs --acls -C /var/lib -czf /srv/backup/porty-$(date +%Y%m%dT%H%M%S).tar.gz porty
sudo systemctl start porty
```

Store backups with mode `0600` and test restores on a separate host. To restore, stop Porty, move the current `/var/lib/porty` aside, extract the archive, verify ownership and mode `0700`, then start Porty. Do not merge database files or copy only `porty.db` while the service is running; SQLite may also have WAL state.

## Upgrade and rollback

Build or download the new binary, run the release checks, take a backup, stop the service, atomically replace `/usr/local/bin/porty`, and start it. Check `readyz` and the journal:

```sh
curl --fail http://127.0.0.1:8080/readyz
sudo journalctl -u porty -n 100 --no-pager
```

Migrations run at startup and are forward-only. Roll back by stopping Porty and restoring both the previous binary and the pre-upgrade data backup.

## OCI image

The image is a convenience deployment and is not a sandbox. Mount a mode-`0700` data directory and the Docker socket. Map the socket's numeric group into the container when required by the host:

```sh
docker build -t porty:local .
docker run --rm -p 127.0.0.1:8080:8080 \
  -v /srv/porty:/var/lib/porty \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  porty:local
```

Do not expose port 8080 beyond a trusted network without TLS and an appropriate network boundary.
