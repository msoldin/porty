package git

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/go-git/go-git/v6/plumbing/object"
)

func (c *Client) sdkDiff(ctx context.Context, stack string) (string, error) {
	if err := c.stackDirectory(stack); err != nil {
		return "", err
	}
	repo, err := c.openRepository()
	if err != nil {
		return "", err
	}
	defer repo.Close()
	worktree, err := repo.Worktree()
	if err != nil {
		return "", err
	}
	status, err := worktree.Status()
	if err != nil {
		return "", err
	}
	var headTree *object.Tree
	if head, err := repo.Head(); err == nil {
		commit, err := repo.CommitObject(head.Hash())
		if err != nil {
			return "", err
		}
		headTree, err = commit.Tree()
		if err != nil {
			return "", err
		}
	}
	paths := make([]string, 0, len(status))
	for name := range status {
		if name == stack || strings.HasPrefix(name, stack+"/") {
			paths = append(paths, name)
		}
	}
	sort.Strings(paths)
	var output strings.Builder
	for _, name := range paths {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		if output.Len() >= maxDiffBytes {
			break
		}
		oldContent := ""
		if headTree != nil {
			file, err := headTree.File(name)
			if err == nil {
				if file.Size > maxUntrackedFileBytes {
					fmt.Fprintf(&output, "diff --git a/%s b/%s\nBinary files a/%s and b/%s differ\n", name, name, name, name)
					continue
				}
				oldContent, err = file.Contents()
				if err != nil {
					return "", err
				}
			}
		}
		path := filepath.Join(c.repo, filepath.FromSlash(name))
		info, err := os.Lstat(path)
		newContent := ""
		if err == nil && info.Mode().IsRegular() {
			if info.Size() > maxUntrackedFileBytes {
				fmt.Fprintf(&output, "diff --git a/%s b/%s\nBinary files a/%s and b/%s differ\n", name, name, name, name)
				continue
			}
			data, err := os.ReadFile(path)
			if err != nil {
				return "", err
			}
			newContent = string(data)
		} else if err != nil && !errors.Is(err, os.ErrNotExist) {
			return "", err
		}
		if oldContent == newContent {
			continue
		}
		if strings.ContainsRune(oldContent+newContent, 0) {
			fmt.Fprintf(&output, "diff --git a/%s b/%s\nBinary files a/%s and b/%s differ\n", name, name, name, name)
			continue
		}
		appendFileDiff(&output, name, oldContent, newContent)
		if output.Len() > maxDiffBytes {
			return output.String()[:maxDiffBytes], nil
		}
	}
	return output.String(), nil
}

func appendFileDiff(out *strings.Builder, name, oldText, newText string) {
	oldLines := splitDiffLines(oldText)
	newLines := splitDiffLines(newText)
	fmt.Fprintf(out, "diff --git a/%s b/%s\n", name, name)
	if oldText == "" {
		out.WriteString("new file mode 100644\n--- /dev/null\n")
	} else {
		fmt.Fprintf(out, "--- a/%s\n", name)
	}
	fmt.Fprintf(out, "+++ b/%s\n@@ -1,%d +1,%d @@\n", name, len(oldLines), len(newLines))
	for _, line := range oldLines {
		fmt.Fprintln(out, "-"+line)
	}
	for _, line := range newLines {
		fmt.Fprintln(out, "+"+line)
	}
}

func splitDiffLines(value string) []string {
	if value == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(value, "\n"), "\n")
}
