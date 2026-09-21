//go:build !linux

package gitcli

import (
	"errors"
	"os"
)

func publishGitMetadata(source, destination *os.File) error {
	return errors.New("atomic Git metadata publication is unsupported on this platform")
}
