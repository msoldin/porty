package main

import (
	"context"
	"io"
	"path/filepath"
	"strconv"
	"strings"
	"unicode"
)

type commandRunner func(context.Context, string, ...string) error

type processExitError interface {
	ExitCode() int
}

func runGitHelper(ctx context.Context, args []string, lookup func(string) (string, bool), stdout io.Writer, run commandRunner) (bool, int) {
	if marker, _ := lookup("PORTY_GIT_ASKPASS"); marker == "1" {
		if len(args) != 1 {
			return true, 2
		}
		prompt := strings.ToLower(args[0])
		name := ""
		switch {
		case strings.Contains(prompt, "username"):
			name = "PORTY_GIT_USERNAME"
		case strings.Contains(prompt, "password") || strings.Contains(prompt, "token"):
			name = "PORTY_GIT_PASSWORD"
		default:
			return true, 2
		}
		value, ok := lookup(name)
		if !ok {
			return true, 2
		}
		if _, err := io.WriteString(stdout, value); err != nil {
			return true, 1
		}
		return true, 0
	}
	if marker, _ := lookup("PORTY_GIT_SSH"); marker != "1" {
		return false, 0
	}
	key, keyOK := lookup("PORTY_GIT_SSH_KEY")
	knownHosts, hostsOK := lookup("PORTY_GIT_KNOWN_HOSTS")
	if !keyOK || !hostsOK || !filepath.IsAbs(key) || !filepath.IsAbs(knownHosts) || strings.IndexFunc(key+knownHosts, unicode.IsControl) >= 0 || run == nil || !validGitSSHArguments(args) {
		return true, 2
	}
	fixed := []string{"-F", "/dev/null", "-i", key, "-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=" + knownHosts, "-o", "GlobalKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes"}
	if err := run(ctx, "ssh", append(fixed, args...)...); err != nil {
		if exitError, ok := err.(processExitError); ok && exitError.ExitCode() > 0 {
			return true, exitError.ExitCode()
		}
		return true, 1
	}
	return true, 0
}

func validGitSSHArguments(args []string) bool {
	index := 0
	for index < len(args) && strings.HasPrefix(args[index], "-") {
		switch args[index] {
		case "-4", "-6", "-v", "-G":
			index++
		case "-p":
			if index+1 >= len(args) {
				return false
			}
			port, err := strconv.Atoi(args[index+1])
			if err != nil || port < 1 || port > 65535 {
				return false
			}
			index += 2
		case "-o":
			if index+1 >= len(args) || (args[index+1] != "SendEnv=GIT_PROTOCOL" && args[index+1] != "SetEnv=GIT_PROTOCOL=version=1" && args[index+1] != "SetEnv=GIT_PROTOCOL=version=2") {
				return false
			}
			index += 2
		default:
			return false
		}
	}
	if len(args)-index != 2 || args[index] == "" || strings.HasPrefix(args[index], "-") || strings.IndexFunc(args[index], unicode.IsControl) >= 0 {
		return false
	}
	command := args[index+1]
	prefixOK := strings.HasPrefix(command, "git-upload-pack '") || strings.HasPrefix(command, "git-receive-pack '")
	return prefixOK && strings.HasSuffix(command, "'") && strings.Count(command, "'") == 2 && strings.IndexFunc(command, unicode.IsControl) < 0
}
