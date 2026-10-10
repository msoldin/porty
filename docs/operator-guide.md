# Porty operator guide

[Documentation home](README.md) · [First-stack walkthrough](getting-started.md) · [Configuration reference](configuration.md)

Porty runs on one Linux server and manages the Docker daemon with the same effective authority as a Docker administrator. Keep it on a trusted network or place it behind an authenticated TLS reverse proxy.

## Host installation

Install [Docker Engine](https://docs.docker.com/engine/install/) on the Linux runtime host. Porty uses in-process Git and Compose SDKs; Git and the Docker Compose CLI plugin are not runtime dependencies. Docker socket access is required for workload management.

On the build machine, install Git (to obtain the source), [Go **1.27.1**](https://go.dev/doc/install), [Bun **1.4.2**](https://bun.com/docs/installation), and a C compiler/libc development headers for the default CGO-enabled build. These versions match the current repository; check `go.mod` and `web/package.json` when building another revision. Clone the repository and build as an unprivileged user:

```sh
git clone https://github.com/msoldin/porty.git
cd porty
(cd web && bun ci)
(cd web && bun run build)
GOTOOLCHAIN=local go build -trimpath -o porty ./cmd/porty
```

Build the frontend first: the binary embeds `web/dist`. Build for the runtime host's architecture and a compatible libc. `CGO_ENABLED=0 go build -trimpath -o porty ./cmd/porty` is an alternative without NVIDIA monitoring support. The runtime host does not need Go or Bun.

On a Linux host with systemd and the Docker `docker` group, create the service account, install the binary and configuration, and enable the unit. Run from the source directory with the built binary present. For an existing installation, use the upgrade procedure instead of recreating its account or configuration:

```sh
sudo useradd --system --home-dir /var/lib/porty --shell /usr/sbin/nologin --groups docker porty
sudo install -o root -g root -m 0755 porty /usr/local/bin/porty
sudo install -d -o porty -g porty -m 0700 /var/lib/porty /var/lib/porty/.docker
printf '{}\n' | sudo -u porty tee /var/lib/porty/.docker/config.json >/dev/null
sudo chmod 0600 /var/lib/porty/.docker/config.json
sudo install -d -o root -g porty -m 0750 /etc/porty
sudo install -o root -g porty -m 0640 deploy/porty.example.yaml /etc/porty/config.yaml
sudo install -o root -g root -m 0644 deploy/systemd/porty.service /etc/systemd/system/porty.service
sudo systemctl daemon-reload
sudo systemctl enable --now porty
```

The service creates `/var/lib/porty` with mode `0700`. Porty tightens its configured data directory to `0700` before opening the database and refuses to use the filesystem root. The service account needs membership in the Docker socket's group. This grants Docker-equivalent host privileges.

Check startup with `sudo systemctl status porty --no-pager` and `curl --fail http://127.0.0.1:8080/readyz`. Open `http://127.0.0.1:8080` on the server or use the [SSH tunnel in getting started](getting-started.md#1-install-and-open-porty). Complete administrator and repository setup in the browser. Keep access private during initial registration.

## Browser authentication and TLS proxies

Porty issues a signed access JWT in the `HttpOnly` `porty_session` cookie for 15 minutes. A separate `HttpOnly` `porty_refresh` cookie is scoped to `/api/v1/session`; each refresh rotates its random token while retaining the original seven-day login expiry. The readable `porty_csrf` cookie must match the request header for mutations and refresh. Keep the browser on one origin. Logout revokes the refresh family and clears the cookies, but an access JWT issued before logout can remain valid for at most 15 minutes.

When a TLS reverse proxy forwards HTTP to Porty's loopback listener, set `server.public_url` to its exact external origin, such as `https://porty.example.com`. Porty uses it for Origin checks and Secure cookies. Do not rely on `Forwarded` or `X-Forwarded-*` headers; Porty ignores them. Direct TLS and loopback HTTP use the request origin when `server.public_url` is empty.

## Repository setup

Repository setup is mandatory for stack management after administrator registration. Login resumes the setup screen until the repository is ready; stack, editor, deployment, and repository-operation APIs remain unavailable during that time. The Dashboard is available after sign-in before repository setup completes. Porty always operates on `<data-dir>/repository` (`/var/lib/porty/repository` with the installation above). The browser cannot select another server path.

The setup screen offers three modes:

- **Create local repository** initializes the fixed directory without creating `origin`. The branch and repository-local Git author name/email are editable; `main`, `Porty`, and `porty@localhost` are only defaults.
- **Use remote repository** first inspects an HTTPS or `ssh://` remote. Porty selects its symbolic default branch when advertised, selects a sole branch automatically, or asks the administrator to choose. An empty remote starts an editable branch named `main` by default and can receive its first commit later.
- **Use mounted repository** adopts a safe Git worktree already mounted at the fixed directory. A detached `HEAD` must be changed to a branch before adoption. If `origin` exists, the administrator explicitly chooses whether Porty manages it or leaves the repository local-only.

A local-only repository fully satisfies setup and supports stack files, status, history, and commits. **Settings → Repository remote** can later add, replace, update authentication for, or remove Porty's managed `origin`. Removing it preserves the ready lifecycle, local branch, commits, files, and stacks; fetch, pull, and push remain unavailable without a managed remote.

Remote authentication is explicit: public/no authentication, HTTPS username plus secret, or fixed mounted SSH files. HTTPS secrets are write-only: the browser clears them after each attempt, APIs never return them, and Porty stores them in its mode-restricted database for the Git SDK's HTTPS authentication. Use an account/token with only the repository permissions you need; pushing requires write access. Do not embed credentials in the remote URL.

SSH mode reads only these server-managed files:

- `<data-dir>/ssh/id`
- `<data-dir>/ssh/known_hosts`

Create them as regular files owned by the Porty service account in a private `ssh` directory. Set the private key to mode `0600` or stricter; `known_hosts` must not be group- or world-writable. Verify host keys through a trusted source before installing them. Porty requires both files and uses the mounted identity with strict host verification through the Git SDK. The private key must work without an interactive passphrase; Porty does not use an SSH agent or user home configuration.

Only HTTPS and `ssh://` remotes are accepted by the Git adapter, for example `https://github.com/your-org/stacks.git` or `ssh://git@github.com/your-org/stacks.git`. SCP-style `git@github.com:your-org/stacks.git` URLs are unsupported. Porty does not launch Git, SSH commands, credential helpers, hooks, pagers, or filters. Adopted repositories with unsafe command-bearing Git configuration are rejected.

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
sudo install -d -o root -g root -m 0700 /srv/backup
sudo sh -c 'umask 077; tar --numeric-owner --xattrs --acls -C /var/lib -czf "/srv/backup/porty-$(date +%Y%m%dT%H%M%S).tar.gz" porty'
sudo systemctl start porty
```

Store backups with mode `0600` and test restores on a separate host. To restore, stop Porty, move the current `/var/lib/porty` aside, extract the archive, verify ownership and mode `0700`, then start Porty. Do not merge database files or copy only `porty.db` while the service is running; SQLite may also have WAL state.

This protects Porty's state and repository, including stored environment values and credentials. It does not back up application Docker volumes or external bind mounts. Back those up using application-appropriate procedures. For OCI installations, stop the Porty container and capture its complete data volume with the same consistency and permission requirements.

## Upgrade and rollback

Build the new revision's frontend and binary, run the release checks, take a backup, stop the service, atomically replace `/usr/local/bin/porty`, and start it. Check `readyz` and the journal:

```sh
curl --fail http://127.0.0.1:8080/readyz
sudo journalctl -u porty -n 100 --no-pager
```

Migrations run at startup and are forward-only. Roll back by stopping Porty and restoring both the previous binary and the pre-upgrade data backup.

For OCI installations, rebuild/tag the image from the selected source revision, back up the data volume, and recreate the Porty container with the same volume, socket access, configuration, and network settings. Retain the previous image and data backup together for rollback. This guide does not assume a published binary release or registry image.

## OCI image

The image is a convenience deployment and is not a sandbox. On the local Linux Docker host, obtain the source and build the frontend as above. The Dockerfile builds the Go binary and embeds the already-built `web/dist`; it does not run Bun.

Build a local image, then use a named volume for persistent Porty data and map the Docker socket's numeric group. This basic example disables optional host monitoring until its host mounts are configured:

```sh
docker build -t porty:local .
docker run -d --name porty --restart unless-stopped \
  -p 127.0.0.1:8080:8080 \
  -v porty-data:/var/lib/porty \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  -e PORTY_MONITORING_MODE=disabled \
  porty:local
docker logs --tail 100 porty
curl --fail http://127.0.0.1:8080/readyz
```

For a new named volume, Docker initializes ownership from the image's data directory. If you choose a host bind mount instead, pre-create it with mode `0700` and ownership matching the image's `porty` user; do not assume the UID matches a host account. Preserve existing data and ownership during upgrades.

For a configuration file, mount it read-only and pass `--config /path/in/container/config.yaml` after the image name. Replacement arguments replace the image's default command, so explicitly set `--listen 0.0.0.0:8080` for bridge networking; the host `-p` above limits exposure to loopback. See [configuration precedence](configuration.md#sources-and-precedence).

Bind mounts in managed Compose stacks refer to paths on the Docker daemon's host. For relative bind paths, make the repository available at the same absolute path on the host and inside Porty, or use suitable named volumes/absolute host paths. Do not assume a path visible only inside the Porty container exists on the daemon host.

Do not expose port 8080 beyond a trusted network without TLS and an appropriate network boundary.

For automatic wake and idle stop, see [On-demand container groups](on-demand.md).
Containerized Porty needs host networking for that feature and an explicit
loopback UI bind. Ordinary bridge-networked Porty can continue managing stacks.

## Alerts

The **Alerts** page collects persisted stack and container operation failures. The navigation badge counts all unacknowledged incidents, including resolved incidents. Each stack also has an Alerts tab. Stack names and alert history remain available after a stack is archived or deleted.

- **Acknowledge** records that you reviewed an incident. It does not resolve the problem or change container state.
- **Resolved** records recovery. Successful manual actions resolve matching incidents only when the requested runtime state is verified; unrelated failures stay open. Where offered, manual resolution records your recovery and an optional note.
- A repeated failure of an open incident adds an occurrence. A failure after resolution opens a new episode and needs acknowledgment again.
- **Needs attention** includes open incidents and recovered incidents awaiting acknowledgment. Use **All history** to review completed incidents and their linked operations.

Historical failed operations remain failed after recovery. Acknowledging or resolving an alert never starts containers or resumes automatic updates. Alerts do not continuously monitor runtime health or send external notifications. Interrupted accepted mutations are recorded once when Porty restarts.

## Automatic stack updates

In **Stack → Settings → Automatic updates**, enable updates for each stack and save its schedule. Existing stacks default to disabled. The default `0 0 * * *` runs daily at midnight **UTC**, regardless of the server or browser timezone. Use five fields: minute, hour, day of month, month, day of week. Seconds, `@daily`, and timezone prefixes are unsupported. When both day fields are restricted, matching either day of month **or** day of week triggers the schedule.

Porty checks the configured public image tags for a different runnable image on the running platform. It does not discover newer version tags: `postgres:17` stays on that tag. Private registry credentials and automatic builds are unsupported. Built services, digest-pinned images, `pull_policy: never`, images without registry provenance, and services scaled to zero are excluded. Other eligible services in the stack can still update. Registry authentication failures and check failures create persistent alerts; rate limits wait until the next scheduled check.

A check only updates a fully running, healthy stack with a successful deployment matching its current source configuration. Stopped, missing, partially running, archived, never-deployed, or drifted stacks are skipped. One-off containers, unsupported profiles and dependency behavior can also prevent an update. A saved policy can remain enabled while a stack is stopped; checks will skip it until it is eligible. Check the last outcome, exclusions, and eligibility message in Settings.

Updates pull immutable image digests before recreating only changed services. They preserve replica counts and mounted named/anonymous volumes, and do not start dependencies, remove orphans, prune, or roll back. Recreation causes downtime and discards container writable-layer data. Application migrations can change retained volume data; Porty cannot undo those changes. Keep application-appropriate backups. Healthchecks must pass; containers without healthchecks must stay running continuously for 30 seconds, within a five-minute verification window.

Manual operations and automatic mutations share stack/repository coordination. Configuration and runtime evidence are checked again after image preparation; a conflicting manual operation makes the check skip. Run only **one Porty instance per workspace/daemon**. Direct external Docker or filesystem changes are outside these process-local locks.

Two workers process due checks. Each occurrence has a 20-minute admission-to-completion budget; busy or expired queued checks are recorded as skips. Restarting Porty advances schedules to future occurrences, with no missed-run replay. Disabling a schedule invalidates queued checks but lets an already accepted mutation finish verification and persistence.

### Porty self-protection and Docker support

Porty cannot automatically update its own hosting stack. It proves runtime ownership through a private random marker and Docker's container archive API, independently of image names and hostnames. Unreadable/ambiguous scans, changed daemon identity, or a missing marker make automatic updates unavailable. Shared mounts exposing the marker in multiple projects protect all matching projects. Keep Porty's temporary directory private and available for the lifetime of the process; do not deliberately share it into managed stacks. A remote daemon must permit the same complete inspection. Manual operations remain available when this automatic safety proof fails.

Automatic image inspection requires Docker API **1.49 or newer** and an unambiguous platform-specific runnable image identity. Older or incompatible image stores fail closed. The release must pass the disposable-daemon verification matrix described in the development guide before enabling this feature for real workloads.

### Failure and recovery

Check failures before mutation leave the stack running and retry only at the next cron occurrence. A failure or interruption after recreation may have begun creates a deployment incident and **pauses automatic updates**. There is no automatic retry or rollback of a partially changed stack.

1. Review the linked operation, per-service image results, and stack alerts.
2. Inspect and recover the application using manual operations. Deploy any pending source changes.
3. Use **Verify recovery and resume** in the automatic-update panel. Porty verifies the complete current runtime, records its baseline, and clears the pause. It does not recreate or start containers. A disabled policy remains disabled.

Acknowledging or resolving the alert records incident handling; neither clears the recovery pause. Enable/disable and schedule edits also preserve it. A stale browser revision is rejected; refresh the status and review the current state before retrying.

After a verified automatic update, later manual deploy/recreate operations retain the selected image for unchanged image references and platforms. A successful explicit **Pull** clears that selection so the configured tag can be used again. Editing an image reference, platform, or build configuration also invalidates the matching selection after a successful deployment. Source Compose files are not rewritten by automatic updates.

### Restart and shutdown safety

Porty stops background admission and drains accepted operations for up to 30 seconds before closing Docker. The systemd unit allows 45 seconds for HTTP shutdown and this drain. A forced stop can leave durable update intent: prepared-only work is discarded; applying/verifying work is recorded as interrupted and automatic updates stay paused for manual recovery. Startup inspection never replays a recreation or performs rollback. If outcome persistence fails, automatic admission stops until reconciliation succeeds after restart.

## Host monitoring

The Dashboard is available after sign-in, even before repository setup. Porty
samples once every two seconds, shares that cache across clients, and retains
five minutes in memory. Restarting Porty clears history. Rates use decimal B/s;
RAM and GPU memory use binary units. CPU busy excludes I/O wait; RAM is total
minus available. Disk I/O combines eligible physical leaf devices only;
filesystem fullness remains separate for each filesystem and includes reserved
space. Missing, denied, unsupported and stale readings are never shown as zero.

Native installs default to `monitoring.mode: native`. Set
`PORTY_MONITORING_MODE=disabled` to stop collection. The OCI image defaults to
`host`, using `/host/proc`, `/host/sys` and `/host/root`; missing paths never
fall back to the container. Override with `PORTY_MONITORING_HOST_PROC`,
`PORTY_MONITORING_HOST_SYS` and `PORTY_MONITORING_HOST_ROOT`. Host network counters
come from host PID 1's network namespace, without joining that namespace.

For a local Linux Docker Engine supporting recursive read-only binds (Linux
5.12+), use the following monitoring mounts with your existing data/socket
configuration. No host PID namespace, host networking or privileged mode is
needed for basic metrics. Recreate the existing Porty container with these options,
retaining its data volume; do not run a second instance against the same data:

```sh
docker run --rm --name porty -p 127.0.0.1:8080:8080 \
  --mount type=volume,src=porty-data,dst=/var/lib/porty \
  -v /var/run/docker.sock:/var/run/docker.sock \
  --group-add "$(stat -c %g /var/run/docker.sock)" \
  --mount type=bind,src=/proc,dst=/host/proc,readonly,bind-recursive=readonly,bind-propagation=rprivate \
  --mount type=bind,src=/sys,dst=/host/sys,readonly,bind-recursive=readonly,bind-propagation=rprivate \
  --mount type=bind,src=/,dst=/host/root,readonly,bind-recursive=readonly,bind-propagation=rprivate \
  porty:local
```

The host-root bind exposes readable host files to Porty's service user. Omit
that bind if that visibility is inappropriate or recursive read-only mounts
are unsupported; filesystem metrics then report unavailable. Do not replace
it with a writable bind or assume `-v /:/host/root:ro` protects nested mounts.
Docker documents the [recursive mount contract](https://docs.docker.com/engine/storage/bind-mounts/#recursive-mounts).
Private propagation means newly attached filesystems may require recreating
the container's mounts. Porty checks the opened filesystem's device identity
against host mountinfo and rejects a missing or mismatched host mount.
Rootless engines and proc hidepid restrictions can limit host visibility;
Docker Desktop observes its Linux VM, not macOS or Windows hardware.

AMD metrics use readable DRM sysfs counters. NVIDIA support uses the official
NVML binding, loaded at runtime; no CUDA toolkit is bundled. Configure the
[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/docker-specialized.html)
and grant the desired GPU with `--gpus` and
`NVIDIA_DRIVER_CAPABILITIES=utility`. A normal machine without NVIDIA libraries
still starts. Builds made with `CGO_ENABLED=0` explicitly report NVIDIA
monitoring as unsupported. MIG/device-specific fields may be partial.

Intel utilization uses i915/Xe PMU engine activity, labelled **busiest measured
engine**, not whole-GPU busy. Xe discovery may need a specific
`--device=/dev/dri/renderD128` and the matching render group; device paths and
group IDs vary by host. PMU reads may need `--cap-add=PERFMON` plus a copy of
Docker's version-matched default seccomp profile that permits only
`perf_event_open` in addition to its existing rules. Do not disable seccomp or
use `--privileged`. Driver, kernel, permissions and PMU availability determine
coverage. Rootless setups often cannot grant the necessary host PMU access.

The native systemd unit keeps its default restrictions. For a host that
explicitly needs Intel PMU access, create a reviewed service drop-in:

```ini
[Service]
CapabilityBoundingSet=CAP_PERFMON
AmbientCapabilities=CAP_PERFMON
PrivateDevices=no
DevicePolicy=closed
DeviceAllow=/dev/dri/renderD128 r
SupplementaryGroups=render
```

Adjust the render node/group to the selected GPU. If a custom syscall filter blocks perf_event_open, add that syscall to the existing
allowlist rather than replacing the filter. Keep the existing filesystem
restrictions; inspect the effective unit and journal after applying
a drop-in. NVIDIA access under systemd also needs its specific device nodes and
driver libraries made visible; do not grant all devices globally. Temperature,
memory and utilization capabilities are reported independently. Fixture tests
do not establish hardware compatibility for every driver.

App URL health checks and favicon shortcuts are reserved for a later feature.
