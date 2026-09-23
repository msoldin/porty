# Porty development

Porty requires Go 1.27.1+, Bun 1.4.2+, and Linux. The running service uses the Go Git and Docker Compose SDKs, so it needs access to a Docker daemon for stack actions but does not need `git` or `docker` executables. Docker is optional for contract tests and required for the live Compose and OCI gates.

SQLite migrations run automatically at startup through goose using the embedded SQL files in `internal/sqlite/migrations/`. This development version does not upgrade databases created by the previous custom migration runner. Before starting this version with an existing local installation, stop Porty and remove or recreate the local `porty.db` file and its `-wal`/`-shm` companions in the configured data directory. Porty never deletes the database automatically.

For a TLS reverse proxy, set `server.public_url` to the external origin (for example, `https://porty.example.com`). Porty uses that origin for CSRF checks and marks authentication cookies Secure. Direct TLS and local HTTP use the request origin when this setting is empty. Access cookies expire after 15 minutes; the browser refreshes them with a rotating token whose original login expires after seven days. Logout revokes the refresh family, while an access token already issued at logout can remain valid for at most 15 minutes. Password changes and resets rotate the signing key and revoke all refresh families immediately.

```sh
(cd web && bun ci)
(cd web && bun run test)
(cd web && bun run typecheck)
(cd web && bun run build)
go test ./...
go test -race ./...
go vet ./...
```

SQLite read queries are generated with [sqlc v1.31.1](https://github.com/sqlc-dev/sqlc/releases/tag/v1.31.1) from `internal/sqlite/migrations/` and `internal/sqlite/queries/`. The migration directory is the only schema source. Install that exact version and run `sqlc generate` from the repository root. The generated files in `internal/sqlite/generated/` are committed; CI regenerates them and rejects any diff. Nullable database columns use sqlc's `sql.Null*` defaults so the SQLite stores can map them explicitly to domain values. Transactional writes remain handwritten where they enforce cross-table behavior.

The Go binary embeds `web/dist`; rebuild the frontend before building a release binary. Run the browser journey after building `/tmp/porty-e2e`:

```sh
go build -o /tmp/porty-e2e ./cmd/porty
(cd web && PORTY_E2E_BINARY=/tmp/porty-e2e bun run test:e2e)
```

The browser test starts a real Porty process and Git repository. It intercepts Docker-backed API responses in the browser, so no workload or daemon is touched. Run `PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh` on a disposable Docker-enabled builder to verify the OCI image.

The SDK uses a new normalized Compose digest. A stack deployed before this migration may show pending changes once; redeploying records the new digest and restores the current state.

Tests that open an HTTP listener, run processes, or use a browser may require permission in restricted sandboxes. Never commit generated Playwright reports, traces, or screenshots.
