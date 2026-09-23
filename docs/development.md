# Porty development

Porty requires Go 1.27.1+, Node.js 24+, npm, Git, and Linux. Docker is optional for the contract tests and required only for the live Compose and OCI gates.

SQLite migrations run automatically at startup through goose using the embedded SQL files in `internal/sqlite/migrations/`. This development version does not upgrade databases created by the previous custom migration runner. Before starting this version with an existing local installation, stop Porty and remove or recreate the local `porty.db` file and its `-wal`/`-shm` companions in the configured data directory. Porty never deletes the database automatically.

For a TLS reverse proxy, set `server.public_url` to the external origin (for example, `https://porty.example.com`). Porty uses that origin for CSRF checks and marks authentication cookies Secure. Direct TLS and local HTTP use the request origin when this setting is empty. Access cookies expire after 15 minutes; the browser refreshes them with a rotating token whose original login expires after seven days. Logout revokes the refresh family, while an access token already issued at logout can remain valid for at most 15 minutes. Password changes and resets rotate the signing key and revoke all refresh families immediately.

```sh
npm --prefix web ci
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
go test ./...
go test -race ./...
go vet ./...
```

The Go binary embeds `web/dist`; rebuild the frontend before building a release binary. Run the browser journey after building `/tmp/porty-e2e`:

```sh
go build -o /tmp/porty-e2e ./cmd/porty
PORTY_E2E_BINARY=/tmp/porty-e2e npm --prefix web run test:e2e
```

The browser test starts a real Porty process and Git repository. It puts a fake `docker` executable first on that process's `PATH`, so no workload or daemon is touched. Run `PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh` on a disposable Docker-enabled builder to verify the OCI image.

Tests that open an HTTP listener, run processes, or use a browser may require permission in restricted sandboxes. Never commit generated Playwright reports, traces, or screenshots.
