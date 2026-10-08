package compose

import (
	"archive/tar"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"github.com/containerd/errdefs"
	"github.com/docker/compose/v5/pkg/api"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type guardDockerAPI interface {
	Info(context.Context, client.InfoOptions) (client.SystemInfoResult, error)
	ContainerList(context.Context, client.ContainerListOptions) (client.ContainerListResult, error)
	ContainerInspect(context.Context, string, client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	CopyFromContainer(context.Context, string, client.CopyFromContainerOptions) (client.CopyFromContainerResult, error)
}

// RuntimeGuard proves membership using a private process marker, never names or images.
type RuntimeGuard struct {
	mu                  sync.Mutex
	docker              guardDockerAPI
	path, token, daemon string
	protected           map[string]bool
	matchedContainers   []string
}

func NewRuntimeGuard(docker guardDockerAPI) (*RuntimeGuard, error) {
	directory, err := os.MkdirTemp("", "porty-runtime-")
	if err != nil {
		return nil, err
	}
	canonical, err := filepath.EvalSymlinks(directory)
	if err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	var random [32]byte
	if _, err := rand.Read(random[:]); err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	g := &RuntimeGuard{docker: docker, path: filepath.Join(canonical, "marker"), token: hex.EncodeToString(random[:]), protected: map[string]bool{}}
	if err := os.WriteFile(g.path, []byte(g.token), 0600); err != nil {
		os.RemoveAll(directory)
		return nil, err
	}
	return g, nil
}
func (g *RuntimeGuard) Close() error {
	g.mu.Lock()
	defer g.mu.Unlock()
	return os.RemoveAll(filepath.Dir(g.path))
}
func (g *RuntimeGuard) CheckProject(parent context.Context, project string) error {
	return g.checkProject(parent, project, false)
}

func (g *RuntimeGuard) checkProject(parent context.Context, project string, standalone bool) error {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.matchedContainers = nil
	ctx, cancel := context.WithTimeout(parent, 30*time.Second)
	defer cancel()
	stat, err := os.Lstat(g.path)
	if err != nil || !stat.Mode().IsRegular() || stat.Mode().Perm() != 0600 || stat.Size() != int64(len(g.token)) {
		return ErrProtectionUnavailable
	}
	data, err := os.ReadFile(g.path)
	if err != nil || string(data) != g.token {
		return ErrProtectionUnavailable
	}
	info, err := g.docker.Info(ctx, client.InfoOptions{})
	if err != nil || info.Info.ID == "" {
		return ErrProtectionUnavailable
	}
	if g.daemon != "" && g.daemon != info.Info.ID {
		return ErrProtectionUnavailable
	}
	g.daemon = info.Info.ID
	for attempt := 0; attempt < 2; attempt++ {
		list, err := g.docker.ContainerList(ctx, client.ContainerListOptions{})
		if err != nil || len(list.Items) > 1024 {
			return ErrProtectionUnavailable
		}
		matched := map[string]bool{}
		var matchedContainers []string
		changed := false
		for _, row := range list.Items {
			if row.State != "running" {
				return ErrProtectionUnavailable
			}
			result, err := g.docker.CopyFromContainer(ctx, row.ID, client.CopyFromContainerOptions{SourcePath: g.path})
			if err != nil {
				if !errdefs.IsNotFound(err) {
					return ErrProtectionUnavailable
				}
				// A missing path is safe only while the same container still exists and runs.
				current, inspectErr := g.docker.ContainerInspect(ctx, row.ID, client.ContainerInspectOptions{})
				if errdefs.IsNotFound(inspectErr) {
					changed = true
					break
				}
				if inspectErr != nil || current.Container.ID != row.ID || current.Container.State == nil || !current.Container.State.Running {
					return ErrProtectionUnavailable
				}
				continue
			}
			if result.Content == nil {
				return ErrProtectionUnavailable
			}
			content, readErr := io.ReadAll(io.LimitReader(result.Content, 8193))
			closeErr := result.Content.Close()
			if readErr != nil || closeErr != nil || len(content) > 8192 {
				return ErrProtectionUnavailable
			}
			archive := tar.NewReader(bytes.NewReader(content))
			header, err := archive.Next()
			if err != nil || header.Typeflag != tar.TypeReg || header.Name != filepath.Base(g.path) || header.Size != int64(len(g.token)) {
				return ErrProtectionUnavailable
			}
			value, err := io.ReadAll(io.LimitReader(archive, 65))
			if err != nil || string(value) != g.token {
				return ErrProtectionUnavailable
			}
			if _, err := archive.Next(); !errors.Is(err, io.EOF) {
				return ErrProtectionUnavailable
			}
			name := row.Labels[api.ProjectLabel]
			if name == "" && !standalone {
				return ErrProtectionUnavailable
			}
			matched[name] = true
			matchedContainers = append(matchedContainers, row.ID)
		}
		if changed {
			continue
		}
		after, err := g.docker.ContainerList(ctx, client.ContainerListOptions{})
		if err != nil || containerMembership(list.Items) != containerMembership(after.Items) {
			continue
		}
		finalInfo, err := g.docker.Info(ctx, client.InfoOptions{})
		if err != nil || finalInfo.Info.ID != g.daemon {
			return ErrProtectionUnavailable
		}
		for name := range matched {
			g.protected[name] = true
		}
		g.matchedContainers = matchedContainers
		if g.protected[project] {
			return ErrSelfProtected
		}
		return nil
	}
	return ErrProtectionUnavailable
}
func containerMembership(rows []container.Summary) string {
	ids := make([]string, 0, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID+":"+string(row.State)+":"+row.Labels[api.ProjectLabel])
	}
	sort.Strings(ids)
	return strings.Join(ids, "\n")
}
func (c *Client) CheckProject(ctx context.Context, project string) error {
	if c.guard == nil {
		return ErrProtectionUnavailable
	}
	return c.guard.CheckProject(ctx, project)
}
