# Frontend Feature Architecture Implementation Plan

> **For agentic workers:** Use `superpowers:executing-plans` to implement this plan task by task. Repository instructions prohibit subagents. Check off each step after verification.

**Goal:** Reorganize `web/src` by feature and give bootstrapping, API transport, domain behavior, and presentation clear owners without changing Porty's user-visible behavior.

**Architecture:** `app` owns session gating, hash navigation, workspace coordination, theme initialization, and global styles. Each feature owns its API functions, HTTP response types, stateful behavior, and UI; `lib/http.ts` alone owns fetch, CSRF, refresh, and error translation. Shared visual components live in `components` only when used by multiple features. Do not create empty `hooks`, `utils`, or `types` directories.

**Tech Stack:** Preact 10, TypeScript, Vite, Bun, Vitest with Testing Library, Playwright, Prettier. No new runtime dependency or router/state library.

**Spec:** The user-provided frontend requirements in `AGENTS.md` (Frontend and Testing sections), restated in the request. This is an architecture-only migration; preserve the current HTTP and WebSocket contracts and UI behavior.

## Global Constraints

- Keep `web/src/main.tsx` as the Vite entry point and `web/e2e` for cross-feature browser tests. Colocate unit/component tests with the owner they exercise.
- Keep local form, selection, tab, editor, and busy state in their feature. Workspace holds only values consumed across features: session, repository readiness, stack/repository/operation/audit snapshots, selected operation, route, and unsaved-editor guard. Do not add context or a state library.
- Keep feature types with their owner even when another feature imports them. Use explicit TypeScript types for API requests, responses, and component boundaries. Avoid root-level `types`, barrel files, and re-export shims.
- Keep existing CSRF cookie fallback, shared refresh, one retry, WebSocket reconnect/gap behavior, bounded operation output, editor hash handling, secret clearing, and dirty-navigation confirmation.
- Use relative imports with the existing TS/Vite configuration; preserve existing CSS class names while moving code. Split CSS only where sections have clear owners and ordering can be preserved.
- Before every symbol edit or move, run GitNexus `impact` upstream on that symbol. The repository index was three commits behind HEAD during planning, so refresh it first and confirm any `UNKNOWN` with source search. Run `detect_changes --scope all` before each commit; do not treat partial or truncated output as clean.
- Run focused tests after each slice, then full `bun run test`, `bun run typecheck`, `bun run build`, and Prettier check. Run Playwright and packaging at the final gate where the required backend/Docker environment is available.

## Current State and Target File Map

The current frontend has 22 source/test files directly in `web/src`. `App.tsx` is 502 lines and owns session boot, readiness, workspace data, hash routing, repository actions, and shell markup. `api.ts` is 247 lines and mixes transport with six feature contracts. `Settings.tsx` contains stack environment settings and account settings. `ui.tsx` combines shared components with stack/repository status helpers. `styles.css` is 1,287 lines. Existing baseline: 6 test files, 38 tests passing; typecheck and build passing.

| Current source | Target owner and responsibility |
| --- | --- |
| `main.tsx`, `App.tsx`, `theme.ts`, `styles.css` | `main.tsx`; `app/App.tsx` session/readiness gate; `app/Workspace.tsx` orchestration; `app/useHashRoute.ts` route/dirty guard; `app/useWorkspaceData.ts` shared snapshot/loading; `app/WorkspaceShell.tsx` navigation and shell; `app/theme.ts`; `app/styles.css` global tokens/layout. |
| `api.ts`, `api.test.ts` | `lib/http.ts`, `lib/http.test.ts` for `api`, `APIError`, `message`, `setCSRF`, `refreshSession`; feature API/type files below. Delete the old file after direct imports have moved. |
| `Auth.tsx`; account portion of `Settings.tsx` | `features/auth/Auth.tsx`, `features/auth/AccountSettings.tsx`, `features/auth/api.ts`, `features/auth/types.ts`. Account settings remains the composition point for repository settings and theme preference. |
| `RepositorySetup.tsx`, `RepositorySettings.tsx`, `RepositoryRemoteFields.tsx`; repository header/history markup in `App.tsx` | `features/repository/` with those components plus `RepositoryHeader.tsx`, `RepositoryHistory.tsx`, `api.ts`, `types.ts`. Move their existing colocated tests with them. |
| `Dashboard.tsx`, `StackDetail.tsx`, `Editor.tsx`, `CodeEditor.tsx`; stack/environment portion of `Settings.tsx` | `features/stacks/` with those components plus `StackSettings.tsx`, `EnvironmentRow.tsx`, `api.ts`, `types.ts`, `stackStatus.ts`. Keep the CodeMirror dependency confined to `CodeEditor.tsx`. |
| `Operations.tsx`, `stream.ts` | `features/operations/Operations.tsx`, `OperationDrawer.tsx`, `useOperationStream.ts`, `streamState.ts`, `api.ts`, `types.ts`; move `stream.test.ts` beside stream behavior. |
| Audit markup in `App.tsx` | `features/audit/Audit.tsx`, `api.ts`, `types.ts`. |
| `ui.tsx` | `components/Icon.tsx`, `components/Feedback.tsx` (`Badge`, `Empty`, `Notice`); stack/repository helpers go to `features/stacks/stackStatus.ts`. Keep the two small shared component files focused; no index file. |

The table is an ownership map, not a demand to create a file if extraction leaves it empty. `web/src/test/setup.ts` remains test infrastructure. `web/e2e/critical.spec.ts` remains a cross-feature journey. Test files otherwise move with their source.

## Interface and State Rules

```ts
// lib/http.ts: transport only; preserve current retry and CSRF behavior.
export function api<T>(path: string, method?: string, body?: unknown, headers?: Record<string, string>): Promise<T>;
export function setCSRF(token: string): void;
export function refreshSession(): Promise<void>;
export class APIError extends Error { status: number; code: string }
export function message(error: unknown): string;

// Representative feature contracts; use similarly named, typed functions for other endpoints.
// features/repository/api.ts
export function getRepositorySetupStatus(): Promise<RepositorySetupStatus>;
export function inspectRepositoryRemote(request: RemoteInspectionRequest): Promise<RemoteInspection>;
// features/stacks/api.ts
export function listStacksWithState(): Promise<Stack[]>;
export function getStackFile(stackId: string, path: string): Promise<FileContent>;
// features/operations/api.ts
export function listOperations(): Promise<Operation[]>;
```

`app/useWorkspaceData.ts` calls feature API functions and handles the existing independent `Promise.allSettled` loads and 401 logout rule. Do not centralize feature mutations there: stack creation stays in stacks; repository actions stay in repository; editor file mutations stay in stacks. Feature entry components may own event/state coordination, but visual leaf components take props and never import `lib/http.ts` or call `fetch`. Keep response types aligned with the current backend JSON; do not add client-side casts beyond the current transport boundary.

## Review Focus

Pin these high-risk behaviors in the task that owns them before moving the relevant code:

1. Unsaved editor changes block sidebar/hash navigation, browser unload, repository actions, and sign out; cancel retains the current route and content (Task 4).
2. A 401 refresh shares one request across concurrent callers, retries only once, preserves CSRF, and does not retry login/setup requests (Task 1).
3. Stream reconnect refreshes the session, marks gaps, ignores duplicates, and caps output at 65,536 characters (Task 3).
4. Repository setup and remote settings clear a failed HTTPS secret while preserving safe fields and continue to require inspection/confirmation (Task 2).
5. Editor stale hash conflicts keep unsaved content and prevent silent overwrite; stack/environment actions preserve their confirmation and error behavior (Task 5).

## Tasks

### Task 1: Isolate HTTP Transport and Authentication Contracts

**Files:** Move transport and transport tests from `web/src/api.ts` and `web/src/api.test.ts` to `web/src/lib/http.ts` and `http.test.ts`; create `web/src/features/auth/types.ts` and `api.ts`; update direct imports in existing files. Keep a compilable tree at the end of this task.

- [ ] Refresh the GitNexus index, run upstream impact for `api`, `refreshSession`, `setCSRF`, `APIError`, and `Session`, and record direct callers before changing imports. Note the refresh path's shared-client risk.
- [ ] Extend `http.test.ts` with cases for login/setup 401 exclusion and failed refresh producing one failure without a second retry; confirm the new tests fail against any accidental retry behavior while the existing concurrent refresh tests remain green.
- [ ] Move transport code unchanged to `lib/http.ts`. Move `Session` to `features/auth/types.ts`; put session/status/login/logout/password API functions in `features/auth/api.ts`. Update `Auth.tsx`, `App.tsx`, `stream.ts`, and tests to import directly. Keep the existing `api<T>` signature during migration.
- [ ] Run `bun run test -- src/lib/http.test.ts`, `bun run typecheck`, and `bun run build` from `web/`. Remove the former transport exports from `api.ts` only after all direct importers compile; do not leave an `api.ts` compatibility barrel.
- [ ] Run Prettier on touched files, GitNexus `detect_changes --scope all`, then commit `refactor: isolate frontend HTTP transport`.

### Task 2: Move Repository Contracts and Setup UI Together

**Files:** Create `web/src/features/repository/{types,api,RepositorySetup,RepositorySettings,RepositoryRemoteFields}.ts(x)` and move their two tests. Update imports in `App.tsx`, `Settings.tsx`, and affected tests. Keep `RepositoryHeader` and `RepositoryHistory` for Task 4, when the shell is split.

- [ ] Run upstream impact for repository setup/remote functions and components. Copy the repository-specific request/response types from old `api.ts` into `features/repository/types.ts`; move setup, inspection, configure, remove, status, and repository history/status/action endpoint calls into typed `features/repository/api.ts`.
- [ ] Move the three repository UI components and their colocated tests without changing displayed controls. Replace transport imports with feature API imports. Keep their form state local and preserve clearing of `secret` after failed requests.
- [ ] Add or retain user-visible tests for failed-secret clearing, required inspection before importing/adding a remote, and confirmation before replacing/removing an origin. Run `bun run test -- src/features/repository`, `bun run typecheck`, and `bun run build`.
- [ ] Prettier, run GitNexus `detect_changes --scope all`, then commit `refactor: colocate repository setup and remote UI`.

### Task 3: Separate Stack, Operation, and Audit Contracts

**Files:** Create `web/src/features/stacks/{types,api,stackStatus}.ts`, `features/operations/{types,api,streamState,useOperationStream}.ts`, and `features/audit/{types,api}.ts`; move `stream.test.ts` beside the stream code. Update imports in current components and tests.

- [ ] Run upstream impact for `Stack`, `Operation`, `useOperationStream`, `reduceStream`, `isModified`, and `remoteState`. Move each response type to its feature owner. A cross-feature caller imports the owning type directly; `Repository` stays in repository, not a root shared-types folder.
- [ ] Give feature API files named functions for existing endpoints: stack list/state/create/detail/actions/environment/files/deployments, operation list, and audit list. Keep URL encoding in these functions (including stack IDs and file paths) and keep backend routes and response shapes unchanged. Move `stackPath` and stack status helpers into `features/stacks`.
- [ ] Split `stream.ts` by responsibility: pure `reduceStream` and its types in `streamState.ts`; WebSocket lifecycle in `useOperationStream.ts`. Preserve its callback-ref technique so reconnect sees fresh callbacks, refresh-before-reconnect, gap state, duplicate filtering, and output bound.
- [ ] Move existing stream tests and add a bounded-output assertion at exactly 65,536 characters. Run `bun run test -- src/features/operations`, `bun run typecheck`, and `bun run build`.
- [ ] Delete the now-empty `web/src/api.ts` and `stream.ts` only after every importer has a direct owner import. Remove status helpers from `ui.tsx`, leaving its visual components for Task 6. Prettier, run GitNexus `detect_changes --scope all`, then commit `refactor: give frontend domains typed API modules`.

### Task 4: Split App Boot, Route Guard, Workspace Data, and Shell

**Files:** Move `web/src/App.tsx` to `web/src/app/App.tsx`; create `app/{Workspace,useHashRoute,useWorkspaceData,WorkspaceShell}.tsx/ts` as appropriate; create `features/repository/{RepositoryHeader,RepositoryHistory}.tsx` and `features/audit/Audit.tsx`; move `App.test.tsx` to `app/App.test.tsx`; update `main.tsx`.

- [ ] Run upstream impact for `App`, `Workspace`, `refresh`, and `navigate`. Add focused App tests for direct hash navigation, malformed encoded stack IDs, cancellation of dirty navigation, and logout cancellation. Keep the current browser-visible behavior in those tests.
- [ ] Put session/bootstrap/readiness gating in `app/App.tsx`; put the hashchange, beforeunload, dirty-ref, and navigation behavior in `app/useHashRoute.ts`. Keep unknown routes and malformed IDs falling through to the existing not-found view.
- [ ] Move the five independent workspace reads, partial-failure handling, 401 logout, stream-driven refresh, and cross-feature snapshots to `app/useWorkspaceData.ts`. Preserve the existing `Promise.allSettled` semantics, 50-row operation cap, and current error reporting. Keep feature mutations behind their feature API functions.
- [ ] Keep `app/Workspace.tsx` as the small coordinator. Move sidebar/server information and active-route markup to `WorkspaceShell.tsx`; repository toolbar/history to the repository feature; audit list to the audit feature. Pass explicit typed props rather than a broad app-context object.
- [ ] Run `bun run test -- src/app`, full `bun run test`, `bun run typecheck`, and `bun run build`. Prettier, run GitNexus `detect_changes --scope all`, then commit `refactor: separate frontend boot and workspace routing`.

### Task 5: Colocate Stack Dashboard, Detail, Editor, and Settings

**Files:** Move `Dashboard.tsx`, `StackDetail.tsx`, `Editor.tsx`, `CodeEditor.tsx` into `features/stacks`; extract the stack portion of `Settings.tsx` to `features/stacks/StackSettings.tsx` and `EnvironmentRow.tsx`; update `App.test.tsx` imports only where needed.

- [ ] Run upstream impact for the five components, especially editor save and stack actions. Move each component into the stacks feature, retaining its local search/filter/sort/tab/file/form state. Put file/tree/diff/commit and environment requests behind named `features/stacks/api.ts` functions; presentation markup must not import the generic HTTP transport.
- [ ] Split `Editor.tsx` only at existing coherent seams: keep editor coordination and dirty/hash/error handling together; leave CodeMirror setup in `CodeEditor.tsx`. Split `StackDetail.tsx` only where its tab content has a distinct responsibility. Avoid one-file-per-button components.
- [ ] Add focused tests for a stale file hash preserving entered content, a failed create action retaining the form and showing the error, and environment values remaining write-only in the UI. Use the existing App test fixture or feature-local tests; colocate any new feature tests.
- [ ] Run focused stack tests and the full frontend test/typecheck/build commands. Prettier, run GitNexus `detect_changes --scope all`, then commit `refactor: colocate stack screens and editor`.

### Task 6: Finish Feature UI and Shared Components

**Files:** Move `Auth.tsx` and account portion of `Settings.tsx` to `features/auth`; move `Operations.tsx` to `features/operations/Operations.tsx` and `OperationDrawer.tsx`; move shared visual code from `ui.tsx` to `components/Icon.tsx` and `Feedback.tsx`; move `theme.ts` and its test to `app`.

- [ ] Run upstream impact for `Auth`, `AccountSettings`, `Operations`, `OperationDrawer`, `Icon`, `Badge`, `Notice`, `Empty`, and theme functions. Move only components used across features to `components`; retain stack/repository status computation in stacks. No barrel exports.
- [ ] Keep theme preference handling in `app/theme.ts` while `AccountSettings` calls it through direct imports. Keep account/password mutations in `features/auth/api.ts`. Keep operation display and drawer state in the operations feature, with selected operation ID in Workspace because route content and drawer share it.
- [ ] Move/update colocated theme tests. Run full `bun run test`, `bun run typecheck`, and `bun run build`; check no component imports from deleted root modules. Prettier, run GitNexus `detect_changes --scope all`, then commit `refactor: finish frontend feature ownership`.

### Task 7: Review Styles, Architecture Boundaries, and Browser Behavior

**Files:** `web/src/app/styles.css`, `web/src/main.tsx`, affected feature CSS files only if cohesive sections can be moved without changing precedence; `web/e2e/critical.spec.ts` only for a missing cross-feature regression. Update frontend architecture guidance in `docs/development.md` if present; do not restate `AGENTS.md`.

- [ ] Move `styles.css` into `app` and update the entry import. Preserve tokens, resets, responsive rules, selectors, and cascade. If a contiguous feature section is independent, move it beside that feature and import it explicitly in deterministic order from `main.tsx`; otherwise keep the single stylesheet rather than inventing CSS modules.
- [ ] Search for stale root imports, generic `fetch` outside `lib/http.ts` and WebSocket code, unowned API calls in presentational leaf components, unused exports, empty folders, and cyclic feature imports. Fix only actual ownership violations. Verify the final structure against the file map, allowing absent `hooks`, `utils`, and `types` roots.
- [ ] Run `./node_modules/.bin/prettier --check 'src/**/*.{ts,tsx,css}'` from `web/`, then `bun run test`, `bun run typecheck`, and `bun run build`. Run the existing Playwright journey with its required backend binary/environment; if available, run `./deploy/package_test.sh` from the repo root to verify embedded assets. Record any environment-gated checks accurately.
- [ ] Run GitNexus `detect_changes --scope all`, inspect the final diff for accidental behavior/CSS changes, and commit `refactor: finish frontend feature architecture`. Leave the worktree clean.

## Execution Notes

This migration should keep the app compiling at each commit. Move contracts before UI, and move each component's test with it. Use Git-aware moves where helpful; never perform a blind symbol rename or repo-wide text replacement. If a proposed hook merely forwards props or a feature API function becomes a generic endpoint wrapper, keep the simpler local code instead. Preserve current visual output; screenshots are needed only if implementation changes rendered UI.
