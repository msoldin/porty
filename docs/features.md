# Features and limits

[Documentation home](README.md)

Porty is designed for one administrator managing one Linux Docker server. One repository and one selected branch hold the Compose configuration. The Go service serves its embedded web interface; a separate frontend server is unnecessary in production.

## What you can do

| Capability               | What it provides                                                                                          | Learn more                                                                 |
| ------------------------ | --------------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------- |
| Host dashboard           | CPU, RAM, temperature, GPU, network, disk I/O, and filesystem usage where supported; short recent history | [Dashboard](user-guide.md#dashboard)                                       |
| Stack inventory          | Search, filters, status, stack creation, and selected-stack actions                                       | [Stacks](user-guide.md#stacks)                                             |
| Configuration editor     | Compose and supporting files, file operations, saved-file diffs, and stack commits                        | [Compose and files](user-guide.md#compose-and-files)                       |
| Managed environment      | Stored interpolation values, in-place editing, and optional masking                                       | [Environment values](user-guide.md#environment-values)                     |
| Deployment review        | Confirm saved configuration before deployment and follow its operation                                    | [Deployments](user-guide.md#deployments)                                   |
| Container management     | Status, ports, networks, start/stop/restart controls, inspection, and logs                                | [Containers and logs](user-guide.md#containers-and-logs)                   |
| Git integration          | Local, remote, or mounted repository setup; history, fetch, fast-forward pull, and push                   | [Git workflow](user-guide.md#git-workflow)                                 |
| Operations and incidents | Operation output/history, persisted alerts, acknowledgment/resolution, and audit events                   | [Operations, alerts, and audit](user-guide.md#operations-alerts-and-audit) |
| Automatic image updates  | Per-stack UTC schedules to update eligible public image tags with runtime verification                    | [Automatic updates](operator-guide.md#automatic-stack-updates)             |
| On-demand containers     | Stop idle groups and restart the same containers when published ports receive traffic                     | [On-demand guide](on-demand.md)                                            |
| Account and appearance   | Administrator password changes, sign-out, and light/dark/system appearance                                | [Account and appearance](user-guide.md#account-and-appearance)             |

## Terms used in the guides

| Term       | Meaning                                                                          |
| ---------- | -------------------------------------------------------------------------------- |
| Repository | The Git worktree at `<data-dir>/repository`, containing all stack files          |
| Stack      | An immediate, visible child directory containing `docker-compose.yml`            |
| Service    | A named entry under `services:` in a Compose file                                |
| Container  | A running or stopped instance of a service; a service may have multiple replicas |
| Deployment | Applying a stack's saved configuration to Docker                                 |
| Operation  | A tracked action with queued/running/final status and bounded output             |
| Alert      | A persisted incident associated with an operation failure or recovery            |

For example, Porty discovers `repository/redis-demo/docker-compose.yml`. A top-level `repository/compose.yaml` or nested `repository/apps/redis/docker-compose.yml` does not meet the stack layout.

## Know the boundaries

- Porty is not a multi-user or multi-host control plane. Run one Porty instance per workspace/daemon.
- Docker access grants host-level authority. Accepted Compose definitions are trusted administrator input, not sandboxed workloads. See the [security model](security.md).
- Saving, committing, pushing, and deploying are separate actions. Git synchronization never deploys a stack automatically.
- The editor rejects symlinks, special files, traversal, `.git`, stale writes, and files above the configured size limit. It is not a general host filesystem browser or shell.
- Host metrics depend on operating-system, driver, mount, and permission support. Missing metrics are shown as unavailable, not zero; history is held in memory for five minutes.
- Alerts record operation incidents; they are not continuous health monitoring and do not send external notifications.
- Automatic updates do not discover new version tags, update Porty's own hosting stack, or provide automatic rollback. Private registries and builds are unsupported for this feature; other eligibility restrictions also apply.
- On-demand groups require compatible local rootful Linux Docker networking and fixed published ports. The first client request may fail and must be retried. Read the [requirements](on-demand.md#deployment-requirements) before enabling it.
- External credential helpers, remote Git Compose includes, and helper-dependent build features are unsupported. Inline Docker credentials remain usable for ordinary operations.
- App URL health checks, favicon shortcuts, and GeoIP filtering are not currently implemented.
