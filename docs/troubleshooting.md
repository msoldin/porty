# Troubleshooting

[Documentation home](README.md)

Start with the failing operation's output or visible error. Record the version/revision, action, and relevant redacted message before retrying. Never include live tokens, environment values, database copies, or private keys in an issue.

## Porty does not start or become ready

For the systemd installation:

```sh
sudo systemctl status porty --no-pager
sudo journalctl -u porty -n 100 --no-pager
curl --fail http://127.0.0.1:8080/readyz
```

For the named OCI example, use `docker logs --tail 100 porty`. Check configuration paths, paired TLS certificate/key settings, an unused listen port, and the data directory's ownership. Invalid configuration is rejected at startup. After correction and restart, expect `/readyz` to succeed and the UI to load. A healthy listener does not prove every optional metric or automation feature is available.

See [configuration](configuration.md) and [installation](operator-guide.md#host-installation).

## Docker or storage permission errors

Check that the Porty service user can access the configured data directory and Docker socket. Native installation uses the `docker` group; the OCI example adds the socket's numeric group. A host bind-mounted data directory must be writable by the image's user; a Docker named volume is simpler for a fresh installation.

Preserve `0700` data-directory permissions. Do not make the socket world-writable or use `--privileged` as a shortcut. Correct ownership/group access and restart; expect container status and operations to work. On a remote Docker daemon, host bind-mount paths refer to the daemon's host, not your client machine.

## Login fails behind a proxy

Check that the browser uses the intended external origin and that `server.public_url` matches it exactly. Include the scheme, omit a trailing slash, and use HTTPS when the proxy terminates TLS. Porty ignores forwarding headers. Avoid mixing hostnames, schemes, or ports during a session.

Correct the configuration, restart, and sign in at the canonical URL; expect session creation and mutations to work without origin/CSRF errors. See [TLS proxies](operator-guide.md#browser-authentication-and-tls-proxies). For a forgotten password, stop Porty and use the documented [offline reset](operator-guide.md#repository-setup).

## Repository setup or authentication fails

Check the setup error and selected mode. Remote URLs must use HTTPS or `ssh://`; use explicit username/secret authentication for HTTPS or the mounted SSH key and verified `known_hosts` for SSH. SCP-style URLs such as `git@host:team/repo.git` are not accepted.

For mounted repositories, check the fixed `<data-dir>/repository` location, ownership, safe Git configuration, and a checked-out branch rather than detached HEAD. An unsuccessful setup keeps stack APIs unavailable; correct the cause and retry until setup reports ready. Do not delete an existing repository to bypass validation. See [Repository setup](operator-guide.md#repository-setup).

## A stack is missing

Check that its directory is an immediate, visible child of the configured repository and contains exactly `docker-compose.yml`. Check active/archived filters and any search text. Files named `compose.yaml`, hidden directories, and deeper nested projects are not discovered as stacks.

Correct the layout without overwriting an existing stack, then refresh the inventory. Expect its active stack entry to appear. Deleting a stack removes its files and archives metadata; archived metadata is not an undelete function.

## Deployment fails

Read the failed operation output, then select **Validate config**. Check YAML, image access, interpolation values in **Settings → Environment**, published-port conflicts, and daemon-visible bind-mount paths. Unsupported helper-dependent Compose features can fail before Docker changes.

Correct and save configuration, review a new deployment, and follow it to completion. Expect both a successful operation and the intended container/health state; a running container without a healthcheck does not prove the application is ready. If partially changed, inspect the existing containers before retrying. Porty does not promise automatic rollback.

## File save or deployment review is stale

Another edit changed the file or reviewed configuration. Preserve your local edits, reload the current file, compare, and save again. For a rejected deployment confirmation, inspect the current configuration and choose **Review again**. Expect a fresh review to succeed only while its source remains unchanged.

See [editing](user-guide.md#compose-and-files) and [deployments](user-guide.md#deployments).

## Pull or push is unavailable or rejected

Confirm a managed remote is configured and save/discard unsaved editor changes. Commit intentional saved changes before pulling. Fetch current remote information; a pull must be a fast-forward, and a push cannot overwrite diverged remote history.

Resolve divergence in a separate checkout, protect Porty's data with a backup, and reconcile deliberately. Do not discard files or force remote history merely to remove the error. Once the local worktree and history are suitable, retry and verify branch/ahead/behind state. Synchronization does not deploy stacks.

## Metrics are unavailable

Open the metric's details. In native mode, check host/device access; in OCI host mode, check the documented read-only `/proc`, `/sys`, and root filesystem mounts. Missing mounts never fall back to container metrics. GPU metrics need the relevant driver, libraries, and narrowly scoped access.

Apply only the access required by [Host monitoring](operator-guide.md#host-monitoring), then restart and inspect readings. Some hardware/environments remain unsupported. Unavailable should become a measured value only when the source is supported and accessible; it must not be interpreted as zero. `monitoring.mode: disabled` intentionally stops collection.

## Automatic updates are skipped or paused

Read the stack's automatic-update eligibility message and last outcome. Check UTC schedule, enabled state, healthy/running deployment, unchanged source, supported public tag, and Docker API support. Sleeping, stopped, drifted, or otherwise ineligible stacks are skipped.

If paused after a mutation failure, inspect operations and alerts, recover manually, then use **Verify recovery and resume**. Expect the pause to clear only after successful verification. Acknowledging an alert, editing the schedule, or enabling the policy does not clear it. See [Failure and recovery](operator-guide.md#failure-and-recovery).

## On-demand wake or sleep does not work

Check the group's held/paused state, endpoint conflicts, published addresses/ports, and deployment requirements. Porty must share the local rootful Docker host's network namespace; a reachable remote/Desktop socket alone is insufficient. OCI deployment needs host networking and an explicit loopback UI bind. UDP host and container ports must match.

Review the operation, recover the containers manually, and select **Resume** to revalidate. Expect a verified sleeping group to reserve its endpoints and wake on qualifying traffic. The initial request is discarded; clients must retry. Background traffic and monitoring can prevent sleep. See [requirements](on-demand.md#deployment-requirements) and [holds/recovery](on-demand.md#holds-recovery-and-other-operations).

## Restore or roll back

Follow [Backup and restore](operator-guide.md#backup-and-restore) and [Upgrade and rollback](operator-guide.md#upgrade-and-rollback). Stop Porty before capturing/restoring its data, preserve ownership, and restore matching binary/data versions for rollback. Application volumes require separate application-appropriate backups. Verify readiness, repository state, and applications after recovery.
