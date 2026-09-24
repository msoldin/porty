package stack

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

type StackFiles interface {
	Discover() ([]string, error)
	CreateStack(name string) error
	RenameStack(from, to string) error
	RemoveStack(name string) error
}

type StackRepository interface {
	Create(context.Context, Stack) error
	ByID(context.Context, StackID) (Stack, error)
	ByDirectory(context.Context, string) (Stack, error)
	Active(context.Context) ([]Stack, error)
	Rename(context.Context, StackID, string, time.Time) error
	Archive(context.Context, StackID, time.Time) error
	Purge(context.Context, StackID) error
}

type StackService struct {
	files StackFiles
	store StackRepository
	now   func() time.Time
}

func NewStackService(files StackFiles, store StackRepository) *StackService {
	return &StackService{files: files, store: store, now: time.Now}
}

func (s *StackService) Discover(ctx context.Context) ([]Stack, error) {
	directories, err := s.files.Discover()
	if err != nil {
		return nil, err
	}
	for _, directory := range directories {
		if _, err := s.store.ByDirectory(ctx, directory); err == nil {
			continue
		}
		now := s.now().UTC()
		id := StackID("stk_" + randomHex(12))
		stack := Stack{ID: id, DirectoryName: directory, ComposeProjectName: "porty-" + directory + "-" + randomHex(3), CreatedAt: now, UpdatedAt: now}
		if err := s.store.Create(ctx, stack); err != nil {
			return nil, err
		}
	}
	return s.store.Active(ctx)
}

func (s *StackService) Create(ctx context.Context, name string) (Stack, error) {
	if err := s.files.CreateStack(name); err != nil {
		return Stack{}, err
	}
	now := s.now().UTC()
	stack := Stack{ID: StackID("stk_" + randomHex(12)), DirectoryName: name, ComposeProjectName: "porty-" + name + "-" + randomHex(3), CreatedAt: now, UpdatedAt: now}
	if err := s.store.Create(ctx, stack); err != nil {
		_ = s.files.RemoveStack(name)
		return Stack{}, err
	}
	return stack, nil
}

func (s *StackService) Delete(ctx context.Context, stack Stack) error {
	if err := s.files.RemoveStack(stack.DirectoryName); err != nil && !errors.Is(err, ErrNotFound) {
		return err
	}
	return s.store.Archive(ctx, stack.ID, s.now().UTC())
}

func (s *StackService) Rename(ctx context.Context, stack Stack, name string) (Stack, error) {
	if err := s.files.RenameStack(stack.DirectoryName, name); err != nil {
		return Stack{}, err
	}
	now := s.now().UTC()
	if err := s.store.Rename(ctx, stack.ID, name, now); err != nil {
		_ = s.files.RenameStack(name, stack.DirectoryName)
		return Stack{}, err
	}
	stack.DirectoryName = name
	stack.UpdatedAt = now
	return stack, nil
}

func (s *StackService) Purge(ctx context.Context, id StackID) error {
	return s.store.Purge(ctx, id)
}

type EnvironmentRepository interface {
	SetEnvironment(context.Context, StackID, string, string) error
	SetEnvironmentWithSecret(context.Context, StackID, string, string, bool) error
	DeleteEnvironment(context.Context, StackID, string) error
	Environment(context.Context, StackID) (map[string]string, error)
	EnvironmentValue(context.Context, StackID, string) (EnvironmentValue, error)
}

type EnvironmentValue struct {
	Value  string `json:"value"`
	Secret bool   `json:"secret"`
}

type EnvironmentService struct{ store EnvironmentRepository }

var (
	ErrInvalidEnvironment         = errors.New("invalid environment value")
	ErrEnvironmentSecretImmutable = errors.New("environment secret setting cannot be changed")
	environmentKey                = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
)

func NewEnvironmentService(store EnvironmentRepository) *EnvironmentService {
	return &EnvironmentService{store: store}
}

func (s *EnvironmentService) Set(ctx context.Context, id StackID, key, value string) error {
	if !environmentKey.MatchString(key) || strings.ContainsRune(value, 0) {
		return ErrInvalidEnvironment
	}
	return s.store.SetEnvironment(ctx, id, key, value)
}

func (s *EnvironmentService) SetWithSecret(ctx context.Context, id StackID, key, value string, secret bool) error {
	if !environmentKey.MatchString(key) || strings.ContainsRune(value, 0) {
		return ErrInvalidEnvironment
	}
	return s.store.SetEnvironmentWithSecret(ctx, id, key, value, secret)
}

func (s *EnvironmentService) Delete(ctx context.Context, id StackID, key string) error {
	if !environmentKey.MatchString(key) {
		return ErrInvalidEnvironment
	}
	return s.store.DeleteEnvironment(ctx, id, key)
}

func (s *EnvironmentService) Keys(ctx context.Context, id StackID) ([]string, error) {
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

func (s *EnvironmentService) Value(ctx context.Context, id StackID, key string) (EnvironmentValue, error) {
	if !environmentKey.MatchString(key) {
		return EnvironmentValue{}, ErrInvalidEnvironment
	}
	return s.store.EnvironmentValue(ctx, id, key)
}

// Values is for trusted deployment infrastructure. HTTP handlers must expose Keys instead.
func (s *EnvironmentService) Values(ctx context.Context, id StackID) (map[string]string, error) {
	return s.store.Environment(ctx, id)
}

var ErrNotFound = errors.New("not found")

func randomHex(bytes int) string {
	value := make([]byte, bytes)
	_, _ = rand.Read(value)
	return hex.EncodeToString(value)
}
