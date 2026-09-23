package repository

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	defaultGitAuthorName   = "Porty"
	defaultGitAuthorEmail  = "porty@localhost"
	remoteInspectTimeout   = 30 * time.Second
	repositorySetupTimeout = 2 * time.Minute
)

var (
	ErrInvalidRequest              = errors.New("invalid request")
	ErrRepositorySetupRequired     = errors.New("repository setup required")
	ErrRepositoryPathNotEmpty      = errors.New("repository path is not empty")
	ErrInvalidWorktree             = errors.New("invalid repository worktree")
	ErrDetachedHead                = errors.New("repository has a detached HEAD")
	ErrRemoteAuthenticationFailed  = errors.New("remote authentication failed")
	ErrRemoteUnavailable           = errors.New("remote unavailable")
	ErrSSHMaterialUnavailable      = errors.New("SSH material unavailable")
	ErrUnrelatedHistory            = errors.New("repository histories are unrelated")
	ErrRepositoryRemoteConflict    = errors.New("repository remote conflict")
	ErrRepositoryRemoteUnavailable = errors.New("repository remote unavailable")
	ErrRepositorySetupFailed       = errors.New("repository setup failed")
	ErrRepositoryPersistenceFailed = errors.New("repository setup persistence failed")
)

type RepositorySetupStore interface {
	Load(context.Context) (RepositoryConfiguration, RepositoryAuthentication, error)
	Save(context.Context, RepositoryConfiguration, RepositoryAuthentication) error
}

type RepositoryProvisionRequest struct {
	Mode                 RepositorySetupMode
	Branch               string
	Author               GitIdentity
	RemoteURL            string
	ManageExistingRemote bool
	Authentication       RepositoryAuthentication
}

type RepositoryRemoteProvisionRequest struct {
	RemoteURL       string
	Branch          string
	ReplaceExisting bool
	Authentication  RepositoryAuthentication
}

type RepositoryProvisioner interface {
	InspectPath(context.Context) (RepositoryPathInspection, error)
	InspectSSHMaterial() SSHMaterialStatus
	InspectRemote(context.Context, string, RepositoryAuthentication) (RemoteInspection, error)
	Provision(context.Context, RepositoryProvisionRequest) (GitRepository, RepositoryConfiguration, error)
	ConfigureRemote(context.Context, RepositoryRemoteProvisionRequest, RepositoryConfiguration) (GitRepository, RepositoryConfiguration, error)
	RemoveRemote(context.Context, RepositoryConfiguration) (GitRepository, RepositoryConfiguration, error)
	Open(context.Context, RepositoryConfiguration, RepositoryAuthentication) (GitRepository, error)
}

type RepositorySetupOptions struct {
	SSHKeyPath     string
	KnownHostsPath string
}

type operationLocker interface {
	Try(bool, string) (func(), error)
}

type RepositorySetupService struct {
	store       RepositorySetupStore
	provisioner RepositoryProvisioner
	repository  *RepositoryService
	coordinator operationLocker
	options     RepositorySetupOptions
}

func NewRepositorySetupService(
	store RepositorySetupStore,
	provisioner RepositoryProvisioner,
	repository *RepositoryService,
	coordinator operationLocker,
	options RepositorySetupOptions,
) *RepositorySetupService {
	return &RepositorySetupService{
		store:       store,
		provisioner: provisioner,
		repository:  repository,
		coordinator: coordinator,
		options:     options,
	}
}

func (s *RepositorySetupService) Status(ctx context.Context) (RepositorySetupStatus, error) {
	configuration, _, err := s.store.Load(ctx)
	if err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	return s.status(ctx, configuration)
}

func (s *RepositorySetupService) InspectRemote(ctx context.Context, request RemoteInspectionRequest) (RemoteInspection, error) {
	remoteURL := strings.TrimSpace(request.Remote.URL)
	if remoteURL == "" {
		return RemoteInspection{}, ErrInvalidRequest
	}
	authentication, err := s.authentication(request.Remote.Authentication)
	if err != nil {
		return RemoteInspection{}, err
	}

	inspectCtx, cancel := context.WithTimeout(ctx, remoteInspectTimeout)
	defer cancel()
	inspection, err := s.provisioner.InspectRemote(inspectCtx, remoteURL, authentication)
	if err != nil {
		return RemoteInspection{}, safeProvisionError(err)
	}
	if inspection.Empty && inspection.Suggested == "" {
		inspection.Suggested = "main"
	}
	return inspection, nil
}

func (s *RepositorySetupService) Setup(ctx context.Context, request RepositorySetupRequest) (RepositorySetupStatus, error) {
	provisionRequest, authentication, err := s.validateSetupRequest(request)
	if err != nil {
		return RepositorySetupStatus{}, err
	}
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	git, configuration, err := s.provisioner.Provision(setupCtx, provisionRequest)
	if err != nil {
		return RepositorySetupStatus{}, safeProvisionError(err)
	}
	configuration.State = RepositorySetupReady
	if err := s.store.Save(setupCtx, configuration, authentication); err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
	return s.status(setupCtx, configuration)
}

func (s *RepositorySetupService) ConfigureRemote(ctx context.Context, request RepositoryRemoteRequest) (RepositorySetupStatus, error) {
	remoteURL := strings.TrimSpace(request.Remote.URL)
	branch, err := validateBranch(request.Branch)
	if err != nil || remoteURL == "" {
		return RepositorySetupStatus{}, ErrInvalidRequest
	}
	authentication, err := s.authentication(request.Remote.Authentication)
	if err != nil {
		return RepositorySetupStatus{}, err
	}
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	configuration, _, err := s.store.Load(setupCtx)
	if err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	if configuration.State != RepositorySetupReady {
		return RepositorySetupStatus{}, ErrRepositorySetupRequired
	}
	git, updated, err := s.provisioner.ConfigureRemote(setupCtx, RepositoryRemoteProvisionRequest{
		RemoteURL:       remoteURL,
		Branch:          branch,
		ReplaceExisting: request.ReplaceExisting,
		Authentication:  authentication,
	}, configuration)
	if err != nil {
		return RepositorySetupStatus{}, safeProvisionError(err)
	}
	updated.State = RepositorySetupReady
	if err := s.store.Save(setupCtx, updated, authentication); err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, updated.Remote != nil && updated.Remote.Managed)
	return s.status(setupCtx, updated)
}

func (s *RepositorySetupService) RemoveRemote(ctx context.Context) (RepositorySetupStatus, error) {
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	configuration, _, err := s.store.Load(setupCtx)
	if err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	if configuration.State != RepositorySetupReady {
		return RepositorySetupStatus{}, ErrRepositorySetupRequired
	}
	if configuration.Remote == nil {
		return RepositorySetupStatus{}, ErrRepositoryRemoteUnavailable
	}
	git, updated, err := s.provisioner.RemoveRemote(setupCtx, configuration)
	if err != nil {
		return RepositorySetupStatus{}, safeProvisionError(err)
	}
	updated.State = RepositorySetupReady
	authentication := RepositoryAuthentication{Type: RepositoryAuthNone}
	if err := s.store.Save(setupCtx, updated, authentication); err != nil {
		return RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, false)
	return s.status(setupCtx, updated)
}

func (s *RepositorySetupService) Ready(ctx context.Context) (bool, error) {
	configuration, _, err := s.store.Load(ctx)
	if err != nil {
		return false, ErrRepositoryPersistenceFailed
	}
	return configuration.State == RepositorySetupReady, nil
}

func (s *RepositorySetupService) Reconcile(ctx context.Context) error {
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return err
	}
	defer release()

	configuration, authentication, err := s.store.Load(ctx)
	if err != nil {
		return ErrRepositoryPersistenceFailed
	}
	switch configuration.State {
	case RepositorySetupUnregistered:
		return nil
	case RepositorySetupRegistered:
		return s.reconcileRegistered(ctx)
	case RepositorySetupReady:
		git, err := s.provisioner.Open(ctx, configuration, authentication)
		if err != nil {
			return safeProvisionError(err)
		}
		s.repository.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
		return nil
	default:
		return ErrInvalidWorktree
	}
}

func (s *RepositorySetupService) reconcileRegistered(ctx context.Context) error {
	inspection, err := s.provisioner.InspectPath(ctx)
	if err != nil {
		return safeProvisionError(err)
	}
	if inspection.State != RepositoryPathWorktree {
		return nil
	}
	if inspection.Detached {
		return nil
	}
	branch, err := validateBranch(inspection.Branch)
	if err != nil {
		return nil
	}
	author := inspection.Author
	if strings.TrimSpace(author.Name) == "" && strings.TrimSpace(author.Email) == "" {
		author = defaultGitIdentity()
	} else {
		author, err = validateIdentity(author)
		if err != nil {
			return nil
		}
	}

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	git, configuration, err := s.provisioner.Provision(setupCtx, RepositoryProvisionRequest{
		Mode:                 RepositorySetupAdopt,
		Branch:               branch,
		Author:               author,
		ManageExistingRemote: false,
		Authentication:       RepositoryAuthentication{Type: RepositoryAuthNone},
	})
	if err != nil {
		return safeProvisionError(err)
	}
	configuration.State = RepositorySetupReady
	if err := s.store.Save(setupCtx, configuration, RepositoryAuthentication{Type: RepositoryAuthNone}); err != nil {
		return ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
	return nil
}

func (s *RepositorySetupService) status(ctx context.Context, configuration RepositoryConfiguration) (RepositorySetupStatus, error) {
	inspection, err := s.provisioner.InspectPath(ctx)
	if err != nil {
		return RepositorySetupStatus{}, safeProvisionError(err)
	}
	status := RepositorySetupStatus{
		State:          configuration.State,
		Required:       configuration.State != RepositorySetupReady,
		PathState:      inspection.State,
		Branch:         inspection.Branch,
		Author:         inspection.Author,
		DefaultAuthor:  defaultGitIdentity(),
		ExistingRemote: inspection.ExistingRemote,
		ManagedRemote:  configuration.Remote,
		SSH:            s.provisioner.InspectSSHMaterial(),
	}
	if configuration.State == RepositorySetupReady {
		status.Branch = configuration.Branch
		status.Author = configuration.Author
	}
	if status.Author.Name == "" && status.Author.Email == "" {
		status.Author = status.DefaultAuthor
	}
	status.Modes = modeAvailability(inspection)
	return status, nil
}

func modeAvailability(inspection RepositoryPathInspection) []RepositoryModeAvailability {
	initAvailable := inspection.State == RepositoryPathEmpty
	adoptAvailable := inspection.State == RepositoryPathWorktree && !inspection.Detached
	return []RepositoryModeAvailability{
		{Mode: RepositorySetupInit, Available: initAvailable, Reason: unavailableReason(initAvailable, "repository path is not empty")},
		{Mode: RepositorySetupRemote, Available: initAvailable, Reason: unavailableReason(initAvailable, "repository path is not empty")},
		{Mode: RepositorySetupAdopt, Available: adoptAvailable, Reason: unavailableReason(adoptAvailable, "repository path is not an adoptable worktree")},
	}
}

func unavailableReason(available bool, reason string) string {
	if available {
		return ""
	}
	return reason
}

func (s *RepositorySetupService) validateSetupRequest(request RepositorySetupRequest) (RepositoryProvisionRequest, RepositoryAuthentication, error) {
	branch, err := validateBranch(request.Branch)
	if err != nil {
		return RepositoryProvisionRequest{}, RepositoryAuthentication{}, err
	}
	author, err := validateIdentity(request.Author)
	if err != nil {
		return RepositoryProvisionRequest{}, RepositoryAuthentication{}, err
	}
	provisionRequest := RepositoryProvisionRequest{
		Mode:                 request.Mode,
		Branch:               branch,
		Author:               author,
		ManageExistingRemote: request.ManageExistingRemote,
		Authentication:       RepositoryAuthentication{Type: RepositoryAuthNone},
	}

	switch request.Mode {
	case RepositorySetupInit:
		if request.Remote != nil || request.ManageExistingRemote {
			return RepositoryProvisionRequest{}, RepositoryAuthentication{}, ErrInvalidRequest
		}
	case RepositorySetupRemote:
		if request.Remote == nil || request.ManageExistingRemote {
			return RepositoryProvisionRequest{}, RepositoryAuthentication{}, ErrInvalidRequest
		}
		provisionRequest.RemoteURL = strings.TrimSpace(request.Remote.URL)
		if provisionRequest.RemoteURL == "" {
			return RepositoryProvisionRequest{}, RepositoryAuthentication{}, ErrInvalidRequest
		}
		provisionRequest.Authentication, err = s.authentication(request.Remote.Authentication)
		if err != nil {
			return RepositoryProvisionRequest{}, RepositoryAuthentication{}, err
		}
	case RepositorySetupAdopt:
		if request.Remote != nil {
			return RepositoryProvisionRequest{}, RepositoryAuthentication{}, ErrInvalidRequest
		}
	default:
		return RepositoryProvisionRequest{}, RepositoryAuthentication{}, ErrInvalidRequest
	}
	return provisionRequest, provisionRequest.Authentication, nil
}

func (s *RepositorySetupService) authentication(input RemoteAuthenticationInput) (RepositoryAuthentication, error) {
	switch input.Type {
	case RepositoryAuthNone:
		if input.Username != "" || input.Secret != "" {
			return RepositoryAuthentication{}, ErrInvalidRequest
		}
		return RepositoryAuthentication{Type: RepositoryAuthNone}, nil
	case RepositoryAuthHTTPS:
		username := strings.TrimSpace(input.Username)
		if username == "" || input.Secret == "" || containsControl(username) {
			return RepositoryAuthentication{}, ErrInvalidRequest
		}
		return RepositoryAuthentication{Type: RepositoryAuthHTTPS, Username: username, Secret: input.Secret}, nil
	case RepositoryAuthSSH:
		if input.Username != "" || input.Secret != "" {
			return RepositoryAuthentication{}, ErrInvalidRequest
		}
		if s.options.SSHKeyPath == "" || s.options.KnownHostsPath == "" {
			return RepositoryAuthentication{}, ErrSSHMaterialUnavailable
		}
		return RepositoryAuthentication{
			Type:           RepositoryAuthSSH,
			SSHKeyPath:     s.options.SSHKeyPath,
			KnownHostsPath: s.options.KnownHostsPath,
		}, nil
	default:
		return RepositoryAuthentication{}, ErrInvalidRequest
	}
}

func validateIdentity(identity GitIdentity) (GitIdentity, error) {
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Email = strings.TrimSpace(identity.Email)
	nameLength := utf8.RuneCountInString(identity.Name)
	emailLength := utf8.RuneCountInString(identity.Email)
	if nameLength < 1 || nameLength > 128 || emailLength < 3 || emailLength > 254 || containsControl(identity.Name) || containsControl(identity.Email) {
		return GitIdentity{}, ErrInvalidRequest
	}
	address, err := mail.ParseAddress(identity.Email)
	if err != nil || address.Address != identity.Email {
		return GitIdentity{}, ErrInvalidRequest
	}
	return identity, nil
}

func validateBranch(branch string) (string, error) {
	if branch == "" || branch != strings.TrimSpace(branch) || len(branch) > 255 || !utf8.ValidString(branch) || containsControl(branch) {
		return "", ErrInvalidRequest
	}
	if strings.HasPrefix(branch, "-") || strings.HasPrefix(branch, ".") || strings.HasSuffix(branch, "/") || strings.HasSuffix(branch, ".") || strings.HasSuffix(branch, ".lock") || strings.Contains(branch, "..") || strings.Contains(branch, "@{") || strings.Contains(branch, "//") || strings.ContainsAny(branch, " ~^:?*[\\") {
		return "", ErrInvalidRequest
	}
	for _, component := range strings.Split(branch, "/") {
		if component == "" || strings.HasPrefix(component, ".") || strings.HasSuffix(component, ".") || strings.HasSuffix(component, ".lock") {
			return "", ErrInvalidRequest
		}
	}
	return branch, nil
}

func containsControl(value string) bool {
	for _, character := range value {
		if unicode.IsControl(character) {
			return true
		}
	}
	return false
}

func defaultGitIdentity() GitIdentity {
	return GitIdentity{Name: defaultGitAuthorName, Email: defaultGitAuthorEmail}
}

func safeProvisionError(err error) error {
	known := []error{
		ErrInvalidRequest,
		ErrRepositorySetupRequired,
		ErrRepositoryPathNotEmpty,
		ErrInvalidWorktree,
		ErrDetachedHead,
		ErrRemoteAuthenticationFailed,
		ErrRemoteUnavailable,
		ErrSSHMaterialUnavailable,
		ErrUnrelatedHistory,
		ErrRepositoryRemoteConflict,
		ErrRepositoryRemoteUnavailable,
	}
	for _, candidate := range known {
		if errors.Is(err, candidate) {
			return candidate
		}
	}
	return ErrRepositorySetupFailed
}
