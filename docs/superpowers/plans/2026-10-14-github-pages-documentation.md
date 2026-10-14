# GitHub Pages Documentation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:executing-plans to implement this plan task-by-task. Execute inline without subagents, as required by AGENTS.md. Steps use checkbox syntax for tracking.

**Goal:** Publish the existing Porty guides as a searchable GitHub Pages site while retaining their usefulness on GitHub.

**Architecture:** Build the existing Markdown with an isolated VitePress package under `docs/`. Validate pull requests without publication credentials; deploy successful `main` builds with GitHub's Pages artifact actions.

**Tech Stack:** VitePress 1.6.4, Bun 1.4.2, Node 24 LTS, Playwright, Node's built-in test runner, GitHub Actions.

**Spec:** [Approved documentation and releases design](../specs/2026-10-14-documentation-and-releases-design.md), documentation sections. The [release plan](2026-10-14-tagged-releases.md) is independent and follows this phase.

## Global Constraints

- The default URL is `https://msoldin.github.io/porty/`, with base path `/porty/`.
- The site carries a visible "Development documentation — main branch" notice with a Releases link.
- Preserve Markdown filenames and heading anchors.
- Never deploy from pull requests.
- Generated output and caches remain ignored.
- Keep documentation dependencies out of `web/package.json`; preserve existing frontend worktree changes.
- Resolve Node 24's supported patch and action commit SHAs from official sources at implementation; record exact pins and commit the Bun lockfile. Do not install VitePress 2 prereleases.
- Before existing symbol edits, run GitNexus impact checks; before each commit run `detect_changes(scope: "all")`, then inspect the staged-only scope. Graph UNKNOWN requires source confirmation, not an assumed all-clear.
- Check isolation using the worktree skill before implementation. No product dependencies or workflows are installed during planning.

## Review Focus

1. Root-hosted previews can conceal broken `/porty/` links: test the production subpath (Task 1).
2. Rewriting README routes can break existing links, fragments, and GitHub browsing: test both source and generated destinations (Task 1).
3. Removing sidebar entries does not exclude internal files from the artifact: inspect the output tree (Task 1).
4. A manually dispatched workflow on a feature branch must not publish: test its deployment condition (Task 2).
5. Failed or overlapping builds must not replace the live site with partial output: separate build/deploy and serialize deployment (Task 2).

## Task 1: Build and validate the documentation site

**Files:** Create `docs/package.json`, `docs/bun.lock`, `docs/.vitepress/config.ts`, `docs/.vitepress/links.mjs`, `docs/.vitepress/links.test.mjs`, `docs/.vitepress/theme/index.ts`, `docs/.vitepress/theme/style.css`, `docs/playwright.config.ts`, `docs/tests/site.spec.ts`. Modify `.gitignore`, `.dockerignore`, `README.md`, `docs/README.md`, `docs/development.md`.

**Interfaces:** Package scripts `dev`, `build`, `preview`, `test:unit`, `test:site`; production output `docs/.vitepress/dist/`; preview `http://127.0.0.1:4174/porty/`. `rewriteDocLink(href: string): string` in `links.mjs` transforms only known repository/home links, preserving query strings and fragments. The Markdown renderer applies it to link tokens, never source files or code fences.

- [ ] **Write failing link and browser regressions.** Use Node's built-in runner for `links.test.mjs`. Required assertions:

  ```js
  assert.equal(rewriteDocLink("README.md#daily-use"), "index.md#daily-use");
  assert.equal(
    rewriteDocLink("../AGENTS.md"),
    "https://github.com/msoldin/porty/blob/main/AGENTS.md",
  );
  assert.equal(
    rewriteDocLink("../README.md#porty"),
    "https://github.com/msoldin/porty/blob/main/README.md#porty",
  );
  assert.equal(
    rewriteDocLink("operator-guide.md#upgrade-and-rollback"),
    "operator-guide.md#upgrade-and-rollback",
  );
  assert.equal(
    rewriteDocLink("https://example.com/README.md"),
    "https://example.com/README.md",
  );
  ```

  Browser cases: `opens every guide directly under the project path`, `keeps guide anchors and images usable`, `finds configuration with local search`, `opens navigation on mobile`, and `excludes internal design content`. Crawl generated same-origin links/assets and check fragment IDs; confirm no `superpowers`, design mockup, branding preview, or generation metadata in output. Use a fixture broken anchor to prove the check fails. Validate source Markdown links against repository files and GitHub-compatible heading slugs using a maintained checker, pinned in CI; do not write a Markdown parser.

- [ ] **Run the failures.** `rtk node --test docs/.vitepress/links.test.mjs` must fail because the helper is absent. After adding only package/test harness configuration, run `rtk bun run --cwd docs build` and `rtk bun run --cwd docs test:site`; the site checks must fail until the site exists. No dependency changes outside the docs package.

- [ ] **Implement the site.** Use VitePress `rewrites: { 'README.md': 'index.md' }`, `base: '/porty/'`, `cleanUrls: false`, local search, and fatal dead-link checking. Exclude `superpowers/**`, `design/**`, `branding/**`, `tests/**`, and dependency/build directories from source discovery. Import only the selected existing brand image through the asset pipeline; do not copy whole source directories into `public/`. Add the notice through the default theme's layout slot. Keep typography/layout close to the standard theme; avoid custom page components.

  Navigation covers all existing top-level guides. Apply the narrow link helper before VitePress resolves links; explicitly map repository-root references encountered in the guides. Preserve original Markdown content and fenced examples. Use existing screenshots and descriptive alt text.

  Ignore docs dependencies, build/cache files, and browser output in both Git and Docker contexts. Update the root README/index with the Pages URL, retaining source-guide links. Add local commands, screenshot refresh, validation, and the development-versus-tagged policy to `docs/development.md`. Mark the website link as planned until the first deployment succeeds.

- [ ] **Verify.** Run `rtk bun run --cwd docs test:unit`, `rtk bun run --cwd docs build`, and `rtk bun run --cwd docs test:site`; expect all assertions to pass. Run the pinned Markdown link checker over the root README and public guides. Test desktop 1440×1000 and mobile 390×844, including reloads, theme contrast, search keyboard focus, and no horizontal overflow. Save screenshots as implementation PR artifacts, outside the published site. Use the frontend-testing skill's Browser-plugin-first policy for visual inspection.

- [ ] **Review and commit.** Check only the listed files are staged, run formatting and graph checks, then commit `docs: build the Porty documentation site`.

## Task 2: Deploy verified documentation with GitHub Pages

**Files:** Create `.github/workflows/docs.yml`. Modify `docs/development.md` and, after deployment, the website-link status in `README.md` and `docs/README.md`.

**Interfaces:** Consume Task 1's scripts/output. Upload a Pages artifact only after validation; deploy that artifact through the `github-pages` environment. No app build or Docker daemon is needed for this workflow.

- [ ] **Validate the workflow contract before publication.** Use `actionlint` plus a small event matrix in the implementation review: PR → build only; relevant `main` push → build/deploy; feature-branch manual run → build only; `main` manual run → build/deploy; failed build → no deployment. Do not add unit tests that merely duplicate YAML.
- [ ] **Implement the workflow and maintainer instructions.** Trigger on PRs and `main` pushes affecting `docs/**`, root README, repository guides linked by docs, and this workflow; also `workflow_dispatch`. Pin actions, install locked dependencies, run unit/build/site/link checks, and upload screenshots on failure. Keep write permissions on the deploy job only. Its condition must require `github.ref == 'refs/heads/main'` and a push/manual event. Use `needs: build`, `environment: github-pages`, `concurrency: { group: pages, cancel-in-progress: false }`, and official configure/upload/deploy Pages actions. Document Settings → Pages → GitHub Actions and the environment's allowed branch.
- [ ] **Verify locally and in the PR.** `rtk actionlint .github/workflows/docs.yml` and all Task 1 commands must pass. Inspect the PR run to confirm it has no deployment or write credentials. The Actions build must inspect the actual uploaded directory for excluded content.
- [ ] **Review and commit.** Run formatting and graph checks, then commit `ci: publish verified documentation to GitHub Pages`.
- [ ] **Roll out after implementation review.** Enable Pages, merge, and inspect the deployment at `/porty/`, a direct guide URL, search, images, and mobile navigation. Record deployment URL and screenshots. A deployment failure leaves the previous site live; fix the build and rerun on `main`. Do not create application release tags in this phase.

## Completion evidence

Record the tested dependency/action pins, successful commands, desktop/mobile screenshots, GitHub build/deployment URLs, and any remote check not yet performed. An approved plan or local preview alone is not a published website.

## References

- [VitePress 1 site configuration](https://vuejs.github.io/vitepress/v1/reference/site-config)
- [VitePress routing](https://vitepress.dev/guide/routing)
- [GitHub Pages workflow requirements](https://docs.github.com/en/pages/getting-started-with-github-pages/using-custom-workflows-with-github-pages)
