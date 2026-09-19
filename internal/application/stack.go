package application

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/msoldin/porty/internal/domain"
)

type StackFiles interface {
	Discover() ([]string, error)
	CreateStack(name string) error
	RenameStack(from, to string) error
	RemoveStack(name string) error
}

type StackRepository interface {
	Create(context.Context, domain.Stack) error
	ByDirectory(context.Context, string) (domain.Stack, error)
	Active(context.Context) ([]domain.Stack, error)
	Rename(context.Context, domain.StackID, string, time.Time) error
	Archive(context.Context, domain.StackID, time.Time) error
	Purge(context.Context, domain.StackID) error
}

type StackService struct {
	files StackFiles
	store StackRepository
	now   func() time.Time
}

func NewStackService(files StackFiles, store StackRepository) *StackService {
	return &StackService{files: files, store: store, now: time.Now}
}

func (s *StackService) Discover(ctx context.Context) ([]domain.Stack, error) {
	directories, err := s.files.Discover()
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		if _, err := s.store.ByDirectory(ctx, directory); err == nil {
			continue
		}
		now := s.now().UTC()
		id := domain.StackID("stk_" + randomHex(12))
		stack := domain.Stack{ID: id, DirectoryName: directory, ComposeProjectName: "porty-" + directory + "-" + randomHex(3), CreatedAt: now, UpdatedAt: now}
		if err := s.store.Create(ctx, stack); err != nil {
			return nil, err
		}
	}
	return s.store.Active(ctx)
}

func (s *StackService) Create(ctx context.Context, name string) (domain.Stack, error) {
	if err := s.files.CreateStack(name); err != nil {
		return domain.Stack{}, err
	}
	now := s.now().UTC()
	stack := domain.Stack{ID: domain.StackID("stk_" + randomHex(12)), DirectoryName: name, ComposeProjectName: "porty-" + name + "-" + randomHex(3), CreatedAt: now, UpdatedAt: now}
	if err := s.store.Create(ctx, stack); err != nil {
		_ = s.files.RemoveStack(name)
		return domain.Stack{}, err
	}
	return stack, nil
}

func (s *StackService) Delete(ctx context.Context, stack domain.Stack) error {
	if err := s.files.RemoveStack(stack.DirectoryName); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.store.Archive(ctx, stack.ID, s.now().UTC())
}

func (s *StackService) Rename(ctx context.Context, stack domain.Stack, name string) (domain.Stack, error) {
	if err := s.files.RenameStack(stack.DirectoryName, name); err != nil {
		return domain.Stack{}, err
	}
	now := s.now().UTC()
	if err := s.store.Rename(ctx, stack.ID, name, now); err != nil {
		_ = s.files.RenameStack(name, stack.DirectoryName)
		return domain.Stack{}, err
	}
	stack.DirectoryName = name
	stack.UpdatedAt = now
	return stack, nil
}

func (s *StackService) Purge(ctx context.Context, id domain.StackID) error {
	return s.store.Purge(ctx, id)
}

type EnvironmentRepository interface {
	SetEnvironment(context.Context, domain.StackID, string, string) error
	DeleteEnvironment(context.Context, domain.StackID, string) error
	Environment(context.Context, domain.StackID) (map[string]string, error)
}

type EnvironmentService struct{ store EnvironmentRepository }

var (
	ErrInvalidEnvironment = errors.New("invalid environment value")
	environmentKey        = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func NewEnvironmentService(store EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{store: store}
}

func (s *EnvironmentService) Set(ctx context.Context, id domain.StackID, key, value string) error {
	if !environmentKey.MatchString(key) || strings.ContainsRune(value, 0) {
		return ErrInvalidEnvironment
	}
	return s.store.SetEnvironment(ctx, id, key, value)
}

func (s *EnvironmentService) Delete(ctx context.Context, id domain.StackID, key string) error {
	if !environmentKey.MatchString(key) {
		return ErrInvalidEnvironment
	}
	return s.store.DeleteEnvironment(ctx, id, key)
}

func (s *EnvironmentService) Keys(ctx context.Context, id domain.StackID) ([]string, error) {
	values, err := s.store.Environment(ctx, id)
	if err != nil {
		return nil, err
	}
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys, nil
}

// Values is for trusted deployment infrastructure. HTTP handlers must expose Keys instead.
func (s *EnvironmentService) Values(ctx context.Context, id domain.StackID) (map[string]string, error) {
	return s.store.Environment(ctx, id)
}

var ErrNotFound = errors.New("not found")

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
