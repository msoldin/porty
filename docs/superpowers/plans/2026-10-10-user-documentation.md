# User Documentation Implementation Plan

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task by task in the current session. Follow `AGENTS.md`: do not use subagents.

**Goal:** Help a new administrator understand Porty's features, install it, deploy a first stack, and operate it confidently from documentation easily reached on GitHub.

**Architecture:** Keep version-controlled GitHub Markdown in `docs/`, with a prominent root README link to `docs/README.md`. Use task-oriented user pages alongside the existing operator, on-demand, security, and development guides; keep each procedure in one authoritative location.

**Tech stack:** Markdown, relative links, existing repository verification tools. No documentation framework or hosting dependency.

**Spec:** The documentation brief below captures the user's request and the proposed structure. Implementation started after user approval; see the execution record below. The separately requested `AGENTS.md` documentation policy is applied with this plan.

## Documentation brief

- Audience: the single administrator of a Linux Docker host, familiar with basic shell commands and Compose but new to Porty.
- Journey: GitHub README → documentation home → feature overview → installation → first stack → daily use → recovery.
- Scope: feature set, supported deployment paths, configuration, first-run setup, everyday workflows, automation, troubleshooting, backup, upgrade, and limitations.
- Recommended publishing approach: repository Markdown, reviewed and versioned with code. A GitHub Wiki would separate documentation changes from code review; GitHub Pages would add a publishing workflow. Neither is needed for this first version.
- Success: a newcomer can complete the first-stack walkthrough without consulting source code or design plans, and every current feature has a discoverable user-facing explanation.

## Global constraints

- Document implemented behavior, not intentions from design specs. Recheck behavior against the revision being documented, especially the dashboard, which has local work in progress at planning time.
- Preserve existing `docs/operator-guide.md`, `docs/on-demand.md`, `docs/security.md`, and `docs/development.md` URLs. Link between guides instead of copying detailed procedures.
- Use relative links that work on GitHub and in a checkout. User navigation must not send readers to engineering plans.
- Explain the single-server, single-administrator, single-repository/branch model and the required immediate-child `docker-compose.yml` layout.
- Separate build prerequisites from runtime prerequisites. Verify versions against `go.mod`, `web/package.json`, `Dockerfile`, and CI when writing the guide.
- Do not invent published binaries, release assets, registry image names, or platform support. Document source builds and local OCI builds unless distribution artifacts are verified.
- Use synthetic example values. Preserve Docker privilege, file-permission, TLS/origin, secret-handling, and destructive-action guidance at the relevant steps.
- Documentation-only work needs link, content, and example verification, not new application tests or unrelated application changes.

## File map

| Path                            | Responsibility                                                                                                    |
| ------------------------------- | ----------------------------------------------------------------------------------------------------------------- |
| `README.md`                     | Brief product introduction, feature summary, prominent **Documentation** and **Get started** links                |
| `docs/README.md` (new)          | User documentation home with a reading path and task-based table of contents                                      |
| `docs/features.md` (new)        | Supported capabilities, terminology, limits, links to instructions                                                |
| `docs/getting-started.md` (new) | Installation choice, first login, repository setup, first deployment walkthrough                                  |
| `docs/user-guide.md` (new)      | Everyday stack, container, editor, Git, monitoring, alerts, and account workflows                                 |
| `docs/configuration.md` (new)   | YAML, environment variables, flags, defaults, precedence, persistence paths                                       |
| `docs/troubleshooting.md` (new) | Symptom → checks → recovery → expected result                                                                     |
| `docs/operator-guide.md`        | Authoritative host/OCI installation, repository authentication, backup, restore, upgrade, and advanced operations |
| `docs/on-demand.md`             | Authoritative wake/sleep setup, deployment requirements, holds and recovery                                       |
| `docs/security.md`              | Trust model and security reference                                                                                |
| `docs/development.md`           | Contributor instructions, linked separately from the user journey                                                 |
| `AGENTS.md`                     | Require documentation with every feature/change; policy added with this plan                                      |

## Review focus

1. A fresh host or OCI container must have all required prerequisites, writable storage ownership, and Docker socket access explicitly covered.
2. Saved files, Git commits/synchronization, deployment, image pulls, and runtime controls must have distinct documented effects.
3. Missing host metrics and unavailable automation must explain their prerequisites rather than look like zero usage or a broken installation.
4. Destructive actions, stale edits, failed operations, and paused automation must have accurate recovery instructions without promising rollback.
5. All user pages must be reachable from the README; older guide URLs and anchors must remain usable.

## Task 1: Verify the feature inventory and publish the documentation entry point

**Files:** Create `docs/README.md`, `docs/features.md`; modify `README.md`.

**Sources:** Current `web/src/app/WorkspaceShell.tsx`, `web/src/features/`, existing operator/on-demand/security guides, and associated feature tests. Use GitNexus first when tracing behavior, following `GIT_NEXUS.md`.

- [x] Inventory current capabilities and match each to a user workflow: dashboard/host metrics, stack inventory and batch actions, service/container details and controls, file editor and diffs, environment values, deployment review/history, repository setup/status/history/sync, operation output/live logs, alerts, audit history, account settings, scheduled image updates, and on-demand groups.
- [x] Write `docs/features.md` with a capability table linking each feature to its owning guide. Explain repository, stack, service, container, operation, and deployment in user terms.
- [x] State limits where readers choose features: one administrator/host/repository, Compose layout, optional hardware metrics, no external alert notifications, automatic-update exclusions, and on-demand network restrictions. Do not advertise reserved app-health/favicon or GeoIP features as available.
- [x] Write the documentation home with **Start here**, **Daily use**, and **Administration and recovery** sections, plus a separate contributor link.
- [x] Add prominent documentation and getting-started links immediately below the README introduction, followed by a compact feature summary. Keep existing guide links useful.
- [x] Verify every inventory item has an owning page/section. While drafting, track links to planned pages; all must resolve before the documentation change is complete.

## Task 2: Document installation, configuration, and the first successful deployment

**Files:** Create `docs/getting-started.md`, `docs/configuration.md`; update `docs/operator-guide.md` and `docs/security.md` where accuracy requires it.

**Sources:** `internal/config/config.go`, `cmd/porty/main.go`, `deploy/porty.example.yaml`, `deploy/systemd/porty.service`, `Dockerfile`, `.github/workflows/verify.yml`, repository setup/authentication code and tests.

- [x] Reconcile existing installation advice with the SDK implementation. The operator guide currently asks for Git/Compose CLI tools and describes askpass/CLI behavior, while the security guide describes in-process SDKs. Establish actual runtime requirements before rewriting those statements.
- [x] Provide complete host/systemd and locally built OCI installation procedures in the operator guide. Cover obtaining source, building the embedded frontend before the Go binary/image, service/data ownership, Docker socket group access, persistence, listener binding, startup, and readiness. Explain which shell commands run on the server versus the build machine.
- [x] Make getting started a short ordered journey linking to those installation procedures: choose deployment → start Porty → open its URL → register administrator → configure local, remote, or mounted repository → deploy first stack. Explain secure remote access when the default listener is loopback.
- [x] Include a minimal Compose example in the required repository layout, created through the actual supported UI flow. Explain stored interpolation values, save/commit/deploy distinctions, deployment review, expected running state, logs, and cleanup. Verify cleanup effects before describing them.
- [x] Cover repository default-branch selection, Git author identity, HTTPS/SSH authentication, fixed repository location, and later remote changes. Link to canonical authentication instructions rather than repeating secrets or SSH setup.
- [x] Write a configuration table for all supported YAML keys, environment variables, and flags. Include defaults and precedence (defaults → YAML → environment → explicit flags), reload/restart behavior, TLS/public origin, monitoring modes/paths, file-size limit, and data storage. Mark settings without a CLI flag explicitly.
- [x] Distinguish ordinary OCI stack management, optional host-monitoring mounts, and host networking required for on-demand mode. Keep basic installation usable without optional GPU/automation setup.
- [x] Validate both installation paths and the first-stack walkthrough on disposable Linux/Docker fixtures. Record the tested revision/environment and any untested path; do not claim compatibility based only on reading commands.

## Task 3: Write everyday workflows and connect advanced features

**Files:** Create `docs/user-guide.md`; update `docs/operator-guide.md`, `docs/on-demand.md`, and the documentation index as needed.

**Sources:** Corresponding feature components, APIs, and tests under `web/src/features/`; backend behavior when UI evidence is insufficient.

- [x] Organize sections around tasks: understand dashboard/status → find/create stack → edit and review files → set environment values → deploy → inspect/control services and containers → inspect logs and operation history → synchronize Git → manage alerts → change account settings.
- [x] Explain stack/container and batch actions using actual labels, availability conditions, confirmations, partial failures, and resulting state. Include deletion/archive behavior and whether any separate archive/unarchive controls exist and effects on repository files, runtime containers, and volumes after verifying each action.
- [x] Explain file creation/rename/delete, unsaved edits, stale-write recovery, restricted paths/file sizes, and stored environment values and secret masking/reveal. Distinguish editor changes from saved files and committed history.
- [x] Explain Git fetch/pull/push and local-only repositories, including rejected/conflicting changes and the fact that repository synchronization does not deploy stacks.
- [x] Cover deployment review, source drift, deployment history, image pulls, and runtime actions with explicit effects. Describe operation output, live logs, and audit history without implying shell/terminal access or durable log storage.
- [x] Explain dashboard metric availability and short in-memory history, alert acknowledgment versus resolution, and password/session behavior. Keep advanced permission and hardware instructions in the operator guide.
- [x] Introduce automatic updates and on-demand containers with task links to their existing guides. Explain UTC scheduling, eligibility, pauses, explicit recovery/resume, sleeping/held states, initial client retry, and interaction between the two features.
- [x] Walk through each section against the UI on the documented revision. Add only useful screenshots from synthetic fixtures under `docs/images/`, with descriptive alt text and relative paths; existing concept art is not evidence of the current UI.

## Task 4: Document recovery and validate the complete reader journey

**Files:** Create `docs/troubleshooting.md`; finalize all affected guides and README links.

- [x] Add symptom-based entries for failed startup/configuration, data/socket permissions, proxy login/origin errors, incomplete repository setup/authentication, missing stacks, failed deployment, stale editor changes, absent metrics, skipped/paused automatic updates, and on-demand bind/wake failures.
- [x] Each entry must give a non-destructive first check, likely causes, recovery steps, expected result, and a link to the authoritative procedure. Link password reset, backup/restore, and upgrade/rollback to the operator guide.
- [x] Recheck backup scope: Porty's data backup does not replace backups of application volumes or external bind-mounted data. Preserve stop-before-backup guidance and explain forward-only migration rollback requirements.
- [ ] Add a **Documentation** home link to each user guide. Check every local Markdown/image link and heading anchor, including retained old guide anchors. Browse the rendered README and documentation home on GitHub before publishing the documentation change.
- [x] Check formatting with the existing Prettier executable for the changed Markdown files (for example, `web/node_modules/.bin/prettier --check README.md docs/README.md docs/features.md docs/getting-started.md docs/user-guide.md docs/configuration.md docs/troubleshooting.md`). Run `git diff --check` and inspect the scoped diff.
- [x] Perform a newcomer walkthrough from README to a running stack, then edit/deploy, inspect logs, handle a failure, and find backup/recovery instructions. Verify all five review-focus conditions above.
- [x] Record validation and remaining environment limitations in the documentation PR. Review `AGENTS.md` compliance; if committing, run the required GitNexus change analysis first. Keep unrelated working-tree changes outside documentation commits.

## Completion criteria

- Root README links directly to documentation and getting started; all user pages are reachable from the documentation home.
- Features, both installation paths, configuration, first deployment, daily usage, automation, and recovery are covered without duplicating authoritative procedures.
- Commands, UI labels, defaults, limits, and screenshots match the documented revision. Any unverified environment is explicitly identified.
- Links, anchors, formatting, and walkthrough checks pass; no credentials or runtime data appear in examples.
- Every future feature/change requires corresponding documentation in the same change through the updated `AGENTS.md` policy.

## Execution record — 2026-10-10

Implemented the documentation home, six guide/overview pages including that home, README navigation, six app screenshots, and corrections to existing guides. The documentation policy in `AGENTS.md` remains in place.

Source verification corrected two assumptions in the plan: environment values can be retrieved and edited (secrets are masked/revealable); the UI has no standalone archive/unarchive or pull-image control. The guides describe actual UI workflows. Git SDK prerequisites and authentication descriptions were reconciled.

Native and OCI walkthroughs used disposable data and real Redis containers. Native validation failure/recovery and redeployment passed. Screenshot inspection, local links/anchors, formatting, and static packaging checks passed. See `docs/development.md` for the environment and verification limits. A clean-host systemd install and every advanced-feature workflow were not rerun; advanced guidance was checked against source, tests, and existing operator documentation.

The remaining unchecked navigation step is complete locally; its final live GitHub rendering check is deferred until these files are published. No push, PR, or external publication was requested. Application source and existing local frontend changes were not modified by this documentation work.
