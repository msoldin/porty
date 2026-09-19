package filesystem

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/msoldin/porty/internal/domain"
)

var (
	ErrInvalidPath = errors.New("invalid path")
	ErrStaleFile   = errors.New("file changed since it was opened")
	ErrTooLarge    = errors.New("file is too large")
)

type Limits struct {
	MaxEditableBytes int64
	MaxDepth         int
	MaxEntries       int
}

type Manager struct {
	root   *os.Root
	limits Limits
}

func Open(path string, limits Limits) (*Manager, error) {
	root, err := os.OpenRoot(path)
	if err != nil {
		return nil, fmt.Errorf("open stack root: %w", err)
	}
	return &Manager{root: root, limits: limits}, nil
}

func (m *Manager) Close() error { return m.root.Close() }

func (m *Manager) Discover() ([]string, error) {
	entries, err := fs.ReadDir(m.root.FS(), ".")
	if err != nil {
		return nil, err
	}
	stacks := make([]string, 0)
	for _, entry := range entries {
		name := entry.Name()
		if strings.HasPrefix(name, ".") || !entry.IsDir() || !validStackName(name) {
			continue
		}
		info, err := m.root.Lstat(name)
		if err != nil || info.Mode()&os.ModeSymlink != 0 {
			continue
		}
		stack, err := m.root.OpenRoot(name)
		if err != nil {
			continue
		}
		compose, err := stack.Lstat("docker-compose.yml")
		_ = stack.Close()
		if err == nil && compose.Mode().IsRegular() {
			stacks = append(stacks, name)
		}
	}
	sort.Strings(stacks)
	return stacks, nil
}

func (m *Manager) CreateStack(name string) error {
	if !validStackName(name) {
		return ErrInvalidPath
	}
	if err := m.root.Mkdir(name, 0o700); err != nil {
		return err
	}
	stack, err := m.root.OpenRoot(name)
	if err != nil {
		_ = m.root.Remove(name)
		return err
	}
	defer stack.Close()
	if err := stack.WriteFile("docker-compose.yml", []byte("services: {}\n"), 0o640); err != nil {
		_ = m.root.RemoveAll(name)
		return err
	}
	return nil
}

func (m *Manager) RenameStack(from, to string) error {
	if !validStackName(from) || !validStackName(to) {
		return ErrInvalidPath
	}
	return m.root.Rename(from, to)
}

func (m *Manager) RemoveStack(name string) error {
	if !validStackName(name) {
		return ErrInvalidPath
	}
	info, err := m.root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return ErrInvalidPath
	}
	return m.root.RemoveAll(name)
}

func (m *Manager) Read(stackName, path string) (domain.FileContent, error) {
	stack, err := m.openStack(stackName)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer stack.Close()
	clean, info, err := validateRegularFile(stack, path)
	if err != nil {
		return domain.FileContent{}, err
	}
	if info.Size() > m.limits.MaxEditableBytes {
		return domain.FileContent{}, ErrTooLarge
	}
	contents, err := stack.ReadFile(clean)
	if err != nil {
		return domain.FileContent{}, err
	}
	return domain.FileContent{Path: clean, Content: contents, Hash: hash(contents), Size: int64(len(contents))}, nil
}

func (m *Manager) Write(stackName, path string, contents []byte, expectedHash string) (domain.FileContent, error) {
	if int64(len(contents)) > m.limits.MaxEditableBytes {
		return domain.FileContent{}, ErrTooLarge
	}
	stack, err := m.openStack(stackName)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer stack.Close()
	clean, info, err := validateRegularFile(stack, path)
	if err != nil {
		return domain.FileContent{}, err
	}
	current, err := stack.ReadFile(clean)
	if err != nil {
		return domain.FileContent{}, err
	}
	if expectedHash == "" || hash(current) != expectedHash {
		return domain.FileContent{}, ErrStaleFile
	}

	dir := filepath.Dir(clean)
	temporary := filepath.Join(dir, ".porty-write-"+randomSuffix())
	file, err := stack.OpenFile(temporary, os.O_CREATE|os.O_EXCL|os.O_WRONLY, info.Mode().Perm()&0o666)
	if err != nil {
		return domain.FileContent{}, err
	}
	removeTemporary := true
	defer func() {
		_ = file.Close()
		if removeTemporary {
			_ = stack.Remove(temporary)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return domain.FileContent{}, err
	}
	if err := file.Sync(); err != nil {
		return domain.FileContent{}, err
	}
	if err := file.Close(); err != nil {
		return domain.FileContent{}, err
	}
	if err := stack.Rename(temporary, clean); err != nil {
		return domain.FileContent{}, err
	}
	removeTemporary = false
	return domain.FileContent{Path: clean, Content: contents, Hash: hash(contents), Size: int64(len(contents))}, nil
}

func (m *Manager) CreateDirectory(stackName, path string) error {
	stack, err := m.openStack(stackName)
	if err != nil {
		return err
	}
	defer stack.Close()
	clean, err := cleanLocalPath(path)
	if err != nil {
		return err
	}
	components := strings.Split(clean, string(filepath.Separator))
	for index := range components {
		partial := filepath.Join(components[:index+1]...)
		info, statErr := stack.Lstat(partial)
		switch {
		case statErr == nil:
			if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
				return ErrInvalidPath
			}
		case errors.Is(statErr, os.ErrNotExist):
			if err := stack.Mkdir(partial, 0o750); err != nil {
				return err
			}
		default:
			return statErr
		}
	}
	return nil
}

func (m *Manager) CreateFile(stackName, path string, contents []byte) (domain.FileContent, error) {
	if int64(len(contents)) > m.limits.MaxEditableBytes {
		return domain.FileContent{}, ErrTooLarge
	}
	stack, err := m.openStack(stackName)
	if err != nil {
		return domain.FileContent{}, err
	}
	defer stack.Close()
	clean, err := cleanLocalPath(path)
	if err != nil {
		return domain.FileContent{}, err
	}
	if err := validateDirectory(stack, filepath.Dir(clean)); err != nil {
		return domain.FileContent{}, err
	}
	file, err := stack.OpenFile(clean, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if err != nil {
		return domain.FileContent{}, err
	}
	removeCreated := true
	defer func() {
		_ = file.Close()
		if removeCreated {
			_ = stack.Remove(clean)
		}
	}()
	if _, err := file.Write(contents); err != nil {
		return domain.FileContent{}, err
	}
	if err := file.Sync(); err != nil {
		return domain.FileContent{}, err
	}
	if err := file.Close(); err != nil {
		return domain.FileContent{}, err
	}
	removeCreated = false
	return domain.FileContent{Path: clean, Content: contents, Hash: hash(contents), Size: int64(len(contents))}, nil
}

func (m *Manager) Move(stackName, from, to string) error {
	stack, err := m.openStack(stackName)
	if err != nil {
		return err
	}
	defer stack.Close()
	fromClean, err := cleanLocalPath(from)
	if err != nil {
		return err
	}
	toClean, err := cleanLocalPath(to)
	if err != nil {
		return err
	}
	if _, err := validateEntry(stack, fromClean); err != nil {
		return err
	}
	if err := validateDirectory(stack, filepath.Dir(toClean)); err != nil {
		return err
	}
	if _, err := stack.Lstat(toClean); err == nil {
		return os.ErrExist
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return stack.Rename(fromClean, toClean)
}

func (m *Manager) Remove(stackName, path string) error {
	stack, err := m.openStack(stackName)
	if err != nil {
		return err
	}
	defer stack.Close()
	clean, err := cleanLocalPath(path)
	if err != nil {
		return err
	}
	info, err := validateEntry(stack, clean)
	if err != nil {
		return err
	}
	if info.IsDir() {
		return stack.RemoveAll(clean)
	}
	return stack.Remove(clean)
}

func (m *Manager) Tree(stackName string) ([]domain.FileEntry, error) {
	stack, err := m.openStack(stackName)
	if err != nil {
		return nil, err
	}
	defer stack.Close()
	result := make([]domain.FileEntry, 0)
	err = fs.WalkDir(stack.FS(), ".", func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if path == "." {
			return nil
		}
		if len(result) >= m.limits.MaxEntries || strings.Count(path, "/")+1 > m.limits.MaxDepth {
			return ErrTooLarge
		}
		if entry.Name() == ".git" {
			return fs.SkipDir
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		result = append(result, domain.FileEntry{Path: path, Size: info.Size(), IsDir: entry.IsDir(), Editable: info.Mode().IsRegular() && info.Size() <= m.limits.MaxEditableBytes})
		return nil
	})
	return result, err
}

func (m *Manager) openStack(name string) (*os.Root, error) {
	if !validStackName(name) {
		return nil, ErrInvalidPath
	}
	info, err := m.root.Lstat(name)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, ErrInvalidPath
	}
	root, err := m.root.OpenRoot(name)
	if err != nil {
		return nil, ErrInvalidPath
	}
	return root, nil
}

func validateRegularFile(root *os.Root, path string) (string, os.FileInfo, error) {
	clean, err := cleanLocalPath(path)
	if err != nil {
		return "", nil, err
	}
	components := strings.Split(clean, string(filepath.Separator))
	for index := range components {
		partial := filepath.Join(components[:index+1]...)
		info, err := root.Lstat(partial)
		if err != nil {
			return "", nil, err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return "", nil, ErrInvalidPath
		}
		if index < len(components)-1 && !info.IsDir() {
			return "", nil, ErrInvalidPath
		}
		if index == len(components)-1 {
			if !info.Mode().IsRegular() {
				return "", nil, ErrInvalidPath
			}
			return clean, info, nil
		}
	}
	return "", nil, ErrInvalidPath
}

func cleanLocalPath(path string) (string, error) {
	if path == "" || path == "." || !filepath.IsLocal(path) {
		return "", ErrInvalidPath
	}
	clean := filepath.Clean(path)
	for _, component := range strings.Split(clean, string(filepath.Separator)) {
		if component == ".git" {
			return "", ErrInvalidPath
		}
	}
	return clean, nil
}

func validateDirectory(root *os.Root, path string) error {
	if path == "." {
		return nil
	}
	clean, err := cleanLocalPath(path)
	if err != nil {
		return err
	}
	components := strings.Split(clean, string(filepath.Separator))
	for index := range components {
		partial := filepath.Join(components[:index+1]...)
		info, err := root.Lstat(partial)
		if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return ErrInvalidPath
		}
	}
	return nil
}

func validateEntry(root *os.Root, path string) (os.FileInfo, error) {
	clean, err := cleanLocalPath(path)
	if err != nil {
		return nil, err
	}
	if err := validateDirectory(root, filepath.Dir(clean)); err != nil {
		return nil, err
	}
	info, err := root.Lstat(clean)
	if err != nil {
		return nil, err
	}
	if info.Mode()&os.ModeSymlink != 0 || (!info.IsDir() && !info.Mode().IsRegular()) {
		return nil, ErrInvalidPath
	}
	return info, nil
}

func validStackName(name string) bool {
	if name == "" || name == "." || name == ".." || strings.HasPrefix(name, ".") || len(name) > 63 || !filepath.IsLocal(name) || filepath.Base(name) != name {
		return false
	}
	for index, character := range name {
		if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || (index > 0 && (character == '-' || character == '_')) {
			continue
		}
		return false
	}
	return true
}

func hash(contents []byte) string {
	digest := sha256.Sum256(contents)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func randomSuffix() string {
	var value [8]byte
	_, _ = rand.Read(value[:])
	return hex.EncodeToString(value[:])
}
