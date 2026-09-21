package application

import (
	"context"
	"errors"
	"net/mail"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/msoldin/porty/internal/domain"
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
	Authentication  domain.RepositoryAuthentication
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

type RepositorySetupService struct {
	store       RepositorySetupStore
	provisioner RepositoryProvisioner
	repository  *RepositoryService
	coordinator *Coordinator
	options     RepositorySetupOptions
}

func NewRepositorySetupService(
	store RepositorySetupStore,
	provisioner RepositoryProvisioner,
	repository *RepositoryService,
	coordinator *Coordinator,
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

func (s *RepositorySetupService) Status(ctx context.Context) (domain.RepositorySetupStatus, error) {
	configuration, _, err := s.store.Load(ctx)
	if err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	return s.status(ctx, configuration)
}

func (s *RepositorySetupService) InspectRemote(ctx context.Context, request domain.RemoteInspectionRequest) (domain.RemoteInspection, error) {
	remoteURL := strings.TrimSpace(request.Remote.URL)
	if remoteURL == "" {
		return domain.RemoteInspection{}, ErrInvalidRequest
	}
	authentication, err := s.authentication(request.Remote.Authentication)
	if err != nil {
		return domain.RemoteInspection{}, err
	}

	inspectCtx, cancel := context.WithTimeout(ctx, remoteInspectTimeout)
	defer cancel()
	inspection, err := s.provisioner.InspectRemote(inspectCtx, remoteURL, authentication)
	if err != nil {
		return domain.RemoteInspection{}, safeProvisionError(err)
	}
	if inspection.Empty && inspection.Suggested == "" {
		inspection.Suggested = "main"
	}
	return inspection, nil
}

func (s *RepositorySetupService) Setup(ctx context.Context, request domain.RepositorySetupRequest) (domain.RepositorySetupStatus, error) {
	provisionRequest, authentication, err := s.validateSetupRequest(request)
	if err != nil {
		return domain.RepositorySetupStatus{}, err
	}
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return domain.RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	git, configuration, err := s.provisioner.Provision(setupCtx, provisionRequest)
	if err != nil {
		return domain.RepositorySetupStatus{}, safeProvisionError(err)
	}
	configuration.State = domain.RepositorySetupReady
	if err := s.store.Save(setupCtx, configuration, authentication); err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
	return s.status(setupCtx, configuration)
}

func (s *RepositorySetupService) ConfigureRemote(ctx context.Context, request domain.RepositoryRemoteRequest) (domain.RepositorySetupStatus, error) {
	remoteURL := strings.TrimSpace(request.Remote.URL)
	branch, err := validateBranch(request.Branch)
	if err != nil || remoteURL == "" {
		return domain.RepositorySetupStatus{}, ErrInvalidRequest
	}
	authentication, err := s.authentication(request.Remote.Authentication)
	if err != nil {
		return domain.RepositorySetupStatus{}, err
	}
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return domain.RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	configuration, _, err := s.store.Load(setupCtx)
	if err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	if configuration.State != domain.RepositorySetupReady {
		return domain.RepositorySetupStatus{}, ErrRepositorySetupRequired
	}
	git, updated, err := s.provisioner.ConfigureRemote(setupCtx, RepositoryRemoteProvisionRequest{
		RemoteURL:       remoteURL,
		Branch:          branch,
		ReplaceExisting: request.ReplaceExisting,
		Authentication:  authentication,
	}, configuration)
	if err != nil {
		return domain.RepositorySetupStatus{}, safeProvisionError(err)
	}
	updated.State = domain.RepositorySetupReady
	if err := s.store.Save(setupCtx, updated, authentication); err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, updated.Remote != nil && updated.Remote.Managed)
	return s.status(setupCtx, updated)
}

func (s *RepositorySetupService) RemoveRemote(ctx context.Context) (domain.RepositorySetupStatus, error) {
	release, err := s.coordinator.Try(true, "")
	if err != nil {
		return domain.RepositorySetupStatus{}, err
	}
	defer release()

	setupCtx, cancel := context.WithTimeout(ctx, repositorySetupTimeout)
	defer cancel()
	configuration, _, err := s.store.Load(setupCtx)
	if err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	if configuration.State != domain.RepositorySetupReady {
		return domain.RepositorySetupStatus{}, ErrRepositorySetupRequired
	}
	if configuration.Remote == nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryRemoteUnavailable
	}
	git, updated, err := s.provisioner.RemoveRemote(setupCtx, configuration)
	if err != nil {
		return domain.RepositorySetupStatus{}, safeProvisionError(err)
	}
	updated.State = domain.RepositorySetupReady
	authentication := domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}
	if err := s.store.Save(setupCtx, updated, authentication); err != nil {
		return domain.RepositorySetupStatus{}, ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, false)
	return s.status(setupCtx, updated)
}

func (s *RepositorySetupService) Ready(ctx context.Context) (bool, error) {
	configuration, _, err := s.store.Load(ctx)
	if err != nil {
		return false, ErrRepositoryPersistenceFailed
	}
	return configuration.State == domain.RepositorySetupReady, nil
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
	case domain.RepositorySetupUnregistered:
		return nil
	case domain.RepositorySetupRegistered:
		return s.reconcileRegistered(ctx)
	case domain.RepositorySetupReady:
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
	if inspection.State != domain.RepositoryPathWorktree {
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
		Mode:                 domain.RepositorySetupAdopt,
		Branch:               branch,
		Author:               author,
		ManageExistingRemote: false,
		Authentication:       domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone},
	})
	if err != nil {
		return safeProvisionError(err)
	}
	configuration.State = domain.RepositorySetupReady
	if err := s.store.Save(setupCtx, configuration, domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}); err != nil {
		return ErrRepositoryPersistenceFailed
	}
	s.repository.Replace(git, configuration.Remote != nil && configuration.Remote.Managed)
	return nil
}

func (s *RepositorySetupService) status(ctx context.Context, configuration domain.RepositoryConfiguration) (domain.RepositorySetupStatus, error) {
	inspection, err := s.provisioner.InspectPath(ctx)
	if err != nil {
		return domain.RepositorySetupStatus{}, safeProvisionError(err)
	}
	status := domain.RepositorySetupStatus{
		State:          configuration.State,
		Required:       configuration.State != domain.RepositorySetupReady,
		PathState:      inspection.State,
		Branch:         inspection.Branch,
		Author:         inspection.Author,
		DefaultAuthor:  defaultGitIdentity(),
		ExistingRemote: inspection.ExistingRemote,
		ManagedRemote:  configuration.Remote,
		SSH: domain.SSHMaterialStatus{
			IdentityAvailable:   s.options.SSHKeyPath != "",
			KnownHostsAvailable: s.options.KnownHostsPath != "",
			Usable:              s.options.SSHKeyPath != "" && s.options.KnownHostsPath != "",
		},
	}
	if configuration.State == domain.RepositorySetupReady {
		status.Branch = configuration.Branch
		status.Author = configuration.Author
	}
	if status.Author.Name == "" && status.Author.Email == "" {
		status.Author = status.DefaultAuthor
	}
	status.Modes = modeAvailability(inspection)
	return status, nil
}

func modeAvailability(inspection domain.RepositoryPathInspection) []domain.RepositoryModeAvailability {
	initAvailable := inspection.State == domain.RepositoryPathEmpty
	adoptAvailable := inspection.State == domain.RepositoryPathWorktree && !inspection.Detached
	return []domain.RepositoryModeAvailability{
		{Mode: domain.RepositorySetupInit, Available: initAvailable, Reason: unavailableReason(initAvailable, "repository path is not empty")},
		{Mode: domain.RepositorySetupRemote, Available: initAvailable, Reason: unavailableReason(initAvailable, "repository path is not empty")},
		{Mode: domain.RepositorySetupAdopt, Available: adoptAvailable, Reason: unavailableReason(adoptAvailable, "repository path is not an adoptable worktree")},
	}
}

func unavailableReason(available bool, reason string) string {
	if available {
		return ""
	}
	return reason
}

func (s *RepositorySetupService) validateSetupRequest(request domain.RepositorySetupRequest) (RepositoryProvisionRequest, domain.RepositoryAuthentication, error) {
	branch, err := validateBranch(request.Branch)
	if err != nil {
		return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, err
	}
	author, err := validateIdentity(request.Author)
	if err != nil {
		return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, err
	}
	provisionRequest := RepositoryProvisionRequest{
		Mode:                 request.Mode,
		Branch:               branch,
		Author:               author,
		ManageExistingRemote: request.ManageExistingRemote,
		Authentication:       domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone},
	}

	switch request.Mode {
	case domain.RepositorySetupInit:
		if request.Remote != nil || request.ManageExistingRemote {
			return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
	case domain.RepositorySetupRemote:
		if request.Remote == nil || request.ManageExistingRemote {
			return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
		provisionRequest.RemoteURL = strings.TrimSpace(request.Remote.URL)
		if provisionRequest.RemoteURL == "" {
			return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
		provisionRequest.Authentication, err = s.authentication(request.Remote.Authentication)
		if err != nil {
			return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, err
		}
	case domain.RepositorySetupAdopt:
		if request.Remote != nil {
			return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
	default:
		return RepositoryProvisionRequest{}, domain.RepositoryAuthentication{}, ErrInvalidRequest
	}
	return provisionRequest, provisionRequest.Authentication, nil
}

func (s *RepositorySetupService) authentication(input domain.RemoteAuthenticationInput) (domain.RepositoryAuthentication, error) {
	switch input.Type {
	case domain.RepositoryAuthNone:
		if input.Username != "" || input.Secret != "" {
			return domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
		return domain.RepositoryAuthentication{Type: domain.RepositoryAuthNone}, nil
	case domain.RepositoryAuthHTTPS:
		username := strings.TrimSpace(input.Username)
		if username == "" || input.Secret == "" || containsControl(username) {
			return domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
		return domain.RepositoryAuthentication{Type: domain.RepositoryAuthHTTPS, Username: username, Secret: input.Secret}, nil
	case domain.RepositoryAuthSSH:
		if input.Username != "" || input.Secret != "" {
			return domain.RepositoryAuthentication{}, ErrInvalidRequest
		}
		if s.options.SSHKeyPath == "" || s.options.KnownHostsPath == "" {
			return domain.RepositoryAuthentication{}, ErrSSHMaterialUnavailable
		}
		return domain.RepositoryAuthentication{
			Type:           domain.RepositoryAuthSSH,
			SSHKeyPath:     s.options.SSHKeyPath,
			KnownHostsPath: s.options.KnownHostsPath,
		}, nil
	default:
		return domain.RepositoryAuthentication{}, ErrInvalidRequest
	}
}

func validateIdentity(identity domain.GitIdentity) (domain.GitIdentity, error) {
	identity.Name = strings.TrimSpace(identity.Name)
	identity.Email = strings.TrimSpace(identity.Email)
	nameLength := utf8.RuneCountInString(identity.Name)
	emailLength := utf8.RuneCountInString(identity.Email)
	if nameLength < 1 || nameLength > 128 || emailLength < 3 || emailLength > 254 || containsControl(identity.Name) || containsControl(identity.Email) {
		return domain.GitIdentity{}, ErrInvalidRequest
	}
	address, err := mail.ParseAddress(identity.Email)
	if err != nil || address.Address != identity.Email {
		return domain.GitIdentity{}, ErrInvalidRequest
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

func defaultGitIdentity() domain.GitIdentity {
	return domain.GitIdentity{Name: defaultGitAuthorName, Email: defaultGitAuthorEmail}
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
