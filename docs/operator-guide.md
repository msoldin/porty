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

## Repository setup

Porty v1 uses `/var/lib/porty/repository`, branch `main`, and remote `origin`. Initialize, clone, or adopt that repository before using Git actions in the UI. Configure the commit identity locally:

```sh
sudo -u porty git -C /var/lib/porty/repository config user.name Porty
sudo -u porty git -C /var/lib/porty/repository config user.email porty@localhost
```

Only HTTPS and `ssh://` remotes are accepted by the Git adapter. Porty disables interactive prompts, global/system Git configuration, hooks, pagers, external diffs, filters, submodules, and credential helpers when it invokes Git.

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
