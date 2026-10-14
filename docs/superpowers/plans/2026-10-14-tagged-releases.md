# Tagged Releases Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Execute inline without subagents, as required by AGENTS.md. Steps use checkbox syntax for tracking.

**Goal:** Turn a maintainer-pushed version tag into tested Linux amd64 downloads and a version-pinned GHCR image, starting at `v0.1.0`.

**Architecture:** The tag supplies version identity. Reuse verification for its exact commit, build with GoReleaser OSS without publication, smoke-test binaries/images, and publish through a small GitHub workflow helper using the Octokit client supplied by `actions/github-script`. Recover drafts by verifying existing artifacts rather than replacing them.

**Tech Stack:** Go 1.27.1, Bun 1.4.2, GoReleaser OSS 2.18.3, pinned Debian Bookworm builders/runtime, Docker, GitHub Actions, Node 24's built-in tests.

**Spec:** [Approved documentation and releases design](../specs/2026-10-14-documentation-and-releases-design.md), version/release sections. The [documentation plan](2026-10-14-github-pages-documentation.md) publishes the website independently.

## Global Constraints

- The Git tag is the authoritative release version.
- Start at `v0.1.0`; do not introduce release PR automation or automatic version bumps.
- Initial distribution targets Linux amd64, preserving the existing CGO-enabled monitoring behavior.
- Published tags never move.
- Prereleases are marked as such and never become the latest stable release.
- Use exact version image tags initially, without moving `latest` or major/minor aliases.
- Native runtime baseline: Linux amd64, glibc 2.36 or newer; retain the existing Dockerfile's pinned Bookworm base images and non-root/security settings.
- Pin GoReleaser checksums and action SHAs from official sources at implementation; no runtime dependency is added for release tooling.
- Before source edits use current GitNexus impact analysis; before commits use full and staged change analysis. Existing frontend changes are outside this plan. Use an isolated worktree for implementation and preserve them.

## Review Focus

1. `--version` must work with invalid configuration and unusable data/Docker locations without creating state (Task 1).
2. Multiple tags on one commit or a newer tag elsewhere must not change the selected release's identity (Tasks 2–3).
3. Checked-in UI assets and the runner's newer libc must not leak into release artifacts (Task 2).
4. Interrupted publishing, expired build records, or a moved registry tag must never cause silent artifact replacement (Task 4).
5. Concurrent stable/prerelease/maintenance releases must preserve the newest stable latest designation (Task 4).

## Task 1: Report executable build identity without startup

**Files:** Create `cmd/porty/version.go`, `cmd/porty/version_test.go`. Modify `cmd/porty/main.go`, `docs/configuration.md`, `docs/development.md`.

**Interfaces:** Linker variables `main.version = "dev"`, `main.commit = "unknown"`, `main.buildDate = "unknown"`; `printVersion(args []string, out io.Writer) (handled bool, err error)`. Handle only the standalone `--version` invocation, emitting `porty <version> (commit <commit>, built <date>)\n`. Tagged version values retain the leading `v`.

- [ ] **Write failing regressions.** `TestPrintVersionUnderStandaloneFlag` checks exact output with `v0.1.0`, a full commit SHA, and an RFC3339 UTC date. `TestPrintVersionUnderOtherArguments` checks empty args, `reset-password`, and `--version extra` remain unhandled. `TestPrintVersionUnderWriterFailure` checks the error is returned. `TestVersionCommandUnderInvalidRuntimeConfiguration` runs the test executable as a subprocess invoking `main()` with standalone `--version`, invalid Porty configuration, an absent temporary data path, and an unavailable Docker socket; assert exit 0, output, and no new files. This tests integration without building a second binary per case.
- [ ] **Run red.** `rtk go test ./cmd/porty -run 'Test(PrintVersion|VersionCommand)'` must fail for missing behavior.
- [ ] **Implement the branch before `reset-password` and `config.Load`.** Keep formatting and handling in `version.go`; preserve the rest of startup. Report write errors through the existing logging/exit style. Document the command and developer defaults.
- [ ] **Run green.** Repeat focused tests, then `rtk go test ./cmd/porty ./internal/config` and `rtk go vet ./cmd/porty`. Format Go and Markdown. Verify a linker-injected binary prints expected values.
- [ ] **Review and commit:** `feat: report Porty build version` after graph and staged-file checks.

## Task 2: Produce and smoke-test release artifacts

**Files:** Create `.goreleaser.yaml`, `deploy/release-builder.Dockerfile`, `deploy/build-release.sh`, `deploy/release_test.sh`, `deploy/release-install.txt`. Modify `Dockerfile`, `.gitignore`, `.dockerignore`, `deploy/package_test.sh`, `docs/operator-guide.md`, `docs/development.md`.

**Interfaces:** `deploy/build-release.sh snapshot|release` builds into ignored `dist/` and loads local image `porty:release-test`; release mode consumes validated `PORTY_RELEASE_TAG`, `PORTY_RELEASE_COMMIT`, and `PORTY_RELEASE_DATE`. `deploy/release_test.sh DIST_DIR IMAGE_REF` checks native artifacts and the locally built image. Archive: `porty_<version-without-v>_linux_amd64.tar.gz`; checksum list: `checksums.txt`. Docker build arguments: `PORTY_VERSION`, `PORTY_COMMIT`, `PORTY_BUILD_DATE`, defaulting to developer values.

- [ ] **Add failing artifact checks.** In a temporary source fixture, use deliberately stale `web/dist` and assert the built executable serves the freshly rebuilt index and its referenced assets. Assert the archive contains executable `porty`, `porty.example.yaml`, `porty.service`, and `INSTALL.txt`, with no source secrets/database files. Verify checksum tampering fails, executable metadata matches inputs, and both native/image startup reach `/readyz` without NVML. Inspect ELF dependencies and run inside the pinned Bookworm runtime. Check the image's non-root user and private data modes through existing packaging/monitoring tests.
- [ ] **Run red.** `rtk ./deploy/release_test.sh /tmp/porty-missing-release porty:release-test` must report missing artifacts. Run the stale-UI fixture against the current build path and retain the expected failure before changing packaging.
- [ ] **Implement packaging.** Rebuild the UI with frozen Bun dependencies on the host, then build the native archive in the pinned Bookworm builder with GoReleaser 2.18.3 and CGO enabled. Verify GoReleaser's downloaded checksum. Use a deterministic build date derived from the selected commit's timestamp for both native/image metadata. Set `GOAMD64=v1`; package only linux/amd64 and the listed files. The native builder must not inherit host-built Go executables.

  Before rebuilding UI, require a clean tracked source checkout and the requested tag/commit match. Afterwards allow changes only within `web/dist`. Because rebuilding tracked assets makes GoReleaser's generic dirty-tree check fail, invoke `goreleaser release --clean --skip=publish,validate` only after these explicit validations; snapshot mode also uses `--snapshot`. Set `GORELEASER_CURRENT_TAG` to the selected tag in release mode so another tag cannot override it. Still run `goreleaser check`; never skip compilation, tests, or the explicit version/commit checks. Snapshot commands operate on an isolated checkout, not the user's dirty worktree.

  Build the local OCI image from the same fresh UI and source using the existing Dockerfile, with metadata arguments/labels. Save the tested image as `dist/porty-image.tar` for transfer between workflow jobs; record its SHA-256 and local image ID. Use fixed commands and argument arrays where scripts invoke programs. Extend Docker ignores for docs tooling and release output, retaining frontend assets. Keep `.git` excluded from the application image context; mount Git metadata read-only into the native release builder for GoReleaser. Archive/image smoke tests must finish before any registry or GitHub write.

- [ ] **Verify.** `rtk goreleaser check`, `rtk ./deploy/build-release.sh snapshot`, and the new artifact test must pass in an isolated checkout. Run `rtk ./deploy/package_test.sh` and `rtk env PORTY_LIVE_DOCKER_CHECK=1 ./deploy/package_test.sh`. Update operator/development documentation with native compatibility, artifact contents, and local validation; keep download examples marked pending until the workflow ships.
- [ ] **Review and commit:** `build: package versioned Linux release artifacts` after formatting, graph analysis, and artifact inspection.

## Task 3: Validate tags and gate releases on the exact commit

**Files:** Create `.github/scripts/release-policy.cjs`, `.github/scripts/release-policy.test.cjs`, `.github/workflows/release.yml`. Modify `.github/workflows/verify.yml`, `docs/development.md`.

**Interfaces:** `parseReleaseTag(tag)` returns `{tag, version, prerelease}` or throws; `shouldMakeLatest(tag, publishedReleases)` compares valid stable versions numerically, without JavaScript number overflow. Workflow outputs: `tag`, `commit`, `date`, `prerelease`. Reusable verification accepts optional input `ref`; checkout uses that exact SHA when supplied and normal event checkout otherwise.

- [ ] **Write failing policy tests.** Use `node:test` and strict assertions:

  ```js
  assert.deepEqual(parseReleaseTag("v0.1.0"), {
    tag: "v0.1.0",
    version: "0.1.0",
    prerelease: false,
  });
  assert.equal(parseReleaseTag("v0.2.0-rc.1").prerelease, true);
  for (const tag of [
    "0.1.0",
    "v01.1.0",
    "v0.1",
    "v0.1.0+build",
    "v0.1.0-rc.01",
    "v0.1.0\n",
  ]) {
    assert.throws(() => parseReleaseTag(tag));
  }
  ```

  Include long/invalid Docker tags (128-byte limit), huge numeric version components, drafts/prereleases in the release list, `v0.10.0` versus `v0.9.0`, and maintenance releases below the current stable version. Use a temporary Git fixture to test annotated/lightweight tag resolution, two tags on the same commit, and a tag outside main history. No fixture pushes to the real remote.

- [ ] **Run red.** `rtk node --test .github/scripts/release-policy.test.cjs` must fail for missing policy functions.
- [ ] **Implement policy and workflow gates.** Trigger only `push.tags: ['v*']`; validate before publication permissions are available. Fetch tags and `main`, peel the selected tag to its commit, and check `git merge-base --is-ancestor`. Pass all untrusted values through environment variables/SDK parameters, never interpolated shell source. Add `workflow_call` to verify, use explicit branch push triggers to avoid a second standalone tag run, and preserve every existing check including Docker. Release verification must consume the validated SHA. Add policy tests to ordinary CI so release helpers are checked before a tag is pushed.
- [ ] **Verify.** Focused Node tests and `rtk actionlint .github/workflows/verify.yml .github/workflows/release.yml` must pass. Inspect reusable workflow checkout and job dependencies. On the implementation PR, existing verification must still pass; no publishing job runs on a PR.
- [ ] **Review and commit:** `ci: gate tagged releases on verified source` after graph checks. Keep the release workflow build-only until Task 4 is complete.

## Task 4: Publish drafts and images with recoverable retries

**Files:** Create `.github/scripts/publish-release.cjs`, `.github/scripts/publish-release.test.cjs`, `.github/release.yml`, `docs/releasing.md`. Modify `.github/workflows/release.yml`, `docs/README.md`, `docs/development.md`, `docs/getting-started.md`, `docs/operator-guide.md`, `README.md`; add the releasing guide to the VitePress sidebar if the documentation phase is present.

**Interfaces:** `inspectRelease({github, owner, repo, tag, commit})` returns absent/draft/published plus verified existing asset metadata. `prepareDraft({github, owner, repo, manifest, files, notes})` returns its release ID, reusing only byte-identical existing assets. `finalizeRelease({github, owner, repo, releaseId, manifest, imageDigest})` verifies complete remote assets, appends the digest to notes, and publishes once. `github` is the action's Octokit client; tests supply a fake client with recorded reads/writes. Do not create an SDK wrapper framework.

The schema-1 `release-manifest.json` contains `tag`, full `commit`, `buildDate`, `workflowRunId`, archive filenames/SHA-256 hashes, image reference, local image ID, and `imageArchiveSha256`. Persist the manifest, native archive/checksums, and `porty-image.tar` in an immutable Actions artifact before publication, retained for 90 days. Load that saved image in the publish job after checking its hash and ID; do not rebuild it. Store the final registry image digest in the draft's notes in a machine-readable marker and the completed release notes. The manifest is an additional release asset and must not be replaced on retries; the large image tar is CI-only, not a GitHub Release asset.

- [ ] **Write failing publication tests.** Cover new draft upload; existing identical draft reuse without uploads; missing asset resumed from the original build bundle; conflicting commit/checksum rejected before writes; simulated registry failure leaving a draft; a published release causing reads only; pagination over older releases; and latest selection from Task 3. Inject failure after each remote mutation and assert a retry never replaces an existing asset. If an image exists but its original digest record is missing, assert a clear recovery error instead of overwrite. Test an image whose observed digest differs from the recorded digest.
- [ ] **Run red.** `rtk node --test .github/scripts/publish-release.test.cjs` must fail before helpers exist.
- [ ] **Implement the publication stages.** Build/smoke-test under read-only repository permissions. Save the verified build bundle first. On rerun, download that workflow run's original bundle; do not rebuild if a draft/image already exists and the bundle is missing or expired. With no remote outputs, rebuilding after a failed build is allowed. A published release takes a read-only verification path, independent of expired CI artifacts.

  In the publish job, use `contents: write` and `packages: write`, with the repository token and no PAT. Recheck existing release/image state while holding a repository-wide publication concurrency group (`cancel-in-progress: false`) so stable-version selection cannot race; use a separate per-tag build group. Generate GitHub notes with `.github/release.yml` categories and append platform, upgrade, and tagged-guide links. Create the draft and upload exact build-bundle assets through the helpers. Use the tested local image for the initial push, not a fresh unchecked rebuild. For retry after image publication, reuse its recorded digest; refuse any version-tag replacement. Persist the digest immediately after push, then pull/verify it and finalize the GitHub Release. Registry push and note persistence are not atomic: if interrupted between them, stop and document recovery with a new version rather than guessing provenance.

  Re-list stable published releases immediately before finalization; set latest only if this stable version is numerically newer. Existing published releases are never rewritten by reruns. Keep draft/upload/image steps separate from final publication, and enable immutable GitHub Releases during rollout only after confirming drafts accept the complete asset set.

- [ ] **Update operational documentation.** Explain version selection, annotated tags, prereleases, first-release notes, source builds, supported runtime, binary download/checksum verification, exact GHCR tags/digests, backup/upgrade/rollback, Actions permissions, Pages independence, image visibility, rules protecting version-tag updates/deletion, and each retry state above. Link `docs/releasing.md` from index/development and root README. Do not instruct users to delete or move a published tag.
- [ ] **Verify.** Run `rtk node --test .github/scripts/*.test.cjs`, workflow lint, all existing Go/frontend/browser checks, and Task 2's snapshot/artifact/live Docker checks. If the docs phase exists, run its build/site checks. Explicitly distinguish local simulated publication from GitHub-hosted execution.
- [ ] **Review and commit:** `ci: publish verified tagged releases` after graph and staged-file checks.
- [ ] **Roll out after implementation review.** Confirm tag rules allow maintainer creation and prohibit updates/deletion, token permissions, and public GHCR visibility. Publish `v0.1.0-rc.1`; verify anonymous binary download/checksums, anonymous image pull by digest, startup, version output, prerelease/latest flags, and tagged documentation links. Rerun the successful workflow and verify no asset changes. After validation, create `v0.1.0` on the chosen reviewed commit. Record Actions and release URLs; do not publish tags merely while executing this planning task.

## Planning evidence and completion

Source verification covered `cmd/porty/main.go`, the embed declaration, Dockerfile, packaging tests, and current verification workflow. GitNexus resolved startup through configuration and SQLite opening; its MCP index reported one documentation commit behind, so implementation must refresh it before symbol edits. This planning change modifies no application code.

The plan is complete when reviewed; the release feature is complete only after implementation checks and the agreed remote rollout. Record actual test results and unresolved environment requirements without claiming local mocks prove registry publication.

## References

- [GoReleaser OSS dry runs and publishing flags](https://goreleaser.com/getting-started/quick-start/)
- [GoReleaser before-publish hooks require Pro](https://goreleaser.com/customization/publish/beforepublish/)
- [GoReleaser explicit tag selection](https://goreleaser.com/resources/cookbooks/set-a-custom-git-tag/)
- [GitHub workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- [GitHub generated release notes](https://docs.github.com/en/repositories/releasing-projects-on-github/automatically-generated-release-notes)
