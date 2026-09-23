package compose

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/compose-spec/compose-go/v2/cli"
	"github.com/compose-spec/compose-go/v2/types"
	"github.com/docker/compose/v5/pkg/api"
	portyfs "github.com/msoldin/porty/internal/filesystem"
	"gopkg.in/yaml.v3"
)

func Load(ctx context.Context, request Request) (*types.Project, error) {
	if !filepath.IsAbs(request.StackDir) || request.ProjectName == "" {
		return nil, errors.New("invalid Compose request")
	}
	rootInfo, err := os.Lstat(request.StackDir)
	if err != nil {
		return nil, err
	}
	if !rootInfo.IsDir() || rootInfo.Mode()&os.ModeSymlink != 0 {
		return nil, errors.New("Compose stack directory is not safe")
	}
	if err := inspectComposeFiles(request.StackDir, "docker-compose.yml", map[string]bool{}); err != nil {
		return nil, redactComposeError(err, request.Environment)
	}
	if _, err := portyfs.SerializeEnvironment(request.Environment); err != nil {
		return nil, err
	}
	env := make([]string, 0, len(request.Environment))
	for name, value := range request.Environment {
		env = append(env, name+"="+value)
	}
	sort.Strings(env)
	options, err := cli.NewProjectOptions([]string{filepath.Join(request.StackDir, "docker-compose.yml")},
		cli.WithWorkingDirectory(request.StackDir), cli.WithName(request.ProjectName), cli.WithEnv(env))
	if err != nil {
		return nil, redactComposeError(err, request.Environment)
	}
	project, err := options.LoadProject(ctx)
	if err != nil {
		return nil, redactComposeError(err, request.Environment)
	}
	if len(project.Models) != 0 {
		return nil, errors.New("Compose model services require a helper process")
	}
	for name, service := range project.Services {
		if service.Provider != nil || len(service.Models) != 0 {
			return nil, errors.New("Compose provider and model services require a helper process")
		}
		if service.Build != nil {
			if remoteComposeResource(service.Build.Context) || len(service.Build.AdditionalContexts) != 0 ||
				len(service.Build.SSH) != 0 || len(service.Build.Secrets) != 0 || len(service.Build.Platforms) > 1 || service.Build.Privileged {
				return nil, errors.New("Compose build requires a helper process or remote resource")
			}
		}
		service.CustomLabels = types.Labels{
			api.ProjectLabel:     project.Name,
			api.ServiceLabel:     name,
			api.VersionLabel:     api.ComposeVersion,
			api.WorkingDirLabel:  project.WorkingDir,
			api.ConfigFilesLabel: strings.Join(project.ComposeFiles, ","),
			api.OneoffLabel:      "False",
		}
		project.Services[name] = service
	}
	return project, nil
}

func Digest(project *types.Project, environment map[string]string) (string, error) {
	if project == nil {
		return "", errors.New("nil Compose project")
	}
	encoded, err := project.MarshalJSON()
	if err != nil {
		return "", err
	}
	values, err := portyfs.SerializeEnvironment(environment)
	if err != nil {
		return "", err
	}
	hash := sha256.New()
	_, _ = hash.Write(encoded)
	_, _ = hash.Write([]byte{0})
	_, _ = hash.Write(values)
	return "sha256:" + hex.EncodeToString(hash.Sum(nil)), nil
}

func inspectComposeFiles(root, relative string, seen map[string]bool) error {
	if len(seen) >= 32 {
		return errors.New("too many Compose include files")
	}
	if !fs.ValidPath(filepath.ToSlash(relative)) || filepath.IsAbs(relative) {
		return errors.New("Compose include leaves the stack directory")
	}
	relative = filepath.Clean(relative)
	if seen[relative] {
		return nil
	}
	seen[relative] = true
	current := root
	for _, component := range strings.Split(relative, string(filepath.Separator)) {
		current = filepath.Join(current, component)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Compose file path is a symlink")
		}
	}
	info, err := os.Lstat(current)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return errors.New("Compose file is not regular")
	}
	contents, err := os.ReadFile(current)
	if err != nil {
		return err
	}
	var document map[string]any
	if err := yaml.Unmarshal(contents, &document); err != nil {
		return err
	}
	for _, include := range includePaths(document["include"]) {
		if remoteComposeResource(include) {
			return errors.New("remote Git Compose include is not supported")
		}
		if filepath.IsAbs(include) {
			return errors.New("Compose include leaves the stack directory")
		}
		candidate := filepath.Clean(filepath.Join(filepath.Dir(relative), include))
		if err := inspectComposeFiles(root, candidate, seen); err != nil {
			return err
		}
	}
	return nil
}

func includePaths(value any) []string {
	var result []string
	switch typed := value.(type) {
	case string:
		result = append(result, typed)
	case []any:
		for _, item := range typed {
			result = append(result, includePaths(item)...)
		}
	case map[string]any:
		result = append(result, includePaths(typed["path"])...)
	}
	return result
}

func remoteComposeResource(value string) bool {
	return strings.Contains(value, "://") || strings.HasPrefix(value, "git@") || strings.HasPrefix(value, "git+") || strings.HasPrefix(value, "github.com/")
}

func redactComposeError(err error, environment map[string]string) error {
	message := err.Error()
	for _, value := range environment {
		if value != "" {
			message = strings.ReplaceAll(message, value, "[REDACTED]")
		}
	}
	if len(message) > maxCommandOutput {
		message = message[:maxCommandOutput]
	}
	return fmt.Errorf("Compose project: %s", message)
}
