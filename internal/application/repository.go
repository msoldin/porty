package application

import (
	"context"
	"sync"

	"github.com/msoldin/porty/internal/domain"
)

type GitRepository interface {
	Status(context.Context) (domain.GitStatus, error)
	Head(context.Context) (string, error)
	Diff(context.Context, string) (string, error)
	Commit(context.Context, string, string) (string, error)
	History(context.Context, int) ([]domain.GitCommit, error)
	Fetch(context.Context) error
	PullFastForward(context.Context) error
	Push(context.Context) error
}

type RepositoryService struct {
	mu  sync.RWMutex
	git GitRepository
}

func NewRepositoryService(git GitRepository) *RepositoryService {
	return &RepositoryService{git: git}
}

func (s *RepositoryService) Status(ctx context.Context) (domain.GitStatus, error) {
	git := s.current()
	return git.Status(ctx)
}

func (s *RepositoryService) Head(ctx context.Context) (string, error) { return s.current().Head(ctx) }

func (s *RepositoryService) Diff(ctx context.Context, stack string) (string, error) {
	return s.current().Diff(ctx, stack)
}

func (s *RepositoryService) Commit(ctx context.Context, stack, message string) (string, error) {
	return s.current().Commit(ctx, stack, message)
}

func (s *RepositoryService) History(ctx context.Context, limit int) ([]domain.GitCommit, error) {
	return s.current().History(ctx, limit)
}

func (s *RepositoryService) HistoryPage(ctx context.Context, limit, offset int) ([]domain.GitCommit, error) {
	git := s.current()
	if paged, ok := git.(interface {
		HistoryPage(context.Context, int, int) ([]domain.GitCommit, error)
	}); ok {
		return paged.HistoryPage(ctx, limit, offset)
	}
	return git.History(ctx, limit)
}

func (s *RepositoryService) Replace(git GitRepository) { s.mu.Lock(); s.git = git; s.mu.Unlock() }
func (s *RepositoryService) current() GitRepository    { s.mu.RLock(); defer s.mu.RUnlock(); return s.git }

func (s *RepositoryService) Pull(ctx context.Context) error {
	git := s.current()
	if err := git.Fetch(ctx); err != nil {
		return err
	}
	return git.PullFastForward(ctx)
}

func (s *RepositoryService) Push(ctx context.Context) error {
	return s.current().Push(ctx)
}

func (s *RepositoryService) Fetch(ctx context.Context) error { return s.current().Fetch(ctx) }
