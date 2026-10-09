package control

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	portycompose "github.com/msoldin/porty/internal/compose"
	portyop "github.com/msoldin/porty/internal/operation"
	portystack "github.com/msoldin/porty/internal/stack"
	"io"
	"path/filepath"
	"sync"
)

type DeploymentReview struct {
	StackID            string `json:"stackId"`
	SourceRevision     string `json:"sourceRevision"`
	UncommittedChanges bool   `json:"uncommittedChanges"`
}

var ErrDeploymentReviewChanged = errors.New("deployment source changed; review again")

// The process-local key prevents public revisions from becoming fingerprints of secrets.
type deploymentReviewKey struct {
	once    sync.Once
	key     [32]byte
	err     error
	entropy io.Reader
}

func (k *deploymentReviewKey) sign(payload []byte) (string, error) {
	k.once.Do(func() {
		reader := k.entropy
		if reader == nil {
			reader = rand.Reader
		}
		_, k.err = io.ReadFull(reader, k.key[:])
	})
	if k.err != nil {
		return "", errors.New("deployment review key unavailable")
	}
	mac := hmac.New(sha256.New, k.key[:])
	_, _ = mac.Write(payload)
	return hex.EncodeToString(mac.Sum(nil)), nil
}
func sameDeploymentRevision(actual, expected string) bool {
	return hmac.Equal([]byte(actual), []byte(expected))
}
func (c *ControlPlane) ReviewDeployment(ctx context.Context, id portystack.StackID) (DeploymentReview, error) {
	release, err := c.coordinator.Try(false, string(id))
	if err != nil {
		return DeploymentReview{}, err
	}
	defer release()
	stack, err := c.lookup.ByID(ctx, id)
	if err != nil {
		return DeploymentReview{}, err
	}
	values, err := c.environment.Values(ctx, id)
	if err != nil {
		return DeploymentReview{}, err
	}
	request := portycompose.Request{StackDir: filepath.Join(c.root, stack.DirectoryName), ProjectName: stack.ComposeProjectName, Environment: values}
	return c.reviewDeploymentLocked(ctx, id, stack, request)
}
func (c *ControlPlane) reviewDeploymentLocked(ctx context.Context, id portystack.StackID, stack portystack.Stack, request portycompose.Request) (DeploymentReview, error) {
	if stack.ArchivedAt != nil {
		return DeploymentReview{}, ErrStackRuntimeActionUnavailable
	}
	digest, err := c.runtime.Digest(ctx, request)
	if err != nil {
		return DeploymentReview{}, err
	}
	head, err := c.repository.Head(ctx)
	if err != nil {
		return DeploymentReview{}, err
	}
	diff, err := c.repository.Diff(ctx, stack.DirectoryName)
	if err != nil {
		return DeploymentReview{}, err
	}
	diffHash := sha256.Sum256([]byte(diff))
	payload, err := json.Marshal([7]string{"deployment-review-v1", string(id), stack.DirectoryName, stack.ComposeProjectName, digest, head, hex.EncodeToString(diffHash[:])})
	if err != nil {
		return DeploymentReview{}, err
	}
	revision, err := c.reviewKey.sign(payload)
	if err != nil {
		return DeploymentReview{}, err
	}
	return DeploymentReview{StackID: string(id), SourceRevision: revision, UncommittedChanges: diff != ""}, nil
}
func (c *ControlPlane) StartReviewedDeployment(ctx context.Context, id portystack.StackID, expected string) (portyop.Operation, error) {
	return c.startDeployment(ctx, id, "deploy", &expected)
}
