# Releasing Porty

[Documentation home](README.md) · [Development](development.md)

Porty uses manually selected Git version tags. The first stable release is `v0.1.0`; validate the pipeline with `v0.1.0-rc.1` first. Release automation is ready for review, but these example tags and public downloads are pending the first hosted rollout.

## Choose a version

Use `vMAJOR.MINOR.PATCH`, optionally followed by a SemVer prerelease such as `v0.2.0-rc.1`. Tags must fit Docker's 128-character limit. Leading zeroes in numeric identifiers and `+build` metadata are rejected. Prereleases never become the latest stable GitHub Release. During `0.x`, document breaking changes explicitly and use a minor bump for them; use patches for compatible fixes.

The tag supplies one application version for the binary, embedded UI, archive, and container. Conventional Commits help describe changes but do not trigger version bumps or release PRs. Only Linux amd64 is initially distributed; native binaries require glibc 2.36+. Containers use the pinned Debian Bookworm runtime. See [installation](operator-guide.md#release-binary) for verification and supported runtime details.

## Create a release

Merge and review the intended changes on `main`, ensure verification passes, and review upgrade notes and the existing live-test limitations in the development guide. From a clean maintainer checkout:

```sh
git fetch origin main --tags
git switch main
git pull --ff-only origin main
git status --short
git tag -a v0.1.0-rc.1 -m "Porty v0.1.0-rc.1"
git push origin refs/tags/v0.1.0-rc.1
```

Choose the intended commit before tagging; never move or reuse a published version. Use the same procedure with `v0.1.0` after the candidate has passed rollout checks. Annotated tags are preferred; lightweight tags are accepted. Multiple version tags may reference the same commit. The selected tag is authoritative even if another newer tag exists.

Watch the **release** Actions run. A tag must be valid and its peeled commit reachable from `origin/main`. The workflow reuses all existing verification checks against that exact SHA, including frontend/browser and live OCI checks. It rebuilds the frontend, builds native code in the pinned Bookworm builder, and tests both distribution formats before publishing anything. A moving tag is rejected when rechecked before publication.

The workflow first stores the tested archive, checksums, image tar, and manifest in an immutable Actions artifact for 90 days (subject to repository retention limits). It then creates a draft, uploads verified assets, pushes the tested image, records its registry digest, pulls it by digest, checks identity/version, and publishes the completed release. No rebuild occurs in the publishing job. Generated notes use `.github/release.yml` categories and link to the selected tag's documentation.

The GitHub Release contains:

- `porty_<version>_linux_amd64.tar.gz`: executable, example configuration, systemd unit, and installation pointers.
- `checksums.txt`: SHA-256 of the native archive.
- `release-manifest.json`: source commit/date, workflow run ID, artifact hashes, saved image hash, and image ID.

The image is `ghcr.io/msoldin/porty:<exact-version-tag>`. The release notes record its digest. No `latest`, major, or minor container aliases are moved. GitHub's latest release designation advances only for a numerically newer stable version. Publishing an older maintenance version or prerelease leaves it unchanged.

## Retry and recovery

Use **Re-run failed jobs** or **Re-run all jobs** on the original tag-triggered run. Do not push the tag again or delete it. A workflow rerun keeps the run ID, allowing the original build bundle to be restored.

| State                                                                                                 | Recovery                                                                                                                                           |
| ----------------------------------------------------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------- |
| Verification/build failed with no remote outputs                                                      | Fix the problem and use a new version if source changes are needed. A transient failure can rerun and build again.                                 |
| Original bundle saved; no draft yet                                                                   | Rerun restores that bundle.                                                                                                                        |
| Draft exists with some assets                                                                         | Rerun verifies existing bytes and uploads only missing assets from the original bundle.                                                            |
| Image push failed before publishing a manifest                                                        | Rerun may push the saved tested image if the registry confirms the tag is absent.                                                                  |
| Image exists and its digest was recorded in the draft                                                 | Rerun verifies/reuses that digest, pulls the image, and finishes the release.                                                                      |
| Image exists but digest was never recorded                                                            | Stop. Push and recording are not atomic; provenance is uncertain. Leave the failed version unpublished and create a new version.                   |
| Registry digest changed, asset bytes differ, source identity conflicts, or an unexpected asset exists | Stop and investigate. Automation does not overwrite, remove, or repair conflicting outputs. Use a new version for corrected artifacts.             |
| Draft exists but the original Actions artifact expired or was deleted                                 | Do not rebuild under that version. Use a new version.                                                                                              |
| Release already published                                                                             | Rerun only reads/verifies release assets and the registry image, even if the Actions artifact expired. It never updates the release or its assets. |

An incomplete GitHub asset left by an interrupted server upload is treated as a conflict; it is not deleted automatically. Authentication, rate-limit, and registry errors stop publication rather than being interpreted as a missing image. The draft records the manifest hash when created, so even an empty draft is bound to its original tested build. Keep the source/build/digest markers in draft notes intact. Do not manually publish a partial draft.

Release updates explicitly include `tag_name` when recording the image digest and publishing: omitting it can detach a draft from its version and leave it with an `untagged-…` name. If an older workflow stops with `Release identity conflict`, inspect the draft's tag association before retrying. A rerun uses the original tagged scripts, so a fix on `main` alone does not repair that run.

For a detached draft, preserve the existing Git tag, assets, and image. Manual recovery is appropriate only after verification/build/image-pull steps succeeded, the tag still resolves to the manifest commit, uploaded asset digests match the original Actions bundle, and the recorded image digest matches the verified image. Then edit the existing draft, select the existing version tag, preserve its notes and assets, and publish it with the correct prerelease/latest settings. Run the documentation workflow on `main` afterward. If those checks cannot establish provenance, leave the version unpublished and use a new version containing the fix.

Publication jobs are serialized across versions so stable-version selection cannot race within this workflow. GitHub can replace an older pending concurrency job with a newer one; rerun the canceled run when needed. Build jobs are serialized per tag. Avoid manual registry writes or other release automation that bypasses these controls; GHCR does not provide a transaction spanning the push and GitHub Release update.

## First release rollout

After implementation review and branch integration:

1. Configure repository tag rules for `v*`: allow authorized maintainer creation while prohibiting updates and deletion. Use separate restrictions if creation permissions need different bypass settings. Protect `main` and review workflow changes.
2. Confirm Actions policy permits the pinned actions and `GITHUB_TOKEN` writes to contents/packages. Builds use read-only repository permissions; only the publication job requests writes. No personal access token is required. Existing packages must grant this repository Actions access.
3. Push `v0.1.0-rc.1`. After the first image push, set the GHCR package visibility to **public** and verify repository inheritance/access. New packages may be private even for a public repository.
4. Verify anonymous archive download and checksums, extracted `--version`, Bookworm startup, anonymous image pull by recorded digest, matching version/commit, prerelease flags, and tagged-guide links. Confirm it did not become latest stable.
5. Rerun the successful run and confirm release/asset IDs, bytes, image digest, and notes remain unchanged. Record Actions/release URLs and validation results.
6. Enable immutable GitHub Releases after confirming the draft accepts all assets before publication. Then create `v0.1.0` on the reviewed commit and repeat download/pull checks. Keep the Git tag immutable regardless of the GitHub setting.

Configure Pages to deploy from **GitHub Actions**, with the `github-pages` environment restricted to `main`. After the `release` workflow succeeds, the separate documentation workflow rebuilds the site. [msoldin.github.io/porty](https://msoldin.github.io/porty/) serves the newest published stable release's guides; [the next edition](https://msoldin.github.io/porty/next/) follows `main`. Before the first stable release, both serve labeled development docs. Prereleases and older maintenance releases never replace a newer stable edition. Each release still links to its own guides on GitHub for older-version access.

Verify both documentation routes after the first stable release. Pages failures do not change or roll back release artifacts; rerun the documentation workflow on `main` after fixing the cause. For releases published or removed manually outside the release workflow, trigger that documentation workflow manually to refresh the site.

Before upgrading, [back up Porty and its repository](operator-guide.md#backup-and-restore) and separately back up managed application data. Migrations are forward-only: rollback restores the previous executable/image together with the matching pre-upgrade data backup.

Implementation references: [GitHub release API](https://docs.github.com/en/rest/releases/releases), [workflow concurrency](https://docs.github.com/en/actions/how-tos/write-workflows/choose-when-workflows-run/control-workflow-concurrency), and [GHCR access and visibility](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).
