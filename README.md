# Porty

Porty is a single-server Docker Compose and Git manager for one administrator. A Go service owns filesystem, Git, Compose, operation, and authentication behavior; the embedded Preact interface provides stack management and a CodeMirror editor.

Porty targets Linux and one repository on one configured branch. Compose stacks are immediate, visible child directories containing exactly `docker-compose.yml`.

See the [operator guide](docs/operator-guide.md) for installation, backup, upgrade, and OCI usage; [security model](docs/security.md) for trust boundaries; and [development guide](docs/development.md) for verification commands.
