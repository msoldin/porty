# First-Start Stack Repository Setup Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add a mandatory authenticated first-start flow that makes Porty's fixed stack repository usable as a new local-only repository, an imported remote repository, or an existing local repository, with safe remote management available later.

**Architecture:** Introduce an application-owned repository setup service that coordinates a persisted lifecycle, a Git provisioning port, the existing live `RepositoryService`, and the repository lock. Implement the port with the existing bounded `git` process adapter, persist configuration and write-only authentication in SQLite, gate normal APIs until setup is ready, and place a Preact setup wizard between authentication and the workspace. Keep every repository operation rooted at `<data-dir>/repository`; the browser never supplies a filesystem path.

**Tech Stack:** Go, SQLite, `git` and `ssh` subprocesses through the existing process runner, Preact, TypeScript, Vitest/Testing Library, Playwright.

**Spec:** `docs/superpowers/specs/2026-09-20-first-start-stack-repository-setup-design.md`

## Global Constraints

- Use the fixed repository root `<data-dir>/repository`; do not add a path field to any HTTP or frontend contract.
- Support exactly three setup modes: `init`, `remote`, and `adopt`.
- Treat setup as mandatory: authenticated stack, editor, deployment, repository-action, operation, audit, and WebSocket APIs return `409 RepositorySetupRequired` until persisted state is `ready`.
- Keep registration, login/session, password change, setup status, remote inspection, and setup submission available before repository readiness.
- A local-only repository is a complete ready state. It must not require or synthesize an `origin` remote.
- Use `Porty` and `porty@localhost` only as editable form defaults. Persist the submitted Git identity as repository-local `user.name` and `user.email`.
- Support unauthenticated, HTTPS username/secret, and fixed mounted SSH-file authentication. Never return, log, audit, or serialize a stored secret.
- Read SSH material only from `<data-dir>/ssh/id` and `<data-dir>/ssh/known_hosts`. Reject symlinks, non-regular files, and permissive private-key modes.
- Use strict host verification, batch mode, and a Porty SSH helper invoked via `GIT_SSH`; never construct a shell command or depend on an SSH agent or home-directory configuration.
- Discover remote branches with bounded `git ls-remote --symref`. Prefer symbolic `HEAD`, then the sole branch, then an explicit user choice; suggest `main` only for an empty remote.
- Use `git init`, targeted `fetch`, and explicit checkout for remote import. Do not assume `main` and do not use an opaque `git clone` path.
- Serialize setup and remote mutations with the repository write lock. Make retries safe after either Git mutation or persistence fails.
- Preserve an unmanaged existing `origin` unless the user explicitly chooses to manage or replace it. Removing Porty's managed remote must leave the local repository and ready state intact.
- Keep command output and request sizes bounded. Preserve existing URL, branch, rooted-path, hostile-config, redaction, CSRF, and origin protections.
- Do not introduce a new Git library or credential dependency.
- The repository currently contains unrelated edits. Implement in an isolated worktree and stage only files named by each task.

## File Map

**Create**

- `internal/domain/repository_setup.go` — setup states, requests, safe responses, path inspection, and persisted configuration types.
- `internal/application/repository_setup.go` — setup service, provisioning/store ports, validation, lifecycle reconciliation, and stable errors.
- `internal/application/repository_setup_test.go` — service orchestration, lifecycle, retry, locking, and secret-handling tests.
- `internal/infrastructure/sqlite/repository_store_test.go` — full configuration/auth persistence and clearing tests.
- `internal/infrastructure/gitcli/setup.go` — fixed-root inspection, branch discovery, provisioning, adoption, and remote mutation implementation.
- `internal/infrastructure/gitcli/setup_test.go` — real-repository and recording-runner setup tests.
- `cmd/porty/git_helper.go` and `cmd/porty/git_helper_test.go` — askpass and constrained SSH helper dispatch.
- `internal/httpapi/repository_setup_test.go` — setup endpoint, readiness-gate, CSRF, status, and stable-error tests.
- `web/src/RepositorySetup.tsx` and `web/src/RepositorySetup.test.tsx` — mandatory setup chooser and workflow.
- `web/src/RepositorySettings.tsx` and `web/src/RepositorySettings.test.tsx` — later remote controls.

**Modify**

- `internal/domain/api.go`
- `internal/application/repository.go`
- `internal/application/repository_test.go`
- `internal/infrastructure/sqlite/repository_store.go`
- `internal/infrastructure/gitcli/client.go`
- `internal/infrastructure/gitcli/client_test.go`
- `cmd/porty/main.go`
- `cmd/porty/main_test.go`
- `internal/httpapi/auth.go`
- `internal/httpapi/api.go`
- `internal/httpapi/api_test.go`
- `internal/httpapi/auth_test.go`
- `internal/httpapi/stream_test.go`
- `web/src/api.ts`
- `web/src/App.tsx`
- `web/src/App.test.tsx`
- `web/src/Auth.tsx`
- `web/src/AccountSettings.tsx`
- `web/src/styles.css`
- `web/e2e/porty.spec.ts`
- `docs/operator-guide.md`
- `web/dist/` after source verification.

## Core Contracts

Add the public setup model in `internal/domain/repository_setup.go`:

```go
type RepositorySetupState string

const (
	RepositorySetupUnregistered RepositorySetupState = "unregistered"
	RepositorySetupRegistered   RepositorySetupState = "registered"
	RepositorySetupReady        RepositorySetupState = "ready"
)

type RepositorySetupMode string

const (
	RepositorySetupInit   RepositorySetupMode = "init"
	RepositorySetupRemote RepositorySetupMode = "remote"
	RepositorySetupAdopt  RepositorySetupMode = "adopt"
)

type RepositoryAuthType string

const (
	RepositoryAuthNone  RepositoryAuthType = "none"
	RepositoryAuthHTTPS RepositoryAuthType = "https"
	RepositoryAuthSSH   RepositoryAuthType = "ssh"
)

type RepositoryPathState string

const (
	RepositoryPathEmpty    RepositoryPathState = "empty"
	RepositoryPathWorktree RepositoryPathState = "worktree"
	RepositoryPathOccupied RepositoryPathState = "occupied"
	RepositoryPathInvalid  RepositoryPathState = "invalid"
)

type GitIdentity struct {
	Name  string `json:"name"`
	Email string `json:"email"`
}

type RemoteAuthenticationInput struct {
	Type     RepositoryAuthType `json:"type"`
	Username string             `json:"username,omitempty"`
	Secret   string             `json:"secret,omitempty"`
}

type RepositoryRemoteInput struct {
	URL            string                    `json:"url"`
	Authentication RemoteAuthenticationInput `json:"authentication"`
}

type RepositorySetupRequest struct {
	Mode                 RepositorySetupMode    `json:"mode"`
	Branch               string                 `json:"branch"`
	Author               GitIdentity            `json:"author"`
	Remote               *RepositoryRemoteInput `json:"remote,omitempty"`
	ManageExistingRemote bool                   `json:"manageExistingRemote,omitempty"`
}

type RemoteInspectionRequest struct {
	Remote RepositoryRemoteInput `json:"remote"`
}

type RepositoryRemoteRequest struct {
	Remote          RepositoryRemoteInput `json:"remote"`
	Branch          string                `json:"branch"`
	ReplaceExisting bool                  `json:"replaceExisting"`
}
```

Keep persisted credentials non-JSON and status responses secret-free:

```go
type RepositoryAuthentication struct {
	Type           RepositoryAuthType
	Username       string
	Secret         string
	SSHKeyPath     string
	KnownHostsPath string
}

type RepositoryRemoteSummary struct {
	Name     string             `json:"name"`
	URL      string             `json:"url"`
	AuthType RepositoryAuthType `json:"authType"`
	Managed  bool               `json:"managed"`
}

type RepositoryConfiguration struct {
	State  RepositorySetupState
	Root   string
	Branch string
	Author GitIdentity
	Remote *RepositoryRemoteSummary
}

type RepositoryModeAvailability struct {
	Mode      RepositorySetupMode `json:"mode"`
	Available bool                `json:"available"`
	Reason    string              `json:"reason,omitempty"`
}

type SSHMaterialStatus struct {
	IdentityAvailable   bool `json:"identityAvailable"`
	KnownHostsAvailable bool `json:"knownHostsAvailable"`
	Usable              bool `json:"usable"`
}

type RepositorySetupStatus struct {
	State          RepositorySetupState         `json:"state"`
	Required       bool                         `json:"required"`
	PathState      RepositoryPathState          `json:"pathState"`
	Modes          []RepositoryModeAvailability `json:"modes"`
	Branch         string                       `json:"branch,omitempty"`
	Author         GitIdentity                  `json:"author"`
	DefaultAuthor  GitIdentity                  `json:"defaultAuthor"`
	ExistingRemote *RepositoryRemoteSummary     `json:"existingRemote,omitempty"`
	ManagedRemote  *RepositoryRemoteSummary     `json:"managedRemote,omitempty"`
	SSH            SSHMaterialStatus            `json:"ssh"`
}

type RemoteInspection struct {
	RemoteURL     string   `json:"remoteUrl"`
	DefaultBranch string   `json:"defaultBranch,omitempty"`
	Branches      []string `json:"branches"`
	Empty         bool     `json:"empty"`
	Suggested     string   `json:"suggestedBranch"`
}
```

Define path inspection explicitly. `Reason` is a stable safe explanation and never contains raw Git output:

```go
type RepositoryPathInspection struct {
	State          RepositoryPathState
	Branch         string
	Author         GitIdentity
	ExistingRemote *RepositoryRemoteSummary
	Detached       bool
	Reason         string
}
```

The application service owns these ports:

```go
type RepositorySetupStore interface {
	Load(context.Context) (domain.RepositoryConfiguration, domain.RepositoryAuthentication, error)
	Save(context.Context, domain.RepositoryConfiguration, domain.RepositoryAuthentication) error
}

type RepositoryProvisionRequest struct {
	Mode                 domain.RepositorySetupMode
	Branch               string
	Author               domain.GitIdentity
	RemoteURL            string
	ManageExistingRemote bool
	Authentication       domain.RepositoryAuthentication
}

type RepositoryRemoteProvisionRequest struct {
	RemoteURL       string
	Branch          string
	ReplaceExisting bool
	Authentication domain.RepositoryAuthentication
}

type RepositoryProvisioner interface {
	InspectPath(context.Context) (domain.RepositoryPathInspection, error)
	InspectRemote(context.Context, string, domain.RepositoryAuthentication) (domain.RemoteInspection, error)
	Provision(context.Context, RepositoryProvisionRequest) (GitRepository, domain.RepositoryConfiguration, error)
	ConfigureRemote(context.Context, RepositoryRemoteProvisionRequest, domain.RepositoryConfiguration) (GitRepository, domain.RepositoryConfiguration, error)
	RemoveRemote(context.Context, domain.RepositoryConfiguration) (GitRepository, domain.RepositoryConfiguration, error)
	Open(context.Context, domain.RepositoryConfiguration, domain.RepositoryAuthentication) (GitRepository, error)
}

type RepositorySetupOptions struct {
	SSHKeyPath     string
	KnownHostsPath string
}

func NewRepositorySetupService(
	store RepositorySetupStore,
	provisioner RepositoryProvisioner,
	repository *RepositoryService,
	coordinator *Coordinator,
	options RepositorySetupOptions,
) *RepositorySetupService
```

## Stable Errors

Declare these in `internal/application/repository_setup.go`:

| Error | HTTP | Meaning |
| --- | --- | --- |
| `ErrRepositorySetupRequired` | 409 | Normal application API called before ready. |
| `ErrRepositoryPathNotEmpty` | 409 | Init/import requires an empty fixed directory. |
| `ErrInvalidWorktree` | 409 | Fixed path is not a safe worktree. |
| `ErrDetachedHead` | 409 | Adoption found detached `HEAD`. |
| `ErrRemoteAuthenticationFailed` | 401 | Git authentication failed. |
| `ErrRemoteUnavailable` | 502 | Remote command failed otherwise. |
| `ErrSSHMaterialUnavailable` | 409 | Fixed SSH files are missing or unsafe. |
| `ErrUnrelatedHistory` | 409 | Local and remote histories have no merge base. |
| `ErrRepositoryRemoteConflict` | 409 | Unmanaged `origin` needs explicit replacement. |
| `ErrRepositoryRemoteUnavailable` | 409 | Remote action requested for local-only configuration. |

Unsupported mode/auth type, invalid identity/branch, missing URL, and inconsistent fields remain `400 InvalidRequest`.

## Review Focus

- Symlink swaps cannot initialize outside the root or substitute SSH files; checks repeat immediately before execution.
- Concurrent submissions, persistence failures, and retries cannot expose a half-configured client.
- Malformed, oversized, duplicate, or adversarial remote refs fail closed without raw output reflection.
- Local-only/adopt/remove never delete an unmanaged remote; replacement requires explicit approval.
- Persisted ready state with missing, hostile, detached, or credential-incomplete storage cannot pass reconciliation or normal API gates.

---

## Task 1: Add Domain Types and Application Setup Orchestration

**Files:**
- Create: `internal/domain/repository_setup.go`
- Create: `internal/application/repository_setup.go`
- Create: `internal/application/repository_setup_test.go`
- Modify: `internal/domain/api.go`

- [x] **Step 1: Write failing service tests with fakes**

```go
func TestRepositorySetupStatusOffersModesForEmptyPath(t *testing.T)
func TestRepositorySetupStatusOffersOnlyAdoptForSafeWorktree(t *testing.T)
func TestRepositorySetupRejectsInvalidIdentityBeforeProvisioning(t *testing.T)
func TestRepositorySetupInitializesLocalRepositoryWithoutRemote(t *testing.T)
func TestRepositorySetupImportsInspectedRemoteBranch(t *testing.T)
func TestRepositorySetupDoesNotReplaceLiveClientWhenPersistenceFails(t *testing.T)
func TestRepositorySetupRetryRecoversAfterProvisioningSucceeded(t *testing.T)
func TestRepositorySetupSerializesConcurrentMutations(t *testing.T)
func TestRepositorySetupReconcilesRegisteredExistingRepository(t *testing.T)
func TestRepositorySetupReadyRejectsTamperedRepository(t *testing.T)
func TestRepositorySetupResponseNeverContainsSecret(t *testing.T)
```

Use fake store/provisioner implementations and the real `Coordinator`. Record maximum concurrent provisioner calls and assert it is one.

- [x] **Step 2: Run the focused test and confirm failure**

Run: `go test ./internal/application -run 'TestRepositorySetup'`

Expected: compile failures for missing types.

- [x] **Step 3: Add the Core Contracts types**

Add all request, response, configuration, path, constant, and authentication types. Remove the superseded flat setup request from `internal/domain/api.go` after references move.

- [x] **Step 4: Implement validation and orchestration**

```go
const (
	defaultGitAuthorName   = "Porty"
	defaultGitAuthorEmail  = "porty@localhost"
	remoteInspectTimeout   = 30 * time.Second
	repositorySetupTimeout = 2 * time.Minute
)

type RepositorySetupService struct {
	store       RepositorySetupStore
	provisioner RepositoryProvisioner
	repository  *RepositoryService
	coordinator *Coordinator
}
```

Implement `Status`, `InspectRemote`, `Setup`, `ConfigureRemote`, `RemoveRemote`, `Ready`, and `Reconcile`.

- Trim identities; require name length `1..128` and email length `3..254`.
- Reject NUL, CR, LF, and Unicode controls. Parse with `net/mail.ParseAddress` and require the parsed address to equal the trimmed input.
- Require a valid branch for mutation. Empty remote inspection suggests `main` but does not submit it automatically.
- Require no remote for `init`, a remote for `remote`, and no remote input for `adopt` because the adapter detects fixed-root `origin`.
- Convert input auth to non-JSON auth before calling ports. SSH fills only server-configured fixed paths.
- Hold `coordinator.Try(true, "")` across provision/configure/remove, persistence, and client replacement.
- Save before `repository.Replace`; persistence failure leaves the prior live client active.
- Build responses from saved configuration, never the secret-bearing request.

Reconciliation behavior:

- `registered`: import a safe existing worktree; use default identity only if local identity is absent; save ready and activate.
- `ready`: open and validate persisted repo/auth; missing, hostile, detached, or unusable state fails startup readiness.
- `unregistered`: do nothing.

Retry asks the provisioner to accept only an exact prior-attempt branch/identity/remote/history; never delete mismatched state.

- [x] **Step 5: Run and pass**

Run: `gofmt -w internal/domain/repository_setup.go internal/domain/api.go internal/application/repository_setup.go internal/application/repository_setup_test.go && go test ./internal/application -run 'TestRepositorySetup'`

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/domain/repository_setup.go internal/domain/api.go internal/application/repository_setup.go internal/application/repository_setup_test.go
git commit -m "feat: add repository setup orchestration"
```

## Task 2: Persist Lifecycle and Authentication

**Files:**
- Modify: `internal/infrastructure/sqlite/repository_store.go`
- Create: `internal/infrastructure/sqlite/repository_store_test.go`

- [x] **Step 1: Write failing store tests**

```go
func TestRepositoryStoreLoadsRegisteredDefaultsWithoutAuthRow(t *testing.T)
func TestRepositoryStoreSavesLocalOnlyReadyConfiguration(t *testing.T)
func TestRepositoryStoreRoundTripsHTTPSAuthentication(t *testing.T)
func TestRepositoryStoreRoundTripsSSHAuthentication(t *testing.T)
func TestRepositoryStoreReplacingAuthClearsOldSecretColumns(t *testing.T)
func TestRepositoryStoreRemovingRemoteKeepsReadyState(t *testing.T)
func TestRepositoryStoreSaveRollsBackConfigurationWhenAuthWriteFails(t *testing.T)
```

Inspect SQL directly to prove local-only remote fields are `NULL`, HTTPS-to-SSH clears HTTPS secrets, and removal deletes the auth row.

- [x] **Step 2: Run and confirm failure**

Run: `go test ./internal/infrastructure/sqlite -run 'TestRepositoryStore'`

Expected: old store contract fails.

- [x] **Step 3: Implement transactional `Load` and `Save`**

Use existing `app_state` and `repository_auth` columns; add no migration. One transaction updates setup state/root/remote/branch/author and then:

- `none`: delete auth row.
- `https`: store username/secret and set SSH columns `NULL`.
- `ssh`: store fixed SSH paths and set HTTPS columns `NULL`.

Reject unknown stored `auth_type`.

- [x] **Step 4: Run and pass**

Run: `gofmt -w internal/infrastructure/sqlite/repository_store.go internal/infrastructure/sqlite/repository_store_test.go && go test ./internal/infrastructure/sqlite -run 'TestRepositoryStore'`

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add internal/infrastructure/sqlite/repository_store.go internal/infrastructure/sqlite/repository_store_test.go
git commit -m "feat: persist repository setup state"
```

## Task 3: Implement Fixed-Root Inspection, Init, and Adoption

**Files:**
- Create: `internal/infrastructure/gitcli/setup.go`
- Create: `internal/infrastructure/gitcli/setup_test.go`
- Modify: `internal/infrastructure/gitcli/client.go`

- [x] **Step 1: Write failing fixed-root tests**

```go
func TestProvisionerInspectPathClassifiesEmptyDirectory(t *testing.T)
func TestProvisionerInspectPathRejectsOccupiedNonRepository(t *testing.T)
func TestProvisionerInspectPathRejectsRepositoryRootSymlink(t *testing.T)
func TestProvisionerInspectPathRejectsHostileGitConfiguration(t *testing.T)
func TestProvisionerProvisionInitCreatesRequestedUnbornBranchAndIdentity(t *testing.T)
func TestProvisionerProvisionInitDoesNotCreateOrigin(t *testing.T)
func TestProvisionerProvisionInitRejectsNonEmptyDirectory(t *testing.T)
func TestProvisionerProvisionAdoptDetectsBranchIdentityAndOrigin(t *testing.T)
func TestProvisionerProvisionAdoptRejectsDetachedHead(t *testing.T)
func TestProvisionerProvisionAdoptLeavesIgnoredOriginUntouched(t *testing.T)
func TestProvisionerProvisionRetryAcceptsExactInitializedRepository(t *testing.T)
```

Use temporary real repositories for filesystem behavior and the recording runner for exact commands.

- [x] **Step 2: Run and confirm failure**

Run: `go test ./internal/infrastructure/gitcli -run 'TestProvisioner.*(InspectPath|Init|Adopt|Retry)'`

Expected: missing `Provisioner`.

- [x] **Step 3: Implement fixed-root inspection**

- Construct with cleaned repository root, process runner, current executable, and fixed SSH paths.
- `Lstat` root and `.git`; reject symlinks and escape from configured data root.
- Classify empty, valid worktree, occupied, or invalid.
- Use `git symbolic-ref --quiet --short HEAD`; status `1` maps to detached head.
- Read local identity and `origin`; missing values are valid.
- Run hostile-config validation before marking adoptable.

- [x] **Step 4: Implement init and adopt**

Init uses fixed arrays equivalent to:

```text
git init -b <branch> <fixed-root>
git -C <fixed-root> config --local user.name <name>
git -C <fixed-root> config --local user.email <email>
```

Adopt requires a safe symbolic branch and submitted branch equality, writes submitted local identity, and either manages a validated existing origin or leaves it untouched/unmanaged. Create the active client only after final safety validation. Init retry accepts existing state only when branch, identity, and no managed remote match exactly.

- [x] **Step 5: Run and pass**

Run: `gofmt -w internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go internal/infrastructure/gitcli/client.go && go test ./internal/infrastructure/gitcli -run 'TestProvisioner.*(InspectPath|Init|Adopt|Retry)'`

Expected: PASS.

- [x] **Step 6: Commit**

```bash
git add internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go internal/infrastructure/gitcli/client.go
git commit -m "feat: provision local stack repositories"
```

## Task 4: Discover Branches and Import Remote Repositories

**Files:**
- Modify: `internal/infrastructure/gitcli/setup.go`
- Modify: `internal/infrastructure/gitcli/setup_test.go`

- [x] **Step 1: Add failing inspection parser tests**

```go
func TestProvisionerInspectRemoteUsesSymbolicHEAD(t *testing.T)
func TestProvisionerInspectRemoteUsesSoleBranchWithoutHEAD(t *testing.T)
func TestProvisionerInspectRemoteRequiresChoiceForMultipleBranchesWithoutHEAD(t *testing.T)
func TestProvisionerInspectRemoteSuggestsMainForEmptyRemote(t *testing.T)
func TestProvisionerInspectRemoteSortsAndDeduplicatesBranches(t *testing.T)
func TestProvisionerInspectRemoteRejectsInvalidBranchRef(t *testing.T)
func TestProvisionerInspectRemoteRejectsOversizedOutput(t *testing.T)
func TestProvisionerInspectRemoteMapsAuthenticationFailure(t *testing.T)
func TestProvisionerInspectRemoteMapsOtherFailureToUnavailable(t *testing.T)
```

Use exact records including:

```text
ref: refs/heads/trunk\tHEAD
0123456789012345678901234567890123456789\tHEAD
0123456789012345678901234567890123456789\trefs/heads/trunk
```

- [x] **Step 2: Add failing import tests**

```go
func TestProvisionerRemoteImportFetchesSelectedBranch(t *testing.T)
func TestProvisionerRemoteImportCreatesTrackingBranch(t *testing.T)
func TestProvisionerRemoteImportSupportsEmptyRemote(t *testing.T)
func TestProvisionerRemoteImportRejectsUnadvertisedBranch(t *testing.T)
func TestProvisionerRemoteImportRetryAcceptsExactPartialRepository(t *testing.T)
func TestProvisionerRemoteImportRetryRejectsMismatchedOrigin(t *testing.T)
```

- [x] **Step 3: Run and confirm failure**

Run: `go test ./internal/infrastructure/gitcli -run 'TestProvisioner.*Remote'`

Expected: failing parser/import tests.

- [x] **Step 4: Implement bounded branch discovery**

Run with a 30-second context and `MaxOutput: 1 << 20`:

```text
git ls-remote --symref <validated-url> HEAD refs/heads/*
```

Parse only tab-separated `ref:` and object-ID records. Accept only `refs/heads/<validated-branch>`, cap branches at 1,000, sort/deduplicate, and fail on malformed or duplicate symbolic `HEAD` records.

Selection order:

1. Symbolic `HEAD` naming an advertised branch.
2. The only advertised branch.
3. No default when multiple branches lack usable `HEAD`.
4. Empty remote returns `Empty: true` and `Suggested: "main"`.

Return a canonical URL with userinfo removed. Classify stderr through redacted authentication markers; never return stderr.

- [x] **Step 5: Implement targeted import**

For an advertised branch:

```text
git init -b <branch> <fixed-root>
git -C <fixed-root> remote add origin <url>
git -C <fixed-root> fetch --no-tags origin refs/heads/<branch>:refs/remotes/origin/<branch>
git -C <fixed-root> checkout -B <branch> --track origin/<branch>
git -C <fixed-root> config --local user.name <name>
git -C <fixed-root> config --local user.email <email>
```

For an empty remote, stop after init, origin creation, and identity configuration, leaving an unborn branch. Apply auth to inspection, fetch, and returned client. On failure remove only artifacts created by that call; never recursively delete a pre-existing directory.

- [x] **Step 6: Run and pass**

Run: `gofmt -w internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go && go test ./internal/infrastructure/gitcli -run 'TestProvisioner.*Remote'`

Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go
git commit -m "feat: discover and import remote repositories"
```

## Task 5: Add HTTPS and Fixed-File SSH Authentication

**Files:**
- Create: `cmd/porty/git_helper.go`
- Create: `cmd/porty/git_helper_test.go`
- Modify: `cmd/porty/main.go`
- Modify: `internal/infrastructure/gitcli/client.go`
- Modify: `internal/infrastructure/gitcli/client_test.go`
- Modify: `internal/infrastructure/gitcli/setup.go`
- Modify: `internal/infrastructure/gitcli/setup_test.go`

- [x] **Step 1: Write failing helper/auth tests**

```go
func TestGitAskpassHelperReturnsUsernameAndSecretByPrompt(t *testing.T)
func TestGitAskpassHelperRejectsUnexpectedInvocation(t *testing.T)
func TestGitSSHHelperPrependsFixedSecurityArguments(t *testing.T)
func TestGitSSHHelperRejectsUnsafeGitArguments(t *testing.T)
func TestGitSSHHelperPropagatesExitCodeWithoutPrintingSecretState(t *testing.T)
func TestClientHTTPSAuthenticationUsesAskpassWithoutSecretArguments(t *testing.T)
func TestClientSSHAuthenticationUsesFixedFilesWithoutShellCommand(t *testing.T)
func TestProvisionerRejectsMissingSSHMaterial(t *testing.T)
func TestProvisionerRejectsSymlinkedSSHMaterial(t *testing.T)
func TestProvisionerRejectsPermissivePrivateKey(t *testing.T)
```

- [x] **Step 2: Run and confirm failure**

Run: `go test ./cmd/porty ./internal/infrastructure/gitcli -run 'Test(Git|Client.*Authentication|ProvisionerRejects.*SSH|ProvisionerRejectsPermissive)'`

Expected: missing SSH helper/auth APIs.

- [x] **Step 3: Extract helper dispatch from `main`**

```go
type commandRunner func(context.Context, string, ...string) error

func runGitHelper(
	ctx context.Context,
	args []string,
	lookup func(string) (string, bool),
	stdout io.Writer,
	run commandRunner,
) (handled bool, exitCode int)
```

Askpass writes only the requested username or secret. SSH mode requires `PORTY_GIT_SSH=1`, reads only private environment paths, validates Git-supplied arguments, and invokes:

```text
ssh -i <fixed-key> -o IdentitiesOnly=yes -o UserKnownHostsFile=<fixed-known-hosts> -o StrictHostKeyChecking=yes -o BatchMode=yes <validated-git-arguments>
```

Permit only SSH protocol/version/port negotiation options and a destination plus `git-upload-pack` or `git-receive-pack` repository command. Reject alternate identity/config/proxy/known-host options and arbitrary commands.

- [x] **Step 4: Validate SSH files before every Git command**

- Paths must equal server-configured fixed paths.
- `Lstat` must report regular, non-symlink files.
- Private key must satisfy `mode.Perm() & 0o077 == 0`.
- Known hosts must satisfy `mode.Perm() & 0o022 == 0`.

Use `GIT_SSH=<absolute-porty-executable>`, `GIT_SSH_VARIANT=ssh`, `PORTY_GIT_SSH=1`, and private fixed-path variables. Do not use `GIT_SSH_COMMAND`.

- [x] **Step 5: Keep credentials out of arguments and errors**

HTTPS retains askpass with terminal prompts disabled. Extend redaction for secret and username-bearing URL forms. Active clients receive the same sanitized auth environment as provisioning.

- [x] **Step 6: Run and pass**

Run: `gofmt -w cmd/porty/git_helper.go cmd/porty/git_helper_test.go cmd/porty/main.go internal/infrastructure/gitcli/client.go internal/infrastructure/gitcli/client_test.go internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go && go test ./cmd/porty ./internal/infrastructure/gitcli -run 'Test(Git|Client.*Authentication|ProvisionerRejects.*SSH|ProvisionerRejectsPermissive)'`

Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add cmd/porty/git_helper.go cmd/porty/git_helper_test.go cmd/porty/main.go internal/infrastructure/gitcli/client.go internal/infrastructure/gitcli/client_test.go internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go
git commit -m "feat: support repository git authentication"
```

## Task 6: Add Safe Remote Management and Local-Only Semantics

**Files:**
- Modify: `internal/application/repository.go`
- Modify: `internal/application/repository_test.go`
- Modify: `internal/application/repository_setup_test.go`
- Modify: `internal/infrastructure/gitcli/setup.go`
- Modify: `internal/infrastructure/gitcli/setup_test.go`

- [x] **Step 1: Write failing application tests**

```go
func TestRepositoryServiceRejectsFetchWithoutManagedRemote(t *testing.T)
func TestRepositoryServiceRejectsPullWithoutManagedRemote(t *testing.T)
func TestRepositoryServiceRejectsPushWithoutManagedRemote(t *testing.T)
func TestRepositoryServiceEnablesRemoteActionsAfterClientReplacement(t *testing.T)
func TestRepositorySetupRemoveRemoteKeepsRepositoryReady(t *testing.T)
```

Change `RepositoryService.Replace` to accept `(client GitRepository, remoteEnabled bool)`. Guard only fetch/pull/push.

- [x] **Step 2: Write failing adapter tests**

```go
func TestProvisionerConfigureRemoteAcceptsEmptyRemote(t *testing.T)
func TestProvisionerConfigureRemoteAcceptsRelatedHistory(t *testing.T)
func TestProvisionerConfigureRemoteRejectsUnrelatedHistory(t *testing.T)
func TestProvisionerConfigureRemoteAdoptsPopulatedRemoteFromCleanUnbornRepository(t *testing.T)
func TestProvisionerConfigureRemoteRejectsDirtyUnbornRepository(t *testing.T)
func TestProvisionerConfigureRemoteRequiresConfirmationForUnmanagedOrigin(t *testing.T)
func TestProvisionerConfigureRemoteCleansTemporaryRemoteAfterFailure(t *testing.T)
func TestProvisionerRemoveRemoteRemovesOnlyManagedOrigin(t *testing.T)
func TestProvisionerRemoveRemoteLeavesUnmanagedOriginUntouched(t *testing.T)
```

- [x] **Step 3: Run and confirm failure**

Run: `go test ./internal/application ./internal/infrastructure/gitcli -run 'Test(RepositoryService.*Remote|RepositorySetupRemoveRemote|ProvisionerConfigureRemote|ProvisionerRemoveRemote)'`

Expected: missing remote state/methods.

- [x] **Step 4: Probe compatibility before changing `origin`**

Use reserved temporary remote `porty-candidate` after proving it is absent. Fetch the branch to `refs/remotes/porty-candidate/<branch>`.

- Missing or empty branch is compatible.
- Local and remote commits are compatible when `git merge-base <local> <candidate>` succeeds, including divergent related histories.
- No merge base returns `ErrUnrelatedHistory`.
- Unborn local may adopt populated remote only when `git status --porcelain=v1 -z` is empty; then check out the candidate branch.
- Existing unmanaged `origin` requires `ReplaceExisting: true` or returns `ErrRepositoryRemoteConflict`.

Only after validation, set/replace `origin`, clean the temporary remote, persist the redacted summary/auth, and swap clients. Deferred cleanup removes `porty-candidate` on every error.

- [x] **Step 5: Implement managed removal**

Remove `origin` only when persisted configuration says it is managed and named `origin`. Missing Git remote is idempotent. Return ready local-only configuration and disable remote actions. Never remove unmanaged origin.

- [x] **Step 6: Run and pass**

Run: `gofmt -w internal/application/repository.go internal/application/repository_test.go internal/application/repository_setup_test.go internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go && go test ./internal/application ./internal/infrastructure/gitcli -run 'Test(RepositoryService.*Remote|RepositorySetupRemoveRemote|ProvisionerConfigureRemote|ProvisionerRemoveRemote)'`

Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add internal/application/repository.go internal/application/repository_test.go internal/application/repository_setup_test.go internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go
git commit -m "feat: manage repository remotes safely"
```

## Task 7: Wire Startup Reconciliation and Remove Inline Setup

**Files:**
- Modify: `cmd/porty/main.go`
- Modify: `cmd/porty/main_test.go`

- [x] **Step 1: Replace the old setup test with failing startup tests**

```go
func TestBuildHandlerLeavesRegisteredEmptyInstallInSetupState(t *testing.T)
func TestBuildHandlerReconcilesRegisteredExistingRepository(t *testing.T)
func TestBuildHandlerOpensReadyLocalOnlyRepository(t *testing.T)
func TestBuildHandlerRestoresReadyHTTPSRemoteClient(t *testing.T)
func TestBuildHandlerRestoresReadySSHRemoteClient(t *testing.T)
func TestBuildHandlerFailsReadinessForTamperedReadyRepository(t *testing.T)
```

Delete tests of the local `repositorySetup` type once that type is removed.

- [x] **Step 2: Run and confirm failure**

Run: `go test ./cmd/porty -run 'TestBuildHandler.*Repository|TestBuildHandler.*SetupState'`

Expected: failing wiring assertions.

- [x] **Step 3: Wire the new service**

1. Compute repository and SSH paths server-side.
2. Construct SQLite store and Git provisioner.
3. Load configuration/auth.
4. Create the initial `RepositoryService` with persisted managed-remote state or a safe unconfigured client.
5. Construct `RepositorySetupService` with shared `Coordinator` and repository service.
6. Call `Reconcile` before exposing routes.
7. Pass the service through `httpapi.RouterOptions`.

Remove inline `repositorySetup`, `SetupRepository`, and missing-branch-means-main behavior. Setup-incomplete remains healthy; only persisted-ready reconciliation failure prevents normal readiness/startup.

- [x] **Step 4: Run and pass**

Run: `gofmt -w cmd/porty/main.go cmd/porty/main_test.go && go test ./cmd/porty -run 'TestBuildHandler.*Repository|TestBuildHandler.*SetupState'`

Expected: PASS.

- [x] **Step 5: Commit**

```bash
git add cmd/porty/main.go cmd/porty/main_test.go
git commit -m "feat: reconcile repository setup at startup"
```

## Task 8: Add Setup APIs, Readiness Gates, and Stable Errors

**Files:**
- Modify: `internal/httpapi/auth.go`
- Modify: `internal/httpapi/api.go`
- Modify: `internal/httpapi/api_test.go`
- Modify: `internal/httpapi/auth_test.go`
- Modify: `internal/httpapi/stream_test.go`
- Create: `internal/httpapi/repository_setup_test.go`

- [x] **Step 1: Add failing endpoint tests**

Cover:

```text
GET    /api/v1/repository/setup/status
POST   /api/v1/repository/setup/inspect-remote
POST   /api/v1/repository/setup
PUT    /api/v1/repository/remote
DELETE /api/v1/repository/remote
```

Add:

```go
func TestRepositorySetupStatusRequiresAuthenticationButNotReadyState(t *testing.T)
func TestRepositoryRemoteInspectionRequiresCSRFButNotReadyState(t *testing.T)
func TestRepositorySetupAcceptsLocalOnlyRequest(t *testing.T)
func TestRepositorySetupRejectsOversizedBody(t *testing.T)
func TestRepositorySetupNeverReturnsSubmittedSecret(t *testing.T)
func TestRepositoryRemoteSettingsRequireReadyState(t *testing.T)
func TestRepositorySetupErrorsUseStableCodes(t *testing.T)
func TestNormalReadRouteRequiresRepositorySetup(t *testing.T)
func TestNormalMutationRouteRequiresRepositorySetup(t *testing.T)
func TestStreamRequiresRepositorySetupBeforeUpgrade(t *testing.T)
func TestPasswordChangeRemainsAvailableBeforeRepositorySetup(t *testing.T)
func TestRepositorySetupAuditContainsNoSecretOrRawRemoteURL(t *testing.T)
```

- [x] **Step 2: Run and confirm failure**

Run: `go test ./internal/httpapi -run 'Test(RepositorySetup|RepositoryRemote|Normal.*RequiresRepository|StreamRequiresRepository|PasswordChangeRemains)'`

Expected: missing routes/gates.

- [x] **Step 3: Expand the HTTP port**

```go
type RepositorySetupAPI interface {
	Status(context.Context) (domain.RepositorySetupStatus, error)
	InspectRemote(context.Context, domain.RemoteInspectionRequest) (domain.RemoteInspection, error)
	Setup(context.Context, domain.RepositorySetupRequest) (domain.RepositorySetupStatus, error)
	ConfigureRemote(context.Context, domain.RepositoryRemoteRequest) (domain.RepositorySetupStatus, error)
	RemoveRemote(context.Context) (domain.RepositorySetupStatus, error)
	Ready(context.Context) (bool, error)
}
```

Add authenticated wrappers that skip readiness only for setup status/inspection/submission and password change. Mutations still require CSRF and origin enforcement.

- [x] **Step 4: Gate normal HTTP and WebSocket routes**

After authentication, call `Ready`. If false, return:

```json
{"error":{"code":"RepositorySetupRequired","message":"Configure the stack repository before using Porty."}}
```

Gate stack, workspace/file, environment, repository status/history/actions, operation, deployment, audit, and WebSocket routes. Do not gate health/readiness or auth routes.

- [x] **Step 5: Bound decoding, map errors, and audit safely**

Use the strict JSON decoder/body limit and map the Stable Errors table. Never include wrapped Git errors, stderr, raw URL, username, or secret in responses.

Use a dedicated setup mutation wrapper so one audit event records action `repository.setup.<mode>`, `repository.remote.configure`, or `repository.remote.remove`; submitted branch; service-returned redacted remote on success; actor/request ID/outcome/stable code. Never audit decoded auth or raw submitted URL, and avoid duplicate generic mutation audit events.

- [x] **Step 6: Run and pass**

Run: `gofmt -w internal/httpapi/auth.go internal/httpapi/api.go internal/httpapi/api_test.go internal/httpapi/auth_test.go internal/httpapi/stream_test.go internal/httpapi/repository_setup_test.go && go test ./internal/httpapi -run 'Test(RepositorySetup|RepositoryRemote|Normal.*RequiresRepository|StreamRequiresRepository|PasswordChangeRemains)'`

Expected: PASS.

- [x] **Step 7: Commit**

```bash
git add internal/httpapi/auth.go internal/httpapi/api.go internal/httpapi/api_test.go internal/httpapi/auth_test.go internal/httpapi/stream_test.go internal/httpapi/repository_setup_test.go
git commit -m "feat: expose repository setup APIs"
```

## Task 9: Add the Authenticated First-Start Gate

**Files:**
- Modify: `web/src/api.ts`
- Create: `web/src/RepositorySetup.tsx`
- Create: `web/src/RepositorySetup.test.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.test.tsx`
- Modify: `web/src/Auth.tsx`
- Modify: `web/src/styles.css`

- [ ] **Step 1: Write failing wizard tests**

Cover user-visible behavior:

```text
shows all three available setup choices
hides arbitrary filesystem path inputs
pre-fills editable Porty author defaults
creates a local-only repository without remote fields
requires remote inspection before enabling remote submission
selects the advertised symbolic HEAD branch
shows a branch chooser for multiple branches
allows editable main for an empty remote
offers HTTPS, SSH, and no-auth choices
reports fixed SSH availability without upload controls
preserves non-secret fields and clears secret after failure
submits adoption with explicit manage-or-ignore origin choice
enters workspace only after ready status
```

In `App.test.tsx`, registration and login for registered-not-ready users must render setup without fetching stacks or opening the event stream.

- [ ] **Step 2: Run and confirm failure**

Run: `npm --prefix web test -- RepositorySetup.test.tsx App.test.tsx`

Expected: missing component/API and old direct-to-workspace flow.

- [ ] **Step 3: Add typed APIs**

Mirror Go contracts in `web/src/api.ts` and add:

```ts
export function getRepositorySetupStatus(): Promise<RepositorySetupStatus>
export function inspectRepositoryRemote(request: RemoteInspectionRequest): Promise<RemoteInspection>
export function setupRepository(request: RepositorySetupRequest): Promise<RepositorySetupStatus>
export function configureRepositoryRemote(request: RepositoryRemoteRequest): Promise<RepositorySetupStatus>
export function removeRepositoryRemote(): Promise<RepositorySetupStatus>
```

Use a TypeScript auth union so SSH cannot carry username/secret and no-auth cannot carry a secret.

- [ ] **Step 4: Implement chooser and staged remote flow**

- **Create local repository:** branch and identity only.
- **Use remote repository:** URL/auth first, explicit Inspect, then branch and identity.
- **Use existing local repository:** detected branch/identity and manage-or-ignore when an origin exists.

Disable unavailable cards with the safe server reason. Never render path or SSH upload inputs. After mutation failure retain mode, URL, username, branch, and identity but clear in-memory secret.

- [ ] **Step 5: Gate workspace initialization**

After authentication, fetch setup status before mounting `Workspace`. Render loading/setup/ready states. Only ready may fetch workspace resources or create the WebSocket. A successful setup response enters workspace without logout/reload.

- [ ] **Step 6: Format, typecheck, and test**

```bash
npm --prefix web exec prettier -- --write src/api.ts src/RepositorySetup.tsx src/RepositorySetup.test.tsx src/App.tsx src/App.test.tsx src/Auth.tsx src/styles.css
npm --prefix web run typecheck
npm --prefix web test -- RepositorySetup.test.tsx App.test.tsx
```

Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add web/src/api.ts web/src/RepositorySetup.tsx web/src/RepositorySetup.test.tsx web/src/App.tsx web/src/App.test.tsx web/src/Auth.tsx web/src/styles.css
git commit -m "feat: add first-start repository setup"
```

## Task 10: Add Post-Setup Remote Settings

**Files:**
- Create: `web/src/RepositorySettings.tsx`
- Create: `web/src/RepositorySettings.test.tsx`
- Modify: `web/src/AccountSettings.tsx`
- Modify: `web/src/App.tsx`
- Modify: `web/src/App.test.tsx`
- Modify: `web/src/styles.css`

- [ ] **Step 1: Write failing settings tests**

```text
local-only settings offer Add remote
managed settings offer Replace, Update authentication, and Remove
unmanaged origin requires explicit replacement confirmation
inspection and branch selection precede Add or Replace
updating authentication never pre-fills a stored secret
failed update clears the newly entered secret
removing remote keeps ready state and disables remote actions
Fetch, Pull, and Push are disabled without managed remote
```

- [ ] **Step 2: Run and confirm failure**

Run: `npm --prefix web test -- RepositorySettings.test.tsx App.test.tsx`

Expected: missing settings/remote-aware controls.

- [ ] **Step 3: Implement settings**

Reuse setup remote/auth controls. Require inspection before add/replace, show only redacted stored URL, and require confirmation for unmanaged-origin replacement. Explain that removal keeps stacks and local history. Pass `ManagedRemote != null` to repository actions; disable fetch/pull/push but retain status/diff/history/commit locally.

- [ ] **Step 4: Format, typecheck, and test**

```bash
npm --prefix web exec prettier -- --write src/RepositorySettings.tsx src/RepositorySettings.test.tsx src/AccountSettings.tsx src/App.tsx src/App.test.tsx src/styles.css
npm --prefix web run typecheck
npm --prefix web test -- RepositorySettings.test.tsx App.test.tsx
```

Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add web/src/RepositorySettings.tsx web/src/RepositorySettings.test.tsx web/src/AccountSettings.tsx web/src/App.tsx web/src/App.test.tsx web/src/styles.css
git commit -m "feat: add repository remote settings"
```

## Task 11: Update End-to-End Coverage and Operator Documentation

**Files:**
- Modify: `web/e2e/porty.spec.ts`
- Modify: `docs/operator-guide.md`

- [ ] **Step 1: Update the Playwright journey**

Start Porty with an empty data directory and extend the desktop/mobile journey:

1. Register the administrator.
2. Observe mandatory repository setup.
3. Choose **Create local repository**.
4. Keep default identity, choose branch `stacks`, and submit.
5. Enter the workspace and continue the existing stack journey.
6. Open account settings and verify **Add remote** exists while fetch/pull/push are unavailable.

Keep non-`main` remote default-branch coverage in the Go Git integration tests; do not weaken production URL validation by enabling local file transport for Playwright.

- [ ] **Step 2: Update operator documentation**

Document:

- mandatory lifecycle after registration/login;
- three modes and fixed `<data-dir>/repository` path;
- local-only operation and later remote management;
- HTTPS username/secret and write-only secret behavior;
- fixed `<data-dir>/ssh/id` and `<data-dir>/ssh/known_hosts` files;
- ownership and private-key mode `0600` or stricter;
- strict host verification and no agent/home-config dependency;
- branch discovery and empty-remote behavior;
- detached-HEAD rejection and explicit existing-origin choice;
- upgrade reconciliation for a safe existing repository.

- [ ] **Step 3: Build and run end-to-end tests**

```bash
go build -o /tmp/porty-e2e ./cmd/porty
PORTY_E2E_BINARY=/tmp/porty-e2e npm --prefix web run test:e2e
```

Expected: desktop and mobile journeys pass.

- [ ] **Step 4: Commit**

```bash
git add web/e2e/porty.spec.ts docs/operator-guide.md
git commit -m "docs: cover repository first-start setup"
```

## Task 12: Full Verification and Embedded Asset Refresh

**Files:**
- Modify: `web/dist/`

- [ ] **Step 1: Run formatting and race-sensitive tests**

```bash
gofmt -w internal/domain/repository_setup.go internal/application/repository_setup.go internal/application/repository_setup_test.go internal/application/repository.go internal/application/repository_test.go internal/infrastructure/sqlite/repository_store.go internal/infrastructure/sqlite/repository_store_test.go internal/infrastructure/gitcli/client.go internal/infrastructure/gitcli/client_test.go internal/infrastructure/gitcli/setup.go internal/infrastructure/gitcli/setup_test.go cmd/porty/git_helper.go cmd/porty/git_helper_test.go cmd/porty/main.go cmd/porty/main_test.go internal/httpapi/auth.go internal/httpapi/api.go internal/httpapi/api_test.go internal/httpapi/auth_test.go internal/httpapi/stream_test.go internal/httpapi/repository_setup_test.go
go test -race ./internal/application ./internal/infrastructure/gitcli ./internal/httpapi
```

Expected: PASS without race reports.

- [ ] **Step 2: Run complete backend verification**

```bash
go test ./...
go vet ./...
go build ./cmd/porty
```

Expected: PASS.

- [ ] **Step 3: Run complete frontend verification**

```bash
npm --prefix web test
npm --prefix web run typecheck
npm --prefix web run build
```

Expected: PASS; build regenerates hashed `web/dist/` assets.

- [ ] **Step 4: Run packaging verification**

Run: `./deploy/package_test.sh`

Expected: PASS, including embedded assets and systemd state-directory assumptions.

- [ ] **Step 5: Review generated and security-sensitive diffs**

```bash
git status --short
git diff --stat
git diff --check
rg -n 'TODO|FIXME|placeholder|GIT_SSH_COMMAND|exec.Command\("sh"|exec.Command\("bash"' internal cmd web/src docs
rg -n 'secret|password|private.key|known.hosts' internal/httpapi internal/application web/src
```

Expected:

- No unfinished markers or shell-based Git/SSH execution.
- Secret occurrences are limited to request/input, protected persistence, redaction, or test fixtures.
- No secret appears in response structs, audit payloads, snapshots, or errors.
- Only intended source files and regenerated assets changed.

- [ ] **Step 6: Perform requirement and review-focus self-review**

Map every approved requirement to a test and implementation location. Re-run the five Review Focus scenarios as focused tests. Confirm interface signatures match across application fakes, SQLite, Git, HTTP, and startup wiring.

- [ ] **Step 7: Commit generated assets**

```bash
git add web/dist
git commit -m "chore: rebuild embedded frontend"
```

- [ ] **Step 8: Request code review**

Use `superpowers:requesting-code-review` with emphasis on fixed-root/symlink safety, auth redaction, retry atomicity, branch discovery, unrelated-history protection, and readiness-gate coverage.
