# Porty UI overhaul

## Intent and agreed direction

Make Porty feel like a mature, cohesive application and reduce ambiguity in everyday operation. The primary users are Docker-literate homelab owners and experienced operators. Success means users can quickly identify what is running, what needs attention, what has changed, and what a proposed action will affect.

The chosen approach is a focused operational console: retain familiar workflows, improve their presentation and clarity, and selectively support efficient bulk work. The landing page remains a working list of stacks. This is a usability and visual overhaul, not a change to Porty's single-administrator, single-server, single-repository product model.

The user selected **B · Soft contrast**: tinted navigation, contained surfaces, roomier table rows, restrained color, and clear hierarchy. The stack-detail layout and the interaction, navigation, editing, accessibility, and rollout principles below were approved conversationally. The written specification requires review before the detailed implementation plan.

## Scope and compatibility

Cover the application shell, stack inventory and details, existing cross-stack container inventory, container details, editor, repository, operations, alerts, audit log, settings, login, and initial setup. Preserve existing capabilities, routes, authentication, action eligibility, batch limits, and backend security behavior.

The existing `#/` entry route becomes visibly named **Stacks**; stack and container URLs keep their current identities. Preserve access to the existing cross-stack container inventory below the primary stack list. The abbreviated visual mockup does not authorize removing it. Keep archived records discoverable and retain the existing inclusive initial inventory filter; offer explicit All, Active, and Archived choices.

No new metrics subsystem, server fleet management, roles, repositories, command palette, custom shortcut system, automatic recovery, exact deployment change planner, or backend rewrite is part of this design. Do not add speculative frontend dependencies. Product behavior must be supported by existing contracts; if a required presentation cannot be derived truthfully, identify the specific contract gap during planning rather than inventing data.

## Visual system

- Use a softly tinted sidebar, muted page background, and contrasting content panels. Separate sections through spacing and subtle borders; avoid heavy shadows and decorative dashboard cards.
- Use one restrained spacing scale, typography hierarchy, icon family, border treatment, and set of control sizes. Keep technical identifiers in monospace and ordinary labels in the application font.
- Tables remain comfortable for repeated scanning, with stable columns and clear identity links. Roomier rows must not turn operational lists into oversized cards.
- Reserve strong emphasis for the primary action and important exceptions. Use readable status text with color and, where useful, an icon. Running does not imply healthy; sleeping does not imply failure.
- Retain System, Light, and Dark preferences. Theme tokens cover page surfaces, text, borders, links, semantic states, primary actions, hover, focus, and disabled presentation.
- Primary actions use dedicated fill and foreground tokens, independent from link colors. **Deploy changes stays blue with white text in dark mode**, correcting the pale fill in the first mockup. The corrected reference uses `#1769b4` in light mode and `#256bb7` in dark mode, both with white text. These combinations measured 5.65:1 and 5.43:1 text contrast respectively.
- Use at least 4.5:1 contrast for ordinary text and 3:1 for meaningful control boundaries and focus indicators. Check semantic badges and muted copy in both themes. Aim for 44 px touch targets on narrow screens.

Prototype references are preserved locally in the ignored directory `.superpowers/brainstorm/4928-1791542763/content/`: `visual-directions.html` contains direction B; `stack-detail-soft-v2.html` contains the accepted detail structure and corrected button. These are illustrative references, not production code or a required runtime dependency. The requirements in this specification are authoritative when illustrative data or abbreviated screens differ from existing capabilities.

## Navigation and stacks inventory

The primary destinations are **Stacks, Repository, Operations, Alerts, Audit log, Settings**. Group monitoring destinations visually without creating additional navigation levels. Display server identity, account access, and connection state consistently. Stack and container detail use breadcrumbs back to their actual parent context.

The Stacks page contains:

1. A clear title and **New stack** action.
2. A compact attention summary when known issues or undeployed changes exist. Each item filters or links to the relevant records; do not show a decorative all-clear summary without adequate data.
3. Search, runtime/deployment filters, archive visibility, and predictable name sorting. Preserve the ability to identify uncommitted changes; do not conflate it with undeployed configuration.
4. A primary table with selection, stack identity, runtime, deployment configuration state, last deployment, and secondary actions. Mark archived records explicitly.
5. Contextual bulk actions appearing with selection, including the selected count and clear selection control.
6. The existing cross-stack container inventory as a secondary section, preserving its own search, filters, selection, and actions.

Repository sync belongs to the repository and is summarized once, with a link to its page. Do not repeat repository-wide status as though it were independently scoped to every stack.

Retain existing selection limits, eligibility checks, and bounded request behavior. Filtering, data refreshes, removed containers, and partial acceptance must not make the target set ambiguous. Identify accepted and rejected groups separately and preserve useful retry context.

## State vocabulary

Treat these as independent dimensions:

| Dimension | Meaning and presentation |
| --- | --- |
| Runtime | What Docker reports: running, stopped, partially running, unhealthy, sleeping/on-demand, or unknown. Show container health separately when available. |
| Deployment | Whether the saved configuration matches the recorded deployment: deployed/current, undeployed changes, deploying, never deployed, or unable to verify. |
| Git working tree | Whether saved files for this stack differ from Git: uncommitted changes or no uncommitted changes. This is independent of deployment freshness. |
| Repository sync | Local commit relationship to the last fetched remote state, with ahead/behind/diverged distinctions. Avoid implying that remote information is live. |
| Connection/data freshness | Whether updates are live, reconnecting, unavailable, or incomplete. A disconnected stream is not a healthy runtime state. |

Use existing API state as the authority. Do not map unknown or unverifiable data to success. Missing health checks must say **No health check**, not Healthy. An intentionally stopped stack is not automatically an incident. Counts must distinguish incomplete reads from complete inventories; never turn read failures into zero records or an all-clear message.

## Stack and container details

The stack header shows identity, **Validate config**, a primary deployment action, and a secondary whole-stack action menu. Use **Deploy changes…** when configuration changes are known, and **Deploy stack…** for first deployment or other eligible deployment states. Do not claim changes exist when freshness is unknown. Retain server-side eligibility and explain disabled actions.

A restrained status strip separates Runtime, Deployment, and Git for the selected stack. Supporting text clarifies each dimension. When changes are pending, provide a contextual notice with **Review files**; avoid duplicate generic warnings.

Stack sections are **Overview, Compose & files, Logs, Deployments, Alerts, Settings**. Their labels describe what users will find. Tab changes and navigation preserve unsaved-edit protection.

Overview displays existing containers, including stopped containers and distinct replicas. Show service/container identity, image, runtime, health, published ports, and applicable controls. Keep existing network and other inspection details accessible even if they are secondary at narrow widths. Declared services without containers must not be fabricated as runtime rows. An empty runtime explains when Deploy is the appropriate next step.

Page-header controls operate on the whole stack. Row and selection controls operate on explicitly named containers. Preserve per-container eligibility and membership checks. Provide direct access to container logs and retain the existing container-detail route and inspection content. Below the list, show the last deployment with a link to its result.

## Editing and Git

| Action | User-facing meaning |
| --- | --- |
| Save file | Write the current editor contents to the server. |
| Commit changes | Record saved changes in Git history. |
| Deploy stack | Apply saved configuration to Docker. |

Keep these independent actions with distinct success messages. Committing is not required merely to deploy saved configuration; neither saving nor committing should silently deploy. Label the Git diff **Uncommitted changes**, not Deployment preview. Do not promise an exact container change plan from a Git diff.

The editor clearly identifies the selected file, unsaved state, save progress, and validation or save errors. Preserve edits after recoverable errors. On a stale-write conflict, retain the local buffer and explain the conflict; never silently overwrite the server's newer file. Keep file navigation, editing, and reviewing changes organized as coherent areas, with a single active area on narrow screens when necessary.

Before a deployment with unsaved edits, offer **Save and continue**, **Discard and continue**, or **Cancel**. The first saves successfully before opening deployment review; the second explicitly discards the local buffer before review; Cancel preserves it. Neither path submits a deployment automatically. If saving fails or conflicts, remain in the editor with the buffer intact.

The Repository page owns Fetch, Pull, Push, remote configuration context, and history. Explain their distinct effects near the controls and their repository-wide scope. Keep repository action controls off unrelated pages. Derive effect descriptions from the actual operation contract, including any backend restrictions; fetching remote information, updating local files, and publishing commits must not be confused with deploying containers. Do not invent a last-fetch timestamp if the API does not expose one.

## Action feedback and recovery

- Distinguish submitting, queued, running, succeeded, failed, and interrupted outcomes where the backend reports them. An accepted request is not a completed operation.
- Show active-operation progress and provide output/details without losing the user's page context. Disable incompatible actions with an accessible explanation, without broad, unexplained UI lockout.
- Confirm deployment and disruptive actions with the affected stack or named containers, action-specific consequences, and an explicit action label. Use **Stop stack**, **Restart containers**, or **Deploy stack**, not a generic OK. Preserve existing destructive-action protections and explain retained volumes or removed files accurately.
- Deployment review identifies the stack, saved-local configuration source, and known uncommitted changes. Explain that containers may be recreated and service interrupted. Do not imply a calculated plan of exactly which containers will change.
- Revalidate relevant eligibility and source state before submission; do not let a review silently authorize a different selection or newly saved source. Server validation remains authoritative, and conflicts return actionable feedback.
- Keep failure details and recovery links visible in context and operation history. Toasts may supplement, but never replace, persistent error information.
- Preserve useful form data and selection after recoverable failures. Avoid automatic retries of mutations with uncertain outcomes; direct users to the recorded operation or refreshed state.
- Mark disconnected or stale data clearly. Reuse existing stream/polling behavior and bounds; do not retain invalid inventory rows contrary to current partial-read semantics.

## Supporting screens

- **Operations:** distinguish active work from history. Show kind, scope, status, timing, and accessible output/details. A desktop detail panel becomes a full-width view on narrow screens, with a clear return path.
- **Alerts:** show affected resources, problem, acknowledgement/resolution state, and appropriate recovery guidance. Acknowledgement or manual resolution does not restart containers, repair the problem, or resume automation. Do not present the alert list as continuous runtime health monitoring.
- **Audit log:** use a quieter, readable record of actions, preserving existing content and redaction.
- **Application settings:** group appearance, repository configuration, and account settings by responsibility. Explain session effects for password changes.
- **Stack settings:** group automation, environment, and stack identity; isolate destructive actions. Distinguish policy enabled/disabled, paused after an incident, and currently ineligible states. Explain why and expose existing recovery actions without implying that toggling a switch repairs runtime state.
- **On-demand behavior:** explain sleeping and waking separately from failure and automatic updates. Preserve existing eligibility and capability messages.
- **Login/setup:** use the same visual primitives, clear field labels, inline feedback, and an obvious next step. Preserve the existing administrator and repository setup sequence.
- **Secrets:** preserve masking, deliberate reveal behavior, safe errors, and output redaction. Never expose sensitive content in a confirmation or design convenience feature.

## Responsive behavior and accessibility

Desktop uses persistent sidebar navigation and readable tables. Narrow layouts provide a compact navigation control with all destinations and current location available; the prototype's omitted mobile sidebar is not the final navigation design.

On mobile, prioritize stack identity, runtime, and deployment state. Move secondary information into accessible detail/disclosure areas or labeled horizontal table regions. Do not force every column into a compressed viewport or allow page-level horizontal overflow at 320 px. Preserve access to selection and action scope. Stack detail sections remain reachable, and logs/editor regions scroll within their own bounds.

Provide visible keyboard focus, semantic navigation and table labels, meaningful checkbox names, keyboard-operable menus and tabs, and appropriately labeled icons. Dialogs have an accessible title, sensible initial focus, focus containment, Escape/cancel behavior, and focus restoration. Do not convey status through color alone. Announce meaningful asynchronous results without announcing every live update. Respect reduced motion.

Preserve filters, scroll position, and relevant context when viewing operation details. Loading, genuinely empty, failed, partial, and disconnected states must remain distinguishable. Disabled controls have a visible or keyboard-accessible explanation; hover-only tooltips are insufficient.

## Architecture and implementation boundaries

Retain Preact and the existing feature-oriented organization:

- `web/src/app/`: shell, navigation, routing integration, theme, global tokens/base styles, and existing workspace coordination.
- `web/src/components/`: genuinely shared visual primitives such as buttons, fields, notices, badges, menus, confirmation dialogs, and panels. Extract from repeated use; do not build a generic dashboard or table framework.
- `web/src/features/stacks/`: inventory, status presentation, detail, containers, editing, and stack settings.
- Existing repository, operations, alerts, audit, and auth feature directories retain their own behavior and types.

Presentation components receive explicit typed data and callbacks; keep request and operation orchestration in focused feature hooks/controllers. Move feature-specific styles beside their feature as touched, while preserving shared tokens centrally. Avoid unrelated restructuring of the monolith.

Reuse existing APIs, stream hooks, freshness rules, and bounded polling. UI state must not become a competing source of runtime truth. Preserve authentication, cookies, CSRF/origin enforcement, rooted paths, secret redaction, stale-write protection, locks, and restrictive filesystem behavior. Any unavoidable contract gap is a specific planning decision with its own tests, not permission for a backend redesign.

## Delivery sequence

1. **Foundation and shell:** theme tokens, controls, navigation, responsive layout, feedback primitives, and corrected primary-button colors.
2. **Stacks and containers:** inventory, attention summary, state vocabulary, stack/container details, selection, scoped actions, deployment review, and operation progress.
3. **Editing and repository:** explicit Save/Commit/Deploy semantics, unsaved-edit handling, conflicts, diffs, and repository action explanations.
4. **Supporting screens:** operations, alerts, audit log, application/stack settings, login, and setup.
5. **Cross-screen verification:** both themes, responsive layouts, accessibility, real task flows, and regression coverage.

Each phase should be reviewable without temporarily removing existing functionality. The detailed implementation plan will map these phases to concrete files, graph impact checks, test scenarios, and scoped commits. No production implementation starts from the mockup alone.

## Acceptance and verification

Write failing behavior tests before changing behavior. Use colocated Vitest/Testing Library tests and cross-feature Playwright flows. Pure styling adjustments need visual and contrast checks rather than tests that mirror CSS declarations.

Required scenarios:

- Identify an unhealthy container from the stack inventory and reach its logs without ambiguity about the affected stack.
- Distinguish a running stack with undeployed changes from one with uncommitted-but-already-deployed files.
- Identify unknown, sleeping, stopped, no-health-check, never-deployed, and unverifiable states without false success/error implications.
- Save, commit, and deploy independently; exercise Save/Discard/Cancel before deployment, failed saves, and stale-write conflicts.
- Submit deployment, observe acceptance and progress, then distinguish success from failure. Prevent duplicate or conflicting actions while preserving access to details.
- Verify confirmation scope for whole-stack and selected-container actions, including changed selection, archived resources, removed containers, limits, and partial acceptance.
- Show partial read failures and disconnected data honestly; avoid false empty states and misleading healthy summaries.
- Verify alert acknowledgement versus recovery, paused automation, and on-demand eligibility messaging.
- Complete keyboard navigation, menu use, dialog cancellation/focus restoration, and tab navigation. Check labels, status announcements, text contrast, and focus visibility.
- Review desktop and mobile screenshots for both themes, including tables, editor, dialogs, error/empty/loading states, and navigation at narrow widths. Confirm the primary Deploy button remains blue with readable white text.
- Preserve login/setup, account settings, environment masking, container inspection, cross-stack inventory, and existing security-sensitive behavior.

Run affected frontend suites, then `bun run test`, `bun run typecheck`, `bun run build`, and relevant `bun run test:e2e` flows from `web/`. Run Go tests/vet/build or packaging checks when the implementation touches their contracts or embedded delivery. Follow repository GitNexus impact requirements before symbol edits and graph-change analysis before commits. Include desktop/mobile evidence in the implementation PR.

## Current status

The specification and implementation plan were reviewed and approved. The 12-task implementation is complete in a separate worktree. See the [implementation review and verification record](../reviews/2026-10-09-ui-overhaul.md).
