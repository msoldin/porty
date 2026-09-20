# First-Start Stack Repository Setup Design

## Summary

Porty will require its single administrator to configure the stack repository before entering the normal application. The setup experience supports a new local-only repository, an existing remote repository, or an existing Git worktree mounted at Porty's fixed repository path.

The repository remains usable without a remote. A remote and its authentication can be added, replaced, updated, or removed later from Repository settings. Porty never browses or adopts arbitrary host paths.

## Goals

- Gate stack management behind a successful first-start repository setup.
- Make a new local repository immediately usable for edits and commits without operator CLI steps.
- Import remote repositories without assuming the branch is named `main`.
- Support empty remote repositories.
- Adopt an existing local repository only when it is mounted at Porty's managed repository path.
- Support public remotes, HTTPS username/token authentication, and operator-mounted SSH credentials.
- Preserve Porty's rooted-filesystem, process-execution, redaction, locking, and Git-hardening guarantees.
- Let a ready repository move between local-only and remote-backed operation without rerunning onboarding.

## Non-goals

- Browsing or selecting arbitrary server-side repository paths.
- Uploading or pasting SSH private keys through the browser.
- Supporting Git credential helpers, interactive prompts, SSH agents, or inherited user Git configuration.
- Switching an established repository between branches after onboarding.
- Merge, rebase, force-push, or unrelated-history workflows.
- Adding progress streaming or cancellation for setup in the first version.

## Lifecycle and Access Gate

`app_state.setup_state` becomes the authoritative setup lifecycle:

1. `unregistered`: no administrator exists.
2. `registered`: the administrator exists, but no stack repository is ready.
3. `ready`: the repository and its local operating configuration passed validation.

After registration and after every login, the frontend requests authenticated repository setup status before rendering the application shell. A `registered` server renders only the setup wizard. Refreshing or signing in again returns to the wizard until setup succeeds.

The backend enforces the same gate. Stack, editor, deployment, and repository-operation endpoints return `409 RepositorySetupRequired` unless setup state is `ready`. Authentication, password management, setup status, remote inspection, and setup submission remain available while gated. UI gating is not treated as an authorization boundary.

Porty transitions to `ready` only after the worktree, selected branch, repository-local author identity, optional remote authentication, and Git safety checks succeed. Setup retries before that transition are recovery-safe: if Git work completed but persistence failed, the service validates and resumes the matching partial result rather than destructively starting over.

### Existing Installation Reconciliation

On upgrade, a server in `registered` state with an already configured and safe repository is reconciled before serving requests. Porty imports the persisted branch and repository-local author identity where available, supplies the approved defaults when identity is absent, validates the existing worktree, and marks it `ready`. An installation without a valid repository remains `registered` and enters the wizard.

## Fixed Repository and Credential Locations

Every setup mode operates on the existing managed repository root, currently `<data-dir>/repository`. The API does not accept a repository path.

SSH authentication uses operator-mounted files at fixed locations derived from the data directory:

- Private key: `<data-dir>/ssh/id`
- Host keys: `<data-dir>/ssh/known_hosts`

The setup API reports whether those files are present and usable but does not expose their contents. The private key must be a regular, non-symlink file with restrictive permissions. `known_hosts` is mandatory for SSH and strict host verification is always enabled.

## Setup Modes

### Create a New Local Repository

This mode is available only when the managed repository directory is empty and does not contain a Git worktree.

The user confirms an editable initial branch name and Git author name/email. The defaults are `main`, `Porty`, and `porty@localhost`. Porty initializes the repository with the selected branch and writes the identity to repository-local Git configuration. It does not create `origin` or require credentials.

The resulting repository immediately supports stack files, status, history, and commits. Remote-only actions are hidden or disabled until a managed remote is configured.

### Use a Remote Repository

This mode requires an empty managed repository directory. It is a staged flow:

1. The user enters a remote URL and chooses public, HTTPS, or SSH authentication.
2. Porty validates the URL and credentials and runs a bounded remote inspection equivalent to `git ls-remote --symref`.
3. Porty returns the symbolic default branch, discovered branch names, and whether the remote is empty.
4. The UI preselects symbolic `HEAD`, otherwise the sole branch, and otherwise asks the user to choose from the discovered branches.
5. For an empty remote, the UI offers an editable new branch name defaulting to `main`.
6. The user confirms repository-local author identity and submits setup.

Porty initializes the fixed worktree, configures `origin`, fetches only the selected branch when one exists, and checks out a local branch that tracks the remote branch. Initializing and fetching into the fixed path is preferred over a blind `git clone --branch main`, because it supports an already-created mount point, explicit branch discovery, empty remotes, and controlled recovery.

An empty remote results in an unborn local branch with `origin` configured. Porty's explicit `git push origin <branch>` flow can publish the first commit without requiring an existing upstream ref.

### Use an Existing Mounted Repository

This mode adopts only a valid Git worktree already mounted at the managed repository path. It does not show a filesystem picker and never accepts a path from the client.

Porty validates the worktree and local Git configuration, detects the checked-out branch, and imports repository-local author identity when present. A detached `HEAD` is rejected with an actionable message directing the operator to check out a branch first. A remote is not required.

If `origin` already exists, the wizard displays its redacted URL and lets the administrator either configure it as Porty's managed remote or continue in local-only mode. In local-only mode Porty does not delete or rewrite the existing remote, but remote actions remain disabled until the administrator explicitly adopts/configures it in Repository settings.

## Authentication

Remote requests use an explicit authentication union:

- `none`: public access with no stored credentials.
- `https`: username plus token/password, supplied through the existing non-interactive askpass mechanism.
- `ssh`: fixed operator-mounted private key and `known_hosts` files.

HTTPS secrets remain write-only and are cleared from frontend state after submission. They are never returned by status or settings APIs.

SSH execution must preserve the repository rule against constructed shell commands. Git receives a fixed SSH helper executable and explicit environment marker. The helper invokes the system `ssh` executable with fixed arguments for the mounted identity, `IdentitiesOnly=yes`, the mounted `known_hosts`, and `StrictHostKeyChecking=yes`. It does not inherit `SSH_AUTH_SOCK`, user home configuration, or interactive prompting.

Remote URLs continue to permit only HTTPS and `ssh://`. Embedded passwords, query strings, fragments, local paths, and file URLs remain invalid. All command output and errors pass through existing bounded redaction.

## API Contract

All endpoints remain under `/api/v1`, require an authenticated administrator, and require CSRF protection for mutations.

### Setup Status

`GET /repository/setup/status` returns:

- Lifecycle state and whether the normal application is gated.
- Fixed repository path condition: empty, safe worktree, invalid, or non-empty non-repository.
- Modes currently available and blocking reasons.
- Detected branch, local author identity, and redacted existing `origin` for an adoptable worktree.
- Default author identity for new repositories.
- Presence/usability booleans for mounted SSH identity and `known_hosts` files.

It never returns secrets, private key content, or unrestricted host filesystem details.

### Remote Inspection

`POST /repository/setup/inspect-remote` accepts a remote URL and write-only authentication selection. It returns:

- Redacted/canonical remote display value.
- Symbolic default branch when advertised.
- Sorted discovered branch names.
- `empty: true` when no branch refs exist.
- A suggested branch following the approved selection rules.

Inspection validates access but does not mutate the managed repository or persist credentials.

### Setup Submission

`POST /repository/setup` extends the existing endpoint with:

- Mode: `init`, `remote`, or `adopt`.
- Selected branch.
- Author name and email.
- Optional remote URL and authentication union.

The service revalidates all inspected information at submission time rather than trusting frontend state. A successful response means state is persisted as `ready` and a fresh repository client is active.

### Remote Settings

After setup, Repository settings expose authenticated, CSRF-protected endpoints to inspect and add/replace/update or remove the managed `origin`.

Adding or replacing a remote probes and fetches the selected branch before persistence:

- An empty remote is compatible with the current local branch.
- A missing remote branch is compatible and can later be published.
- A related branch is accepted and normal ahead/behind/diverged status applies.
- Unrelated local and remote histories are rejected.
- An unborn local branch may adopt a populated compatible remote branch when the worktree is clean.

Removing the managed remote clears persisted remote metadata and authentication and removes Porty's `origin` configuration. It preserves the local branch, commits, files, author identity, and `ready` lifecycle state.

## Persistence

The existing schema already contains the required fields in `app_state` and `repository_auth`. Repository persistence is expanded to use them consistently:

- `app_state.setup_state`
- `app_state.repository_root`
- `app_state.remote_name`, nullable for local-only repositories
- `app_state.remote_url_redacted`, nullable and credential-free
- `app_state.tracking_branch`
- `app_state.git_author_name`
- `app_state.git_author_email`
- `repository_auth.auth_type`
- `repository_auth.https_username` and `https_secret`
- `repository_auth.ssh_key_path` and `known_hosts_path`

Local-only configuration deletes any managed authentication row and stores no remote name. SSH paths are server-derived and validated rather than accepted from the browser. Configuration changes use a transaction and update the active repository client only after persistence succeeds.

## Application Structure

The setup behavior remains behind application ports so domain and application packages do not depend on Git, SQLite, HTTP, or the host filesystem.

The setup application service coordinates:

- Setup lifecycle storage.
- Managed-path inspection.
- Remote inspection.
- Git initialization/import/adoption.
- Repository-local identity.
- Remote/authentication persistence.
- Repository client replacement.
- Repository locking and audit events.

The Git adapter gains branch discovery, empty-remote handling, local identity management, compatible-history checks, and explicit SSH execution. The SQLite adapter gains complete setup-state and authentication persistence. HTTP remains responsible only for decoding bounded requests, authorization/CSRF, stable error mapping, and response redaction.

## First-Start User Experience

The authenticated first-start screen replaces the normal application navigation with three setup cards:

- **Create local repository**
- **Use remote repository**
- **Use mounted repository**

Each flow reveals only its required fields. Remote setup uses a validate-and-discover step before branch selection. Mounted setup presents detected facts rather than asking for a path. All modes include editable author identity before confirmation.

Recoverable failures preserve non-secret form values and selected mode while displaying actionable inline errors. Secrets are cleared after each submission and never redisplayed. Loading states prevent duplicate submissions.

After setup succeeds, the frontend refreshes repository state and enters the normal workspace. Repository settings show local identity and managed remote state. Fetch, pull, and push are hidden or disabled for local-only repositories; editing and commits remain available.

The wizard and settings controls preserve the current keyboard, responsive, and mobile behavior.

## Safety and Failure Handling

- All setup mutations hold the repository lock for their full Git and persistence sequence.
- The fixed repository root is checked with existing rooted-path and symlink protections.
- New-local and remote modes refuse non-empty unmanaged directories and never overwrite an existing worktree.
- Adopt mode rejects unsafe Git configuration using the existing hostile-config checks.
- Remote inspection and setup validate URL, branch/ref, request size, output size, and time bounds.
- Credentials never appear in process arguments, logs, audit payloads, API responses, or user-visible low-level errors.
- Setup cleans up only artifacts created by the current attempt. It never recursively clears a pre-existing directory.
- Database state does not become `ready` until Git validation succeeds.
- Stable API errors include `RepositorySetupRequired`, `RepositoryPathNotEmpty`, `InvalidWorktree`, `DetachedHead`, `RemoteAuthenticationFailed`, `RemoteUnavailable`, `SSHMaterialUnavailable`, and `UnrelatedHistory`.
- Audit events record setup mode, redacted remote, selected branch, actor, outcome, and request ID without credentials.

## Testing Strategy

### Go Unit and Integration Tests

- Setup lifecycle transitions, mandatory gate decisions, retry recovery, and existing-installation reconciliation.
- Real-Git new local initialization with no remote and a successful stack-scoped commit.
- Mounted repository adoption with and without `origin`, including detached `HEAD` and hostile config rejection.
- Remote inspection for symbolic defaults not named `main`, one branch, multiple branches, no symbolic `HEAD`, and empty bare repositories.
- Remote import and checkout for selected branches and empty-remotes-first-push behavior.
- Compatible, absent, empty, and unrelated remote histories during later remote configuration.
- HTTPS askpass argument/environment behavior and complete redaction.
- SSH helper arguments, fixed paths, strict host verification, file-mode checks, missing files, and redaction.
- Cleanup and state behavior when Git or SQLite fails at each setup stage.
- HTTP authentication, CSRF, request bounds, stable errors, gating, and credentials never returned.

### Frontend Tests

- Registration transitions directly to the mandatory wizard.
- Login resumes an incomplete setup.
- All three setup paths render the correct fields and requests.
- Remote inspection preselects symbolic `HEAD`, offers multiple branches, and handles empty remotes.
- Mounted repository facts render without a path picker.
- Secrets clear while non-secret fields survive recoverable errors.
- A successful setup enters the workspace.
- Local-only repositories hide remote actions and can later add or remove `origin` through settings.

### End-to-End and Full Verification

Add a Playwright journey covering first registration, local repository setup, workspace entry, and a usable local commit flow. Run focused tests first, followed by:

- `go test ./...`
- `go vet ./...`
- `npm --prefix web test`
- `npm --prefix web run typecheck`
- `npm --prefix web run build`
- The affected Playwright journey

## Rollout Notes

Operator documentation must describe the mandatory first-start gate, the fixed mounted repository path, local-only operation, fixed SSH mount locations and permissions, branch discovery, empty-remote behavior, and later remote management. The existing instruction to configure Git identity manually is removed because setup owns repository-local identity.

Generated frontend assets are updated only as part of implementation after source tests and builds pass.
