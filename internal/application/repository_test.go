package application_test

import (
	"context"
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

type fakeGitRepository struct{ calls []string }

func (f *fakeGitRepository) Status(context.Context) (domain.GitStatus, error) {
	return domain.GitStatus{}, nil
}
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
