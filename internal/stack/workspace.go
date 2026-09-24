package stack

import (
	"context"
	"path/filepath"

	portycompose "github.com/msoldin/porty/internal/compose"
	portyfs "github.com/msoldin/porty/internal/filesystem"
)

type WorkspaceFiles interface {
	Tree(string) ([]portyfs.FileEntry, error)
	Read(string, string) (portyfs.FileContent, error)
	Write(string, string, []byte, string) (portyfs.FileContent, error)
	CreateFile(string, string, []byte) (portyfs.FileContent, error)
	CreateDirectory(string, string) error
	Move(string, string, string) error
	Remove(string, string) error
}

type StackLookup interface {
	ByID(context.Context, StackID) (Stack, error)
}

type WorkspaceService struct {
	stacks      *StackService
	lookup      StackLookup
	files       WorkspaceFiles
	environment *EnvironmentService
	coordinator operationLocker
	runtime     StackShutdown
	root        string
}

type StackShutdown interface {
	Down(context.Context, portycompose.Request) error
}

type operationLocker interface {
	Try(bool, string) (func(), error)
}

func NewCoordinatedWorkspaceService(stacks *StackService, lookup StackLookup, files WorkspaceFiles, environment *EnvironmentService, coordinator operationLocker, runtime StackShutdown, root string) *WorkspaceService {
	return &WorkspaceService{stacks: stacks, lookup: lookup, files: files, environment: environment, coordinator: coordinator, runtime: runtime, root: root}
}

func (s *WorkspaceService) lock(repository bool, id StackID) (func(), error) {
	if s.coordinator == nil {
		return func() {}, nil
	}
	return s.coordinator.Try(repository, string(id))
}

func (s *WorkspaceService) ListStacks(ctx context.Context) ([]Stack, error) {
	return s.stacks.Discover(ctx)
}

func (s *WorkspaceService) CreateStack(ctx context.Context, name string) (Stack, error) {
	release, err := s.lock(true, "")
	if err != nil {
		return Stack{}, err
	}
	defer release()
	return s.stacks.Create(ctx, name)
}

func (s *WorkspaceService) RenameStack(ctx context.Context, id StackID, name string) (Stack, error) {
	release, err := s.lock(false, id)
	if err != nil {
		return Stack{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return Stack{}, err
	}
	return s.stacks.Rename(ctx, stack, name)
}

func (s *WorkspaceService) DeleteStack(ctx context.Context, id StackID) error {
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
		request := portycompose.Request{StackDir: filepath.Join(s.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
		if err := s.runtime.Down(ctx, request); err != nil {
			return err
		}
	}
	return s.stacks.Delete(ctx, stack)
}

func (s *WorkspaceService) PurgeStack(ctx context.Context, id StackID) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.stacks.Purge(ctx, id)
}

func (s *WorkspaceService) Tree(ctx context.Context, id StackID) ([]portyfs.FileEntry, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return nil, err
	}
	return s.files.Tree(stack.DirectoryName)
}

func (s *WorkspaceService) ReadFile(ctx context.Context, id StackID, path string) (portyfs.FileContent, error) {
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return portyfs.FileContent{}, err
	}
	return s.files.Read(stack.DirectoryName, path)
}

func (s *WorkspaceService) WriteFile(ctx context.Context, id StackID, path string, contents []byte, expected string) (portyfs.FileContent, error) {
	release, err := s.lock(false, id)
	if err != nil {
		return portyfs.FileContent{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return portyfs.FileContent{}, err
	}
	return s.files.Write(stack.DirectoryName, path, contents, expected)
}

func (s *WorkspaceService) CreateFile(ctx context.Context, id StackID, path string, contents []byte) (portyfs.FileContent, error) {
	release, err := s.lock(false, id)
	if err != nil {
		return portyfs.FileContent{}, err
	}
	defer release()
	stack, err := s.lookup.ByID(ctx, id)
	if err != nil {
		return portyfs.FileContent{}, err
	}
	return s.files.CreateFile(stack.DirectoryName, path, contents)
}

func (s *WorkspaceService) CreateDirectory(ctx context.Context, id StackID, path string) error {
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

func (s *WorkspaceService) MoveFile(ctx context.Context, id StackID, from, to string) error {
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

func (s *WorkspaceService) RemoveFile(ctx context.Context, id StackID, path string) error {
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

func (s *WorkspaceService) EnvironmentKeys(ctx context.Context, id StackID) ([]string, error) {
	return s.environment.Keys(ctx, id)
}

func (s *WorkspaceService) EnvironmentValue(ctx context.Context, id StackID, key string) (EnvironmentValue, error) {
	return s.environment.Value(ctx, id, key)
}

func (s *WorkspaceService) SetEnvironmentWithSecret(ctx context.Context, id StackID, key, value string, secret bool) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.environment.SetWithSecret(ctx, id, key, value, secret)
}

func (s *WorkspaceService) SetEnvironment(ctx context.Context, id StackID, key, value string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.environment.Set(ctx, id, key, value)
}

func (s *WorkspaceService) DeleteEnvironment(ctx context.Context, id StackID, key string) error {
	release, err := s.lock(false, id)
	if err != nil {
		return err
	}
	defer release()
	return s.environment.Delete(ctx, id, key)
}
