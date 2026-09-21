package gitcli

import (
	"os"

	"golang.org/x/sys/unix"
)

func publishGitMetadata(source, destination *os.File) error {
	return unix.Renameat2(int(source.Fd()), "metadata", int(destination.Fd()), ".git", unix.RENAME_NOREPLACE)
}
