package application_test

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/msoldin/porty/internal/application"
	"github.com/msoldin/porty/internal/domain"
)

func TestRepositoryServiceKeepsCommitStackScopedAndPullFastForwardOnly(t *testing.T) {
	git := &fakeGitRepository{}
	service := application.NewRepositoryService(git)
	ctx := context.Background()
	if _, err := service.Commit(ctx, "gateway", "update gateway"); err != nil {
		t.Fatal(err)
	}
	if err := service.Pull(ctx); err != nil {
		t.Fatal(err)
	}
	want := []string{"commit:gateway:update gateway", "fetch", "merge-ff-only"}
	if !reflect.DeepEqual(git.calls, want) {
		t.Fatalf("calls = %#v, want %#v", git.calls, want)
	}
}

func TestRepositoryServiceRejectsFetchWithoutManagedRemote(t *testing.T) {
	testRepositoryServiceRejectsRemote(t, func(s *application.RepositoryService) error { return s.Fetch(context.Background()) })
}
func TestRepositoryServiceRejectsPullWithoutManagedRemote(t *testing.T) {
	testRepositoryServiceRejectsRemote(t, func(s *application.RepositoryService) error { return s.Pull(context.Background()) })
}
func TestRepositoryServiceRejectsPushWithoutManagedRemote(t *testing.T) {
	testRepositoryServiceRejectsRemote(t, func(s *application.RepositoryService) error { return s.Push(context.Background()) })
}

func testRepositoryServiceRejectsRemote(t *testing.T, action func(*application.RepositoryService) error) {
	t.Helper()
	git := &fakeGitRepository{}
	service := application.NewRepositoryService(git)
	service.Replace(git, false)
	if err := action(service); !errors.Is(err, application.ErrRepositoryRemoteUnavailable) {
		t.Fatalf("error = %v", err)
	}
	if len(git.calls) != 0 {
		t.Fatalf("calls = %#v", git.calls)
	}
}

func TestRepositoryServiceEnablesRemoteActionsAfterClientReplacement(t *testing.T) {
	first, second := &fakeGitRepository{}, &fakeGitRepository{}
	service := application.NewRepositoryService(first)
	service.Replace(first, false)
	service.Replace(second, true)
	if err := service.Fetch(context.Background()); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(second.calls, []string{"fetch"}) {
		t.Fatalf("calls = %#v", second.calls)
	}
}

type fakeGitRepository struct{ calls []string }

func (f *fakeGitRepository) Status(context.Context) (domain.GitStatus, error) {
	return domain.GitStatus{}, nil
}
func (f *fakeGitRepository) Head(context.Context) (string, error)         { return "abc123", nil }
func (f *fakeGitRepository) Diff(context.Context, string) (string, error) { return "", nil }
func (f *fakeGitRepository) Commit(_ context.Context, stack, message string) (string, error) {
	f.calls = append(f.calls, "commit:"+stack+":"+message)
	return "abc123", nil
}
func (f *fakeGitRepository) History(context.Context, int) ([]domain.GitCommit, error) {
	return nil, nil
}
func (f *fakeGitRepository) Fetch(context.Context) error {
	f.calls = append(f.calls, "fetch")
	return nil
}
func (f *fakeGitRepository) PullFastForward(context.Context) error {
	f.calls = append(f.calls, "merge-ff-only")
	return nil
}
func (f *fakeGitRepository) Push(context.Context) error {
	f.calls = append(f.calls, "push")
	return nil
}
