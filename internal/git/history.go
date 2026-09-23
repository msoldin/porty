package git

import (
	"context"
	"errors"
	"io"
	"strings"
	"time"

	gitlib "github.com/go-git/go-git/v6"
	"github.com/go-git/go-git/v6/plumbing"
	portyrepo "github.com/msoldin/porty/internal/repository"
)

func (c *Client) sdkHistoryPage(ctx context.Context, limit, offset int) ([]portyrepo.GitCommit, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	configured, err := c.configured()
	if err != nil || !configured {
		return []portyrepo.GitCommit{}, err
	}
	repo, err := c.openRepository()
	if err != nil {
		return nil, err
	}
	defer repo.Close()
	head, err := repo.Head()
	if errors.Is(err, plumbing.ErrReferenceNotFound) {
		return []portyrepo.GitCommit{}, nil
	}
	if err != nil {
		return nil, err
	}
	iter, err := repo.Log(&gitlib.LogOptions{From: head.Hash()})
	if err != nil {
		return nil, err
	}
	defer iter.Close()
	result := make([]portyrepo.GitCommit, 0, limit)
	for index := 0; len(result) < limit; index++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		commit, err := iter.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if index < offset {
			continue
		}
		subject, _, _ := strings.Cut(commit.Message, "\n")
		result = append(result, portyrepo.GitCommit{
			SHA: commit.Hash.String(), Subject: subject,
			Author: commit.Author.Name, Time: commit.Author.When.Format(time.RFC3339),
		})
	}
	return result, nil
}
