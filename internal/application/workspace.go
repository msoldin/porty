package application

import (
	"context"

	"github.com/msoldin/porty/internal/domain"
)

type WorkspaceFiles interface {
	Tree(string) ([]domain.FileEntry, error)
	Read(string, string) (domain.FileContent, error)
	Write(string, string, []byte, string) (domain.FileContent, error)
	CreateFile(string, string, []byte) (domain.FileContent, error)
	CreateDirectory(string, string) error
	Move(string, string, string) error
	Remove(string, string) error
}

type StackLookup interface {
	ByID(context.Context, domain.StackID) (domain.Stack, error)
}

type WorkspaceService struct {
	stacks      *StackService
	lookup      StackLookup
	files       WorkspaceFiles
	environment *EnvironmentService
}

func NewWorkspaceService(stacks *StackService, lookup StackLookup, files WorkspaceFiles, environment *EnvironmentService) *WorkspaceService {
	return &WorkspaceService{stacks: stacks, lookup: lookup, files: files, environment: environment}
}

func (s *WorkspaceService) ListStacks(ctx context.Context) ([]domain.Stack, error) {
	return s.stacks.Discover(ctx)
}

func (s *WorkspaceService) CreateStack(ctx context.Context, name string) (domain.Stack, error) {
	return s.stacks.Create(ctx, name)
}

func (s *WorkspaceService) RenameStack(ctx context.Context, id domain.StackID, name string) (domain.Stack, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.Stack{}, err
	}
	return s.stacks.Rename(ctx, stack, name)
}

func (s *WorkspaceService) DeleteStack(ctx context.Context, id domain.StackID) error {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.stacks.Delete(ctx, stack)
}

func (s *WorkspaceService) PurgeStack(ctx context.Context, id domain.StackID) error {
	return s.stacks.Purge(ctx, id)
}

func (s *WorkspaceService) Tree(ctx context.Context, id domain.StackID) ([]domain.FileEntry, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.files.Tree(stack.DirectoryName)
}

func (s *WorkspaceService) ReadFile(ctx context.Context, id domain.StackID, path string) (domain.FileContent, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.files.Read(stack.DirectoryName, path)
}

func (s *WorkspaceService) WriteFile(ctx context.Context, id domain.StackID, path string, contents []byte, expected string) (domain.FileContent, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.files.Write(stack.DirectoryName, path, contents, expected)
}

func (s *WorkspaceService) CreateFile(ctx context.Context, id domain.StackID, path string, contents []byte) (domain.FileContent, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.files.CreateFile(stack.DirectoryName, path, contents)
}

func (s *WorkspaceService) CreateDirectory(ctx context.Context, id domain.StackID, path string) error {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.files.CreateDirectory(stack.DirectoryName, path)
}

func (s *WorkspaceService) MoveFile(ctx context.Context, id domain.StackID, from, to string) error {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.files.Move(stack.DirectoryName, from, to)
}

func (s *WorkspaceService) RemoveFile(ctx context.Context, id domain.StackID, path string) error {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.files.Remove(stack.DirectoryName, path)
}

func (s *WorkspaceService) EnvironmentKeys(ctx context.Context, id domain.StackID) ([]string, error) {
	return s.environment.Keys(ctx, id)
}

func (s *WorkspaceService) SetEnvironment(ctx context.Context, id domain.StackID, key, value string) error {
	return s.environment.Set(ctx, id, key, value)
}

func (s *WorkspaceService) DeleteEnvironment(ctx context.Context, id domain.StackID, key string) error {
	return s.environment.Delete(ctx, id, key)
}
