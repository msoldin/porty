package compose

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/docker/compose/v5/pkg/api"
)

func TestLoadAddsComposeOwnershipLabels(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"})
	if err != nil {
		t.Fatal(err)
	}
	labels := project.Services["app"].CustomLabels
	for key, want := range map[string]string{
		api.ProjectLabel: "sample", api.ServiceLabel: "app", api.OneoffLabel: "False",
		api.WorkingDirLabel: dir, api.ConfigFilesLabel: filepath.Join(dir, "docker-compose.yml"),
	} {
		if labels[key] != want {
			t.Errorf("Compose label %s = %q, want %q", key, labels[key], want)
		}
	}
}

func TestLoadUsesOnlyStoredInterpolationEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: example:${PORTY_AMBIENT:-safe}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PORTY_AMBIENT", "outside")
	project, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample", Environment: map[string]string{}})
	if err != nil {
		t.Fatal(err)
	}
	if project.Services["app"].Image != "example:safe" {
		t.Fatalf("image = %q", project.Services["app"].Image)
	}
}

func TestLoadRejectsRemoteGitIncludeBeforeExecution(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("include:\n  - https://example.com/repo.git#main:compose.yaml\nservices:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"})
	if err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("Load() error = %v", err)
	}
}

func TestLoadAcceptsLocalInclude(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("include:\n  - extra.yml\nservices:\n  app:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.yml"), []byte("services:\n  worker:\n    image: alpine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	project, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"})
	if err != nil || len(project.Services) != 2 {
		t.Fatalf("local include = %+v, %v", project, err)
	}
}

func TestLoadRejectsRemoteGitIncludeInNestedLocalFile(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("include:\n  - extra.yml\nservices: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "extra.yml"), []byte("include:\n  - git@github.com:team/repo.git\nservices: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"})
	if err == nil || !strings.Contains(err.Error(), "remote") {
		t.Fatalf("nested remote include = %v", err)
	}
}

func TestDigestIsStableAndChangesWithEnvironment(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    image: example:${TAG}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	request := Request{StackDir: dir, ProjectName: "sample", Environment: map[string]string{"TAG": "one"}}
	first, err := Load(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	digestOne, err := Digest(first, request.Environment)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Load(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	digestTwo, err := Digest(second, request.Environment)
	if err != nil || digestOne != digestTwo {
		t.Fatalf("unstable digest: %s, %s, %v", digestOne, digestTwo, err)
	}
	request.Environment["TAG"] = "two"
	third, err := Load(context.Background(), request)
	if err != nil {
		t.Fatal(err)
	}
	digestThree, err := Digest(third, request.Environment)
	if err != nil || digestThree == digestOne {
		t.Fatalf("environment did not change digest: %s, %s, %v", digestOne, digestThree, err)
	}
}

func TestLoadRejectsSymlinkedComposeFileAndProvider(t *testing.T) {
	dir := t.TempDir()
	outside := filepath.Join(t.TempDir(), "outside.yml")
	if err := os.WriteFile(outside, []byte("services: {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"}); err == nil {
		t.Fatal("symlinked Compose file accepted")
	}
	if err := os.Remove(filepath.Join(dir, "docker-compose.yml")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "docker-compose.yml"), []byte("services:\n  app:\n    provider:\n      type: sample\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(context.Background(), Request{StackDir: dir, ProjectName: "sample"}); err == nil {
		t.Fatal("provider accepted")
	}
}
