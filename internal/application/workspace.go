package application

import (
	"context"
	"path/filepath"

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
	coordinator *Coordinator
	runtime     StackShutdown
	root        string
}

type StackShutdown interface {
	Down(context.Context, ComposeRequest) error
}

func NewWorkspaceService(stacks *StackService, lookup StackLookup, files WorkspaceFiles, environment *EnvironmentService) *WorkspaceService {
	return &WorkspaceService{stacks: stacks, lookup: lookup, files: files, environment: environment}
}

func NewCoordinatedWorkspaceService(stacks *StackService, lookup StackLookup, files WorkspaceFiles, environment *EnvironmentService, coordinator *Coordinator, runtime StackShutdown, root string) *WorkspaceService {
	return &WorkspaceService{stacks: stacks, lookup: lookup, files: files, environment: environment, coordinator: coordinator, runtime: runtime, root: root}
}

func (s *WorkspaceService) lock(repository bool, id domain.StackID) (func(), error) {
	if s.coordinator == nil {
		return func() {}, nil
	}
	return s.coordinator.Try(repository, string(id))
}

func (s *WorkspaceService) ListStacks(ctx context.Context) ([]domain.Stack, error) {
	return s.stacks.Discover(ctx)
}

func (s *WorkspaceService) CreateStack(ctx context.Context, name string) (domain.Stack, error) {
	release, err := s.lock(true, "")
	if err != nil {
		return domain.Stack{}, err
	}
	defer release()
	return s.stacks.Create(ctx, name)
}

func (s *WorkspaceService) RenameStack(ctx context.Context, id domain.StackID, name string) (domain.Stack, error) {
	release, err := s.lock(false, id)
	if err != nil {
		return domain.Stack{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.Stack{}, err
	}
	return s.stacks.Rename(ctx, stack, name)
}

func (s *WorkspaceService) DeleteStack(ctx context.Context, id domain.StackID) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	if s.runtime != nil {
		values, err := s.environment.Values(ctx, id)
		if err != nil {
			return err
		}
		request := ComposeRequest{StackDir: filepath.Join(s.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
		if err := s.runtime.Down(ctx, request); err != nil {
			return err
		}
	}
	return s.stacks.Delete(ctx, stack)
}

func (s *WorkspaceService) PurgeStack(ctx context.Context, id domain.StackID) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
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
	release, err := s.lock(false, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.files.Write(stack.DirectoryName, path, contents, expected)
}

func (s *WorkspaceService) CreateFile(ctx context.Context, id domain.StackID, path string, contents []byte) (domain.FileContent, error) {
	release, err := s.lock(false, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return domain.FileContent{}, err
	}
	return s.files.CreateFile(stack.DirectoryName, path, contents)
}

func (s *WorkspaceService) CreateDirectory(ctx context.Context, id domain.StackID, path string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.files.CreateDirectory(stack.DirectoryName, path)
}

func (s *WorkspaceService) MoveFile(ctx context.Context, id domain.StackID, from, to string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return err
	}
	return s.files.Move(stack.DirectoryName, from, to)
}

func (s *WorkspaceService) RemoveFile(ctx context.Context, id domain.StackID, path string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
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
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.environment.Set(ctx, id, key, value)
}

func (s *WorkspaceService) DeleteEnvironment(ctx context.Context, id domain.StackID, key string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.environment.Delete(ctx, id, key)
}
