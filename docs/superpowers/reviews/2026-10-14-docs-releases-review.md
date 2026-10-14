# Documentation and release implementation review

Reviewed 2026-10-14, branch `feat/docs-releases`, base `0bae68d`, implementation through `7b3472f`.

Local implementation is complete. GitHub-hosted verification, Pages configuration/deployment, tag rules, GHCR visibility, and the first candidate/stable publication remain rollout work. No remote tags, releases, packages, or settings were changed.

## Result

- Existing guides build as a searchable VitePress site under `/porty/`, with desktop/mobile navigation, development-version notice, preserved anchors, and internal design content excluded.
- Pages builds on relevant PRs and main changes; only successful main push/manual runs can deploy. Deploy permissions are confined to the deploy job.
- Version tags select the exact source commit and run the existing verification workflow. Release builds produce Linux amd64 CGO binaries in the pinned Bookworm builder and a matching container.
- Publication preserves the tested build bundle, binds drafts to its manifest hash, verifies existing assets, refuses conflicting images, records the digest, pulls/verifies it, and publishes last. Published reruns are read-only.
- Guides explain installation/checksums, runtime support, version selection, upgrades/rollback, settings, and each interrupted-publication state.

## Verification

| Check                                                   | Result                                                                                            |
| ------------------------------------------------------- | ------------------------------------------------------------------------------------------------- |
| Go race suite, vet, normal build                        | Passed                                                                                            |
| CGO-disabled monitoring tests and build                 | Passed                                                                                            |
| sqlc regeneration                                       | Passed, no generated diff                                                                         |
| Frontend tests/types/build                              | 299 tests passed; types/build passed                                                              |
| Application browser suite                               | 56 desktop/mobile tests passed                                                                    |
| Release policy, bundle, registry, and publication tests | 18 passed                                                                                         |
| Documentation unit/build/browser tests                  | Passed, including all 8 browser checks                                                            |
| Source Markdown links and fragments                     | 123 total, zero errors; external links excluded from offline check                                |
| Workflow lint and GoReleaser configuration              | Passed                                                                                            |
| Snapshot release build and live OCI packaging gate      | Passed                                                                                            |
| Clean tagged build                                      | Passed for `v0.1.0-rc.1`, with a conflicting newer `v99.0.0` tag on the same local fixture commit |
| Real build-bundle transfer                              | Manifest/hashes validated; saved image reloaded with identical image ID                           |
| Negative artifact checks                                | Tampered checksum and stale embedded UI fixtures rejected                                         |

The clean tagged fixture used an independent temporary clone and never pushed its tags. Native/container output exactly matched the selected tag, full SHA, and UTC commit timestamp. Both reached readiness and served the fresh index/assets in the Bookworm runtime. Non-root identity, restrictive data modes, glibc linkage, and startup without NVML passed.

Application browser validation initially found a stale Settings expectation after the pre-existing sign-in redirect change. The test now expects Stacks, checks its URL, and checks the persisted theme; the application is unchanged. An initial alert-server startup failure passed in isolation and in the subsequent full suite.

## Review decisions and deviations

1. Review and execution stayed inline because repository instructions prohibit subagents. Cost: no independent reviewer; hosted review remains valuable.
2. Used Playwright directly because no Browser plugin was available. Pinned Playwright 1.63.0 matches the installed browser.
3. Added `github-slugger` 2.0.0 to preserve numeric heading anchors that VitePress otherwise changes. This adds a small docs-only dependency.
4. Implemented tag policy with packaging because clean release builds require it. Workflow gating/publication landed together after their shared tests passed, rather than committing an intermediate build-only release workflow.
5. Developer snapshots permit local edits in an isolated checkout. Their SHA identifies the checkout base; only release mode requires clean source and exact tag identity. This avoids pretending snapshots attest to committed source.
6. GoReleaser skips its generic publication/validation stages after explicit clean/tag checks because frontend rebuilding can change tracked assets. Configuration validation, compilation, artifact checks, and smoke tests still run.
7. Returned builder-generated `dist/` ownership to the invoking UID/GID so Docker can save the image. Source/Git metadata are not chowned; Git metadata is mounted read-only.
8. Reused existing packaging/monitoring checks without redundant changes to `package_test.sh`.
9. Corrected the stale browser expectation required by the current base commit. Cost if wrong: a test would encode the wrong route; both source and browser trace confirmed Stacks.
10. Added the manifest-hash marker at draft creation after review exposed empty-draft retry substitution. The new test failed before the fix, then passed. A mismatched build now requires its original bundle or a new version.

Review covered the version fast path, selected-tag identity, fresh UI/glibc boundaries, interrupted publication, expired artifacts, registry errors, stable/prerelease ordering, and permissions/event gates. No unresolved code findings remain. GitNexus was refreshed and change analysis run; its bounded flow index and unresolved dynamic/workflow references were supplemented by direct source review, not treated as evidence of absence.

The whole-branch graph comparison reports high structural impact because `main` reaches 11 startup flows (configuration, authentication, monitoring, migrations, and HTTP serving). The change adds only the standalone version exit before these flows. Invalid-configuration/no-state regression tests, the full Go/browser suites, and native/container readiness checks cover both the new exit and normal startup.

## Limits and rollout

Local fake API tests do not prove GitHub/GHCR permissions or service behavior. Remote rollout must verify anonymous downloads/pulls, published rerun immutability, package visibility, and Pages paths. Existing workload/hardware test limitations remain in the development guide; the privileged nested-mount fixture was skipped, as reported by the existing packaging test.

Desktop/mobile documentation screenshots are in `/tmp/porty-docs-tests/`; application browser evidence is in `/tmp/porty-release-browser-final/`. The workflow uploads documentation screenshots for PR review. These local artifacts are not committed or published with the site.

Follow [the release rollout](../../releasing.md#first-release-rollout) after integration. The first tags are deliberate maintainer actions, not side effects of this implementation review.
