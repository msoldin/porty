# Configuration reference

[Documentation home](README.md)

Porty reads configuration at startup. Restart it after changing its configuration file, process environment, or command-line flags. Stack settings edited in the browser are separate from this server configuration.

## Sources and precedence

From lowest to highest priority: built-in defaults → YAML file selected with `--config` → environment variables → explicitly supplied flags. There is no automatically selected configuration file. The supplied systemd unit passes `--config /etc/porty/config.yaml`.

```sh
porty --config /etc/porty/config.yaml
```

Start with [the example YAML](../deploy/porty.example.yaml). The runtime binary accepts these settings:

| YAML key                  | Environment variable            | Flag           | Built-in default  |
| ------------------------- | ------------------------------- | -------------- | ----------------- |
| `server.listen`           | `PORTY_LISTEN`                  | `--listen`     | `127.0.0.1:8080`  |
| `server.tls_cert`         | `PORTY_TLS_CERT`                | `--tls-cert`   | Empty             |
| `server.tls_key`          | `PORTY_TLS_KEY`                 | `--tls-key`    | Empty             |
| `server.public_url`       | `PORTY_PUBLIC_URL`              | `--public-url` | Empty             |
| `data_dir`                | `PORTY_DATA_DIR`                | `--data-dir`   | `/var/lib/porty`  |
| `log_format`              | `PORTY_LOG_FORMAT`              | `--log-format` | `text`            |
| `max_editable_file_bytes` | `PORTY_MAX_EDITABLE_FILE_BYTES` | None           | `1048576` (1 MiB) |
| `monitoring.mode`         | `PORTY_MONITORING_MODE`         | None           | `native`          |
| `monitoring.host_proc`    | `PORTY_MONITORING_HOST_PROC`    | None           | `/host/proc`      |
| `monitoring.host_sys`     | `PORTY_MONITORING_HOST_SYS`     | None           | `/host/sys`       |
| `monitoring.host_root`    | `PORTY_MONITORING_HOST_ROOT`    | None           | `/host/root`      |

The example YAML chooses `json` logging. The OCI image sets `PORTY_MONITORING_MODE=host` and supplies command arguments for `--listen 0.0.0.0:8080 --data-dir /var/lib/porty --log-format json`. Those arguments override corresponding environment variables; pass replacement command arguments after the image name when changing them.

## Network and TLS

`server.listen` is the bind address. Keep the UI on loopback or behind an appropriate trusted network boundary. For direct TLS, set both certificate and key paths; setting only one is rejected.

Behind a TLS reverse proxy, set `server.public_url` to the exact external origin, for example `https://porty.example.com`. Use a lowercase host and omit trailing slash, path, query, fragment, credentials, and explicit default port. Porty uses the origin for browser request checks and Secure cookies; forwarding headers are ignored. See [TLS proxy guidance](operator-guide.md#browser-authentication-and-tls-proxies).

## Storage and file limits

| Location                                       | Contents                                                                                                          |
| ---------------------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `<data-dir>/porty.db` and SQLite sidecar files | Account/authentication state, stack metadata, environment values, operation and other persisted application state |
| `<data-dir>/repository`                        | Managed repository and stack files                                                                                |
| `<data-dir>/ssh/id`                            | Optional mounted repository SSH identity                                                                          |
| `<data-dir>/ssh/known_hosts`                   | Optional trusted SSH host keys                                                                                    |

Use an absolute data-directory path and preserve the whole directory with the documented [backup procedure](operator-guide.md#backup-and-restore). Porty restricts it to mode `0700` and refuses the filesystem root. Configuration should be `0640` or stricter; backup archives should be `0600`. Application Docker volumes and external bind mounts need their own backups.

`max_editable_file_bytes` must be positive. Raising it does not disable rooted-path, symlink, special-file, or stale-write checks. Environment values saved through Porty are kept in SQLite and are not automatically Git-tracked.

## Monitoring

Use `native` when Porty runs on the host, `host` for the documented OCI host mounts, or `disabled` to stop collection. Host paths must be absolute. Missing host mounts do not silently fall back to container metrics. See [Host monitoring](operator-guide.md#host-monitoring) for mount and optional GPU access requirements.

## Password reset

`PORTY_RESET_PASSWORD` is used only by the offline `reset-password` subcommand. Stop the service first and follow [the reset procedure](operator-guide.md#repository-setup); a reset revokes existing authentication. Do not put passwords or tokens into committed configuration files.
