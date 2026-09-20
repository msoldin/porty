# Porty security model

Porty's authenticated administrator and every accepted Compose definition have Docker-equivalent host authority. Porty prevents unintended path and command injection; it does not sandbox Compose capabilities or protect the host from a malicious administrator.

The editor is rooted in the configured repository. It rejects traversal, `.git`, symlinks, special files, oversized files, and stale writes. Git and Docker are started directly with explicit arguments and clean environments; Porty never constructs shell commands. Adopted repositories with command-bearing Git configuration must be rejected before use.

Passwords use Argon2id. Random session tokens are stored only as hashes. Browser mutations require a same-origin request and the session-bound CSRF token and are recorded in the audit history. Environment values are write-only through the API, persist in SQLite, and are materialized only in temporary mode-`0600` files for Compose. Process and operation output is bounded and redacted before it reaches persistence or streaming clients. Rendered Compose configuration and container logs are not persisted; log snapshots are sent only to authenticated WebSocket subscribers.

Keep `/var/lib/porty` at mode `0700`, configuration at `0640` or stricter, backups at `0600`, and the HTTP listener on loopback unless a trusted network design says otherwise. Treat access to the Docker socket, Porty's database, repository, backups, process environment, and administrator browser session as privileged access.

Report a suspected vulnerability privately to the repository owner. Include the affected version, reproduction steps, and impact; do not include live credentials or environment values.
