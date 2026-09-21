package application_test

import (
	"context"
	"encoding/json"
	"errors"
	"sync"
	"testing"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

func TestRepositorySetupStatusOffersModesForEmptyPath(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{path: domain.RepositoryPathInspection{State: domain.RepositoryPathEmpty}}
	service, _ := newRepositorySetupService(store, provisioner)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertModeAvailability(t, status.Modes, domain.RepositorySetupInit, true)
	assertModeAvailability(t, status.Modes, domain.RepositorySetupRemote, true)
	assertModeAvailability(t, status.Modes, domain.RepositorySetupAdopt, false)
}

func TestRepositorySetupStatusOffersOnlyAdoptForSafeWorktree(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{path: domain.RepositoryPathInspection{
		State:  domain.RepositoryPathWorktree,
		Branch: "release/v1",
		Author: domain.GitIdentity{Name: "Ada", Email: "ada@example.com"},
	}}
	service, _ := newRepositorySetupService(store, provisioner)

	status, err := service.Status(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	assertModeAvailability(t, status.Modes, domain.RepositorySetupInit, false)
	assertModeAvailability(t, status.Modes, domain.RepositorySetupRemote, false)
	assertModeAvailability(t, status.Modes, domain.RepositorySetupAdopt, true)
	if status.Branch != "release/v1" || status.Author.Name != "Ada" {
		t.Fatalf("detected status = %#v", status)
	}
}

func TestRepositorySetupRejectsInvalidIdentityBeforeProvisioning(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{}
	service, _ := newRepositorySetupService(store, provisioner)

	_, err := service.Setup(context.Background(), domain.RepositorySetupRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: "Ada\nInjected", Email: "ada@example.com"},
	})
	if !errors.Is(err, application.ErrInvalidRequest) {
		t.Fatalf("Setup() error = %v, want ErrInvalidRequest", err)
	}
	if provisioner.provisionCalls != 0 {
		t.Fatalf("Provision() calls = %d, want 0", provisioner.provisionCalls)
	}
}

func TestRepositorySetupInitializesLocalRepositoryWithoutRemote(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{provisionConfiguration: readyConfiguration("main", nil)}
	service, activated := newRepositorySetupService(store, provisioner)

	status, err := service.Setup(context.Background(), domain.RepositorySetupRequest{
		Mode:   domain.RepositorySetupInit,
		Branch: "main",
		Author: domain.GitIdentity{Name: " Porty ", Email: " porty@localhost "},
	})
	if err != nil {
		t.Fatal(err)
	}
	request := provisioner.lastProvision
	if request.Mode != domain.RepositorySetupInit || request.RemoteURL != "" || request.Authentication.Type != domain.RepositoryAuthNone {
		t.Fatalf("Provision() request = %#v", request)
	}
	if request.Author != (domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}) {
		t.Fatalf("Provision() author = %#v", request.Author)
	}
	if store.savedConfiguration.State != domain.RepositorySetupReady || store.savedConfiguration.Remote != nil {
		t.Fatalf("saved configuration = %#v", store.savedConfiguration)
	}
	if got, _ := activated.Head(context.Background()); got != "configured" {
		t.Fatalf("active repository head = %q, want configured", got)
	}
	if status.ManagedRemote != nil || status.State != domain.RepositorySetupReady {
		t.Fatalf("status = %#v", status)
	}
}

func TestRepositorySetupImportsInspectedRemoteBranch(t *testing.T) {
	remote := &domain.RepositoryRemoteSummary{Name: "origin", URL: "https://example.com/team/repo.git", AuthType: domain.RepositoryAuthHTTPS, Managed: true}
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{
		inspection:             domain.RemoteInspection{RemoteURL: remote.URL, DefaultBranch: "trunk", Branches: []string{"trunk"}, Suggested: "trunk"},
		provisionConfiguration: readyConfiguration("trunk", remote),
	}
	service, _ := newRepositorySetupService(store, provisioner)
	input := domain.RepositoryRemoteInput{URL: remote.URL, Authentication: domain.RemoteAuthenticationInput{Type: domain.RepositoryAuthHTTPS, Username: "git", Secret: "token-value"}}

	inspection, err := service.InspectRemote(context.Background(), domain.RemoteInspectionRequest{Remote: input})
	if err != nil {
		t.Fatal(err)
	}
	if inspection.Suggested != "trunk" {
		t.Fatalf("InspectRemote() suggested = %q", inspection.Suggested)
	}
	status, err := service.Setup(context.Background(), domain.RepositorySetupRequest{
		Mode: domain.RepositorySetupRemote, Branch: inspection.Suggested,
		Author: domain.GitIdentity{Name: "Ada", Email: "ada@example.com"}, Remote: &input,
	})
	if err != nil {
		t.Fatal(err)
	}
	if provisioner.lastProvision.Branch != "trunk" || provisioner.lastProvision.Authentication.Secret != "token-value" {
		t.Fatalf("Provision() request = %#v", provisioner.lastProvision)
	}
	if status.ManagedRemote == nil || status.ManagedRemote.URL != remote.URL {
		t.Fatalf("status = %#v", status)
	}
}

func TestRepositorySetupDoesNotReplaceLiveClientWhenPersistenceFails(t *testing.T) {
	saveErr := errors.New("database unavailable")
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}, saveErr: saveErr}
	provisioner := &fakeRepositoryProvisioner{provisionConfiguration: readyConfiguration("main", nil)}
	service, activated := newRepositorySetupService(store, provisioner)

	_, err := service.Setup(context.Background(), validLocalSetupRequest())
	if err == nil {
		t.Fatal("Setup() error = nil, want persistence failure")
	}
	if got, _ := activated.Head(context.Background()); got != "original" {
		t.Fatalf("active repository head = %q, want original", got)
	}
}

func TestRepositorySetupRetryRecoversAfterProvisioningSucceeded(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}, failSaves: 1}
	provisioner := &fakeRepositoryProvisioner{provisionConfiguration: readyConfiguration("main", nil), requireMatchingRetry: true}
	service, _ := newRepositorySetupService(store, provisioner)

	if _, err := service.Setup(context.Background(), validLocalSetupRequest()); err == nil {
		t.Fatal("first Setup() error = nil, want persistence failure")
	}
	if _, err := service.Setup(context.Background(), validLocalSetupRequest()); err != nil {
		t.Fatalf("retry Setup() error = %v", err)
	}
	if provisioner.provisionCalls != 2 {
		t.Fatalf("Provision() calls = %d, want 2", provisioner.provisionCalls)
	}
}

func TestRepositorySetupSerializesConcurrentMutations(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	entered := make(chan struct{})
	release := make(chan struct{})
	provisioner := &fakeRepositoryProvisioner{provisionConfiguration: readyConfiguration("main", nil), entered: entered, release: release}
	service, _ := newRepositorySetupService(store, provisioner)

	firstResult := make(chan error, 1)
	go func() {
		_, err := service.Setup(context.Background(), validLocalSetupRequest())
		firstResult <- err
	}()
	<-entered
	_, secondErr := service.Setup(context.Background(), validLocalSetupRequest())
	close(release)
	if err := <-firstResult; err != nil {
		t.Fatalf("first Setup() error = %v", err)
	}
	if !errors.Is(secondErr, application.ErrOperationConflict) {
		t.Fatalf("concurrent Setup() error = %v, want ErrOperationConflict", secondErr)
	}
	if provisioner.maxConcurrent != 1 {
		t.Fatalf("maximum concurrent Provision() calls = %d, want 1", provisioner.maxConcurrent)
	}
}

func TestRepositorySetupReconcilesRegisteredExistingRepository(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{
		path:                   domain.RepositoryPathInspection{State: domain.RepositoryPathWorktree, Branch: "main"},
		provisionConfiguration: readyConfiguration("main", nil),
	}
	service, activated := newRepositorySetupService(store, provisioner)

	if err := service.Reconcile(context.Background()); err != nil {
		t.Fatal(err)
	}
	if provisioner.lastProvision.Mode != domain.RepositorySetupAdopt {
		t.Fatalf("Provision() mode = %q, want adopt", provisioner.lastProvision.Mode)
	}
	if provisioner.lastProvision.Author != (domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}) {
		t.Fatalf("Provision() author = %#v, want defaults", provisioner.lastProvision.Author)
	}
	if store.savedConfiguration.State != domain.RepositorySetupReady {
		t.Fatalf("saved state = %q, want ready", store.savedConfiguration.State)
	}
	if got, _ := activated.Head(context.Background()); got != "configured" {
		t.Fatalf("active repository head = %q, want configured", got)
	}
}

func TestRepositorySetupReadyRejectsTamperedRepository(t *testing.T) {
	store := &fakeRepositorySetupStore{configuration: readyConfiguration("main", nil)}
	provisioner := &fakeRepositoryProvisioner{openErr: application.ErrInvalidWorktree}
	service, activated := newRepositorySetupService(store, provisioner)

	if err := service.Reconcile(context.Background()); !errors.Is(err, application.ErrInvalidWorktree) {
		t.Fatalf("Reconcile() error = %v, want ErrInvalidWorktree", err)
	}
	if got, _ := activated.Head(context.Background()); got != "original" {
		t.Fatalf("active repository head = %q, want original", got)
	}
}

func TestRepositorySetupResponseNeverContainsSecret(t *testing.T) {
	remote := &domain.RepositoryRemoteSummary{Name: "origin", URL: "https://example.com/repo.git", AuthType: domain.RepositoryAuthHTTPS, Managed: true}
	store := &fakeRepositorySetupStore{configuration: domain.RepositoryConfiguration{State: domain.RepositorySetupRegistered}}
	provisioner := &fakeRepositoryProvisioner{provisionConfiguration: readyConfiguration("main", remote)}
	service, _ := newRepositorySetupService(store, provisioner)
	secret := "never-return-this-token"

	status, err := service.Setup(context.Background(), domain.RepositorySetupRequest{
		Mode: domain.RepositorySetupRemote, Branch: "main",
		Author: domain.GitIdentity{Name: "Ada", Email: "ada@example.com"},
		Remote: &domain.RepositoryRemoteInput{URL: remote.URL, Authentication: domain.RemoteAuthenticationInput{Type: domain.RepositoryAuthHTTPS, Username: "git", Secret: secret}},
	})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(status)
	if err != nil {
		t.Fatal(err)
	}
	if string(payload) == "" || contains(string(payload), secret) {
		t.Fatalf("status JSON exposed submitted secret: %s", payload)
	}
}

func TestRepositoryAuthenticationJSONExcludesCredentials(t *testing.T) {
	tests := map[string]domain.RepositoryAuthentication{
		"https": {
			Type:     domain.RepositoryAuthHTTPS,
			Username: "private-user",
			Secret:   "private-token",
		},
		"ssh": {
			Type:           domain.RepositoryAuthSSH,
			SSHKeyPath:     "/srv/porty/ssh/private-key",
			KnownHostsPath: "/srv/porty/ssh/private-known-hosts",
		},
	}

	for name, authentication := range tests {
		t.Run(name, func(t *testing.T) {
			payload, err := json.Marshal(authentication)
			if err != nil {
				t.Fatal(err)
			}
			if string(payload) != "{}" {
				t.Fatalf("authentication JSON = %s, want empty object", payload)
			}
		})
	}
}

func newRepositorySetupService(store *fakeRepositorySetupStore, provisioner *fakeRepositoryProvisioner) (*application.RepositorySetupService, *application.RepositoryService) {
	repository := application.NewRepositoryService(&setupGitRepository{head: "original"})
	return application.NewRepositorySetupService(store, provisioner, repository, application.NewCoordinator(), application.RepositorySetupOptions{
		SSHKeyPath: "/srv/porty/ssh/id", KnownHostsPath: "/srv/porty/ssh/known_hosts",
	}), repository
}

func validLocalSetupRequest() domain.RepositorySetupRequest {
	return domain.RepositorySetupRequest{Mode: domain.RepositorySetupInit, Branch: "main", Author: domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}}
}

func readyConfiguration(branch string, remote *domain.RepositoryRemoteSummary) domain.RepositoryConfiguration {
	return domain.RepositoryConfiguration{State: domain.RepositorySetupReady, Root: "/srv/porty/repository", Branch: branch, Author: domain.GitIdentity{Name: "Porty", Email: "porty@localhost"}, Remote: remote}
}

func assertModeAvailability(t *testing.T, modes []domain.RepositoryModeAvailability, mode domain.RepositorySetupMode, available bool) {
	t.Helper()
	for _, candidate := range modes {
		if candidate.Mode == mode {
			if candidate.Available != available {
				t.Fatalf("mode %q available = %v, want %v", mode, candidate.Available, available)
			}
			return
		}
	}
	t.Fatalf("mode %q missing from %#v", mode, modes)
}

func contains(value, substring string) bool {
	for index := 0; index+len(substring) <= len(value); index++ {
		if value[index:index+len(substring)] == substring {
			return true
		}
	}
	return false
}

type fakeRepositorySetupStore struct {
	mu                  sync.Mutex
	configuration       domain.RepositoryConfiguration
	authentication      domain.RepositoryAuthentication
	loadErr             error
	saveErr             error
	failSaves           int
	savedConfiguration  domain.RepositoryConfiguration
	savedAuthentication domain.RepositoryAuthentication
}

func (s *fakeRepositorySetupStore) Load(context.Context) (domain.RepositoryConfiguration, domain.RepositoryAuthentication, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.configuration, s.authentication, s.loadErr
}

func (s *fakeRepositorySetupStore) Save(_ context.Context, configuration domain.RepositoryConfiguration, authentication domain.RepositoryAuthentication) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.failSaves > 0 {
		s.failSaves--
		return errors.New("temporary persistence failure")
	}
	if s.saveErr != nil {
		return s.saveErr
	}
	s.configuration = configuration
	s.authentication = authentication
	s.savedConfiguration = configuration
	s.savedAuthentication = authentication
	return nil
}

type fakeRepositoryProvisioner struct {
	mu                     sync.Mutex
	path                   domain.RepositoryPathInspection
	inspection             domain.RemoteInspection
	provisionConfiguration domain.RepositoryConfiguration
	provisionErr           error
	openErr                error
	provisionCalls         int
	lastProvision          application.RepositoryProvisionRequest
	firstProvision         application.RepositoryProvisionRequest
	requireMatchingRetry   bool
	active                 int
	maxConcurrent          int
	entered                chan struct{}
	release                chan struct{}
}

func (f *fakeRepositoryProvisioner) InspectPath(context.Context) (domain.RepositoryPathInspection, error) {
	return f.path, nil
}

func (f *fakeRepositoryProvisioner) InspectRemote(_ context.Context, _ string, _ domain.RepositoryAuthentication) (domain.RemoteInspection, error) {
	return f.inspection, nil
}

func (f *fakeRepositoryProvisioner) Provision(_ context.Context, request application.RepositoryProvisionRequest) (application.GitRepository, domain.RepositoryConfiguration, error) {
	f.mu.Lock()
	f.provisionCalls++
	if f.provisionCalls == 1 {
		f.firstProvision = request
	} else if f.requireMatchingRetry && request != f.firstProvision {
		f.mu.Unlock()
		return nil, domain.RepositoryConfiguration{}, errors.New("retry request did not match")
	}
	f.lastProvision = request
	f.active++
	if f.active > f.maxConcurrent {
		f.maxConcurrent = f.active
	}
	entered := f.entered
	release := f.release
	f.mu.Unlock()
	if entered != nil {
		entered <- struct{}{}
	}
	if release != nil {
		<-release
	}
	f.mu.Lock()
	f.active--
	f.mu.Unlock()
	return &setupGitRepository{head: "configured"}, f.provisionConfiguration, f.provisionErr
}

func (f *fakeRepositoryProvisioner) ConfigureRemote(context.Context, application.RepositoryRemoteProvisionRequest, domain.RepositoryConfiguration) (application.GitRepository, domain.RepositoryConfiguration, error) {
	return &setupGitRepository{head: "configured"}, f.provisionConfiguration, nil
}

func (f *fakeRepositoryProvisioner) RemoveRemote(context.Context, domain.RepositoryConfiguration) (application.GitRepository, domain.RepositoryConfiguration, error) {
	return &setupGitRepository{head: "configured"}, f.provisionConfiguration, nil
}

func (f *fakeRepositoryProvisioner) Open(context.Context, domain.RepositoryConfiguration, domain.RepositoryAuthentication) (application.GitRepository, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	return &setupGitRepository{head: "configured"}, nil
}

type setupGitRepository struct{ head string }

func (*setupGitRepository) Status(context.Context) (domain.GitStatus, error) {
	return domain.GitStatus{}, nil
}
func (g *setupGitRepository) Head(context.Context) (string, error)       { return g.head, nil }
func (*setupGitRepository) Diff(context.Context, string) (string, error) { return "", nil }
func (*setupGitRepository) Commit(context.Context, string, string) (string, error) {
	return "", nil
}
func (*setupGitRepository) History(context.Context, int) ([]domain.GitCommit, error) { return nil, nil }
func (*setupGitRepository) Fetch(context.Context) error                              { return nil }
func (*setupGitRepository) PullFastForward(context.Context) error                    { return nil }
func (*setupGitRepository) Push(context.Context) error                               { return nil }
