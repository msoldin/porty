package repository

import (
	"context"
	"sync"
)

type GitRepository interface {
	Status(context.Context) (GitStatus, error)
	Head(context.Context) (string, error)
	Diff(context.Context, string) (string, error)
	Commit(context.Context, string, string) (string, error)
	History(context.Context, int) ([]GitCommit, error)
	Fetch(context.Context) error
	PullFastForward(context.Context) error
	Push(context.Context) error
}

type RepositoryService struct {
	mu            sync.RWMutex
	git           GitRepository
	remoteEnabled bool
}

func NewRepositoryService(git GitRepository) *RepositoryService {
	return &RepositoryService{git: git, remoteEnabled: true}
}

func (s *RepositoryService) Status(ctx context.Context) (GitStatus, error) {
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

func (s *RepositoryService) History(ctx context.Context, limit int) ([]GitCommit, error) {
	return s.current().History(ctx, limit)
}

func (s *RepositoryService) HistoryPage(ctx context.Context, limit, offset int) ([]GitCommit, error) {
	git := s.current()
	if paged, ok := git.(interface {
		HistoryPage(context.Context, int, int) ([]GitCommit, error)
	}); ok {
		return paged.HistoryPage(ctx, limit, offset)
	}
	return git.History(ctx, limit)
}

func (s *RepositoryService) Replace(git GitRepository, remoteEnabled bool) {
	s.mu.Lock()
	s.git, s.remoteEnabled = git, remoteEnabled
	s.mu.Unlock()
}
func (s *RepositoryService) current() GitRepository { s.mu.RLock(); defer s.mu.RUnlock(); return s.git }
func (s *RepositoryService) remote() (GitRepository, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if !s.remoteEnabled {
		return nil, ErrRepositoryRemoteUnavailable
	}
	return s.git, nil
}

func (s *RepositoryService) Pull(ctx context.Context) error {
	git, err := s.remote()
	if err != nil {
		return err
	}
	if err := git.Fetch(ctx); err != nil {
		return err
	}
	return git.PullFastForward(ctx)
}

func (s *RepositoryService) Push(ctx context.Context) error {
	git, err := s.remote()
	if err != nil {
		return err
	}
	return git.Push(ctx)
}

func (s *RepositoryService) Fetch(ctx context.Context) error {
	git, err := s.remote()
	if err != nil {
		return err
	}
	return git.Fetch(ctx)
}
