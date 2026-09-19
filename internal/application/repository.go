package application

import (
	"context"

	"github.com/msoldin/porty/internal/domain"
)

type GitRepository interface {
	Status(context.Context) (domain.GitStatus, error)
	Diff(context.Context, string) (string, error)
	Commit(context.Context, string, string) (string, error)
	History(context.Context, int) ([]domain.GitCommit, error)
	Fetch(context.Context) error
	PullFastForward(context.Context) error
	Push(context.Context) error
}

type RepositoryService struct{ git GitRepository }

func NewRepositoryService(git GitRepository) *RepositoryService {
	return &RepositoryService{git: git}
}

func (s *RepositoryService) Status(ctx context.Context) (domain.GitStatus, error) {
	return s.git.Status(ctx)
}

func (s *RepositoryService) Diff(ctx context.Context, stack string) (string, error) {
	return s.git.Diff(ctx, stack)
}

func (s *RepositoryService) Commit(ctx context.Context, stack, message string) (string, error) {
	return s.git.Commit(ctx, stack, message)
}

func (s *RepositoryService) History(ctx context.Context, limit int) ([]domain.GitCommit, error) {
	return s.git.History(ctx, limit)
}

func (s *RepositoryService) Pull(ctx context.Context) error {
	if err := s.git.Fetch(ctx); err != nil {
		return err
	}
	return s.git.PullFastForward(ctx)
}

func (s *RepositoryService) Push(ctx context.Context) error {
	return s.git.Push(ctx)
}
