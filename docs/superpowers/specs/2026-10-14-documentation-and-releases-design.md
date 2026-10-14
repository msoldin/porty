# GitHub Pages documentation and tagged releases

Date: 2026-10-14

Status: Approved and implemented locally on 2026-10-14. [Implementation review and verification](../reviews/2026-10-14-docs-releases-review.md) recorded; integration and GitHub-hosted rollout remain pending.

## Intent and scope

Publish Porty's existing documentation on GitHub Pages and make installable releases through GitHub Actions. The maintainer chooses a version by pushing a Git tag. Start at `v0.1.0`; do not introduce release PR automation or automatic version bumps. Conventional Commits remain the repository convention.

The website, release archives, and registry image remain part of `msoldin/porty`. Documentation publishing and application releases are independent workflows. Initial distribution targets Linux amd64, preserving the existing CGO-enabled monitoring behavior. ARM64, package repositories, automatic application updates, a custom domain, and a documentation version selector are outside this first delivery.

This specification defines two implementation phases: documentation publishing, then tagged application releases. Each phase must work independently. Planning does not publish a site, create a release tag, or change GitHub settings.

## Existing constraints

- User guides and screenshots already live in `docs/`, with links from the root `README.md`.
- `.github/workflows/verify.yml` already checks generated SQL, frontend tests/types/build, backend tests/vet/build, browser behavior, and packaging including a live Docker gate.
- The Go binary embeds `web/dist`. The current Dockerfile consumes that directory without rebuilding the frontend.
- The container uses Debian Bookworm and `CGO_ENABLED=1`; optional NVIDIA monitoring depends on CGO. A non-CGO build is tested but must not silently replace the normal release build.
- Database migrations are forward-only. Rollback requires the previous executable or image and the corresponding pre-upgrade data backup.
- The current frontend package version is private build metadata, not an independently released product version.

## Documentation website

Use a pinned stable VitePress release with its standard theme, local search, responsive navigation, and existing Porty branding. Keep its tooling in `docs/package.json`, `docs/bun.lock`, and `docs/.vitepress/`; do not add Vue or documentation dependencies to the embedded Preact application's dependency graph. Use the project's Bun version and a supported pinned Node runtime where VitePress requires Node.

Keep the existing guides as the source of truth. Provide navigation for getting started, features, everyday use, configuration, operations, on-demand groups, troubleshooting, security, and development. Preserve Markdown filenames and heading anchors. Adapt the documentation home to the site root while keeping existing `README.md` links usable. Links outside the documentation tree, such as `../AGENTS.md`, must resolve to the GitHub repository in the website build without breaking Markdown browsing on GitHub.

Publish only the guides, required images, and selected branding assets. Exclude `docs/superpowers/`, design mockups, branding previews, and generation metadata from both generated pages and copied public assets. Do not hide excluded content merely by removing sidebar entries.

The default URL is `https://msoldin.github.io/porty/`, with base path `/porty/`. Use explicit `.html` page links as needed for static hosting, and verify direct navigation and reloads. The site carries a visible "Development documentation — main branch" notice with a Releases link. Each application release links to its tagged `docs/README.md` on GitHub for version-matched instructions. No claim is made that the website describes the latest released binary.

### Publishing and validation

Add `.github/workflows/docs.yml`. Pull requests build and validate the site without deployment permissions. Pushes to `main` affecting documentation or its build inputs, and an explicit manual run on `main`, build and deploy through the official GitHub Pages artifact actions. Configure repository Pages to use GitHub Actions and use the `github-pages` environment. Serialize deployment and do not cancel an active deployment midway.

Grant `contents: read` for builds; grant `pages: write` and `id-token: write` only to deployment. Pin dependencies and action revisions. Never deploy from pull requests. A failed build leaves the previous published site intact.

Validate Markdown links and anchors, production output links/assets under `/porty/`, excluded content, direct page reloads, search, and desktop/mobile navigation. Preserve checks for links back to the repository. Capture desktop and mobile screenshots for the implementation PR. Generated output and caches remain ignored.

## Version contract

The Git tag is the authoritative release version. Accept `vMAJOR.MINOR.PATCH` and SemVer prerelease suffixes such as `v0.2.0-rc.1`; reject malformed versions and build metadata suffixes (`+...`) in this initial release workflow. Validate SemVer rules, including leading zeros, independently of the Actions tag glob.

For the initial `0.x` series, patches contain fixes and minor releases contain features or breaking changes. Every breaking change needs a migration or recovery note. Reaching `1.0.0` is a deliberate maintainer decision about compatibility, not an automatic consequence of a commit type. Prereleases are marked as such and never become the latest stable release.

Add `porty --version` with version, commit SHA, and build date supplied at build time. It must exit successfully before reading configuration, opening a database, creating directories, or contacting Docker. Ordinary developer builds report `dev` and explicit unknown metadata where unavailable. Keep this small and local to `cmd/porty`; do not add an HTTP endpoint or a shared backend subsystem solely for build metadata. The frontend ships with the same binary and needs no separate version bump.

## Tag driven release workflow

Add `.github/workflows/release.yml`, triggered by pushed `v*` tags. A maintainer creates an annotated tag on a merged commit and pushes that exact tag:

```sh
git tag -a v0.1.0 -m "Release v0.1.0"
git push origin v0.1.0
```

Resolve the tag to a full commit SHA and verify that commit is reachable from `origin/main`. Both annotated and lightweight tags must resolve correctly, although maintainer instructions use annotated tags. Check out and build the resolved commit throughout the workflow. Reject a tag pointing outside `main` history, an invalid version, or any attempt to replace a published release.

Reuse the existing verification workflow through `workflow_call`, preserving normal push and PR verification. The release must wait for checks on its exact commit, including the live Docker gate; a successful run on a different `main` commit is insufficient. Avoid duplicate tag-triggered verification by making standalone verification branch/PR triggers explicit. Grant publishing permissions only to jobs that publish.

Build the frontend from the tagged lockfile before building either the native executable or image. Do not trust the checked-in `web/dist` as release input. Build the native executable with CGO enabled in a pinned Debian Bookworm environment; document Linux amd64 and glibc 2.36 or newer as the native runtime baseline. Validate the executable in the matching minimal runtime, including startup without NVML. Keep the existing image's user, private data permissions, certificates, timezone data, and monitoring behavior.

Use pinned GoReleaser OSS configuration in `.goreleaser.yaml` for the native build, archive, and checksums, with publishing disabled. After artifact smoke tests, create and populate the draft GitHub Release through the Octokit client supplied by `actions/github-script`. This separation avoids GoReleaser Pro's before-publish hooks and allows retries without rebuilding published artifacts. Keep OCI building/publishing in the established Docker build tooling, with version and revision labels and matching executable metadata. Supply necessary native build tools in the release builder; do not work around CGO by disabling it.

### Release outputs

For `v0.1.0`, produce:

- `porty_0.1.0_linux_amd64.tar.gz`, containing the executable, example configuration, systemd unit, and a short installation reference.
- `checksums.txt`, containing SHA-256 checksums for downloadable release archives.
- `ghcr.io/msoldin/porty:v0.1.0`, with its digest recorded in the release notes.
- A GitHub Release with generated change notes, supported platform/runtime requirements, installation links, known limitations or migration notes, and a link to documentation at the release tag.

Use exact version image tags initially, without moving `latest` or major/minor aliases. Examples pin a version or digest. Publishing an older maintenance version must not displace a newer stable GitHub Release's latest designation; compare stable versions explicitly. Prereleases never get that designation.

Generate change notes through GitHub, with categories in `.github/release.yml`. These notes summarize merged work; Conventional Commits do not by themselves guarantee curated upgrade instructions. Review user-visible upgrade guidance in the code PR before tagging. Do not add a second manually synchronized version file or changelog solely for release automation.

### Publication and recovery

Build and smoke-test all outputs before external publication. Prepare a draft GitHub Release, attach and verify its complete asset set, push and verify the versioned OCI image, then publish the GitHub Release as the final step. Use the workflow's repository token for GitHub Releases and GHCR; no additional personal access token is required for a human-pushed tag. Configure package visibility so anonymous users can pull the public image and verify this during rollout.

Serialize runs for each tag without cancelling an active publication. A failed build publishes nothing. Failure after uploading assets leaves a draft; failure after pushing an image may leave an image without a published GitHub Release because the two services cannot be updated atomically. Document that distinction.

Retries operate on the same tag and commit. Reuse a matching draft and its validated assets. An existing image must match the original recorded digest and revision before reuse; never overwrite a version tag to accommodate a new build. Preserve enough build records to establish that match, or stop with a recovery instruction if provenance is unavailable. A rerun after successful publication performs verification only and cannot replace assets. Code or packaging fixes require a new version tag. Published tags never move.

## Documentation changes accompanying implementation

Update `README.md` and `docs/README.md` with the website and Releases links. Update `docs/getting-started.md` and `docs/operator-guide.md` for binary downloads, checksum verification, published images, runtime requirements, and upgrades. Retain source build instructions for contributors and unsupported platforms. Preserve the requirement to restore both old software and pre-upgrade data when rolling back migrations.

Add `docs/releasing.md` for version selection, tag creation, prereleases, release checks, GitHub configuration, generated notes, failed-run recovery, and the prohibition on moving published tags. Link it from the documentation index and `docs/development.md`. Document local documentation build/preview commands and the main-versus-tagged documentation policy in the development guide. These user/developer guides, not this specification, own the operational instructions.

## Verification and rollout

Implementation must add failing regression coverage before application behavior changes. Required cases include version reporting without side effects; malformed and prerelease tags; exact commit selection; CGO startup without NVIDIA libraries; fresh frontend embedding; checksum/archive contents; publication blocked by failed checks; retries against drafts and published releases; and preservation of newer stable/latest status.

Run existing backend, frontend, browser, and packaging gates for release changes. Validate workflows with a maintained workflow checker, perform a local GoReleaser snapshot build without publication, and exercise the production docs build under the project subpath. Mocked publication tests must verify ordering and retry behavior; they do not substitute for GitHub-hosted validation.

Roll out documentation first, then release automation. Configure Pages and version-tag protection on GitHub after the implementation is reviewable. Verify publishing permissions and GHCR visibility. Exercise the full workflow with `v0.1.0-rc.1`, including anonymous image pull, binary download/checksums, readiness, and version reporting. After validation, tag the chosen commit as `v0.1.0`. Initial tags and publication are explicit rollout actions after implementation review.

## References

- [VitePress deployment](https://vitepress.dev/guide/deploy)
- [GitHub Pages custom workflows](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages)
- [GitHub Actions workflow syntax](https://docs.github.com/en/actions/reference/workflows-and-actions/workflow-syntax)
- [GitHub generated release notes](https://docs.github.com/en/repositories/releasing-projects-on-github/automatically-generated-release-notes)
- [GoReleaser release configuration](https://goreleaser.com/customization/publish/scm/)
- [GoReleaser CGO constraints](https://goreleaser.com/resources/limitations/cgo/)
- [Semantic Versioning](https://semver.org/)
