package main

import (
	"context"
	"strings"
	"testing"
)

func TestGitAskpassHelperReturnsUsernameAndSecretByPrompt(t *testing.T) {
	values := map[string]string{"PORTY_GIT_ASKPASS": "1", "PORTY_GIT_USERNAME": "deploy", "PORTY_GIT_PASSWORD": "secret"}
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	for _, test := range []struct{ prompt, want string }{{"Username for 'https://example.com':", "deploy"}, {"Password for 'https://deploy@example.com':", "secret"}} {
		var output strings.Builder
		handled, code := runGitHelper(context.Background(), []string{test.prompt}, lookup, &output, nil)
		if !handled || code != 0 || output.String() != test.want {
			t.Fatalf("runGitHelper(%q) = (%v, %d, %q)", test.prompt, handled, code, output.String())
		}
	}
}

func TestGitAskpassHelperRejectsUnexpectedInvocation(t *testing.T) {
	lookup := func(name string) (string, bool) {
		return map[string]string{"PORTY_GIT_ASKPASS": "1", "PORTY_GIT_USERNAME": "deploy", "PORTY_GIT_PASSWORD": "secret"}[name], true
	}
	var output strings.Builder
	handled, code := runGitHelper(context.Background(), []string{"Confirm?", "extra"}, lookup, &output, nil)
	if !handled || code == 0 || output.Len() != 0 {
		t.Fatalf("unexpected result (%v, %d, %q)", handled, code, output.String())
	}
}

func TestGitSSHHelperPrependsFixedSecurityArguments(t *testing.T) {
	values := map[string]string{"PORTY_GIT_SSH": "1", "PORTY_GIT_SSH_KEY": "/fixed/id", "PORTY_GIT_KNOWN_HOSTS": "/fixed/known_hosts"}
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	var gotName string
	var gotArgs []string
	handled, code := runGitHelper(context.Background(), []string{"-p", "2222", "git@example.com", "git-upload-pack '/team/repo.git'"}, lookup, &strings.Builder{}, func(_ context.Context, name string, args ...string) error { gotName, gotArgs = name, args; return nil })
	if !handled || code != 0 || gotName != "ssh" {
		t.Fatalf("result = (%v, %d, %q)", handled, code, gotName)
	}
	wantPrefix := []string{"-F", "/dev/null", "-i", "/fixed/id", "-o", "IdentitiesOnly=yes", "-o", "UserKnownHostsFile=/fixed/known_hosts", "-o", "GlobalKnownHostsFile=/dev/null", "-o", "StrictHostKeyChecking=yes", "-o", "BatchMode=yes"}
	if len(gotArgs) < len(wantPrefix) || strings.Join(gotArgs[:len(wantPrefix)], "\x00") != strings.Join(wantPrefix, "\x00") {
		t.Fatalf("args = %#v", gotArgs)
	}
}

func TestGitSSHHelperRejectsUnsafeGitArguments(t *testing.T) {
	values := map[string]string{"PORTY_GIT_SSH": "1", "PORTY_GIT_SSH_KEY": "/fixed/id", "PORTY_GIT_KNOWN_HOSTS": "/fixed/known_hosts"}
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	for _, args := range [][]string{{"-o", "ProxyCommand=evil", "git@example.com", "git-upload-pack '/repo'"}, {"git@example.com", "sh -c evil"}} {
		called := false
		_, code := runGitHelper(context.Background(), args, lookup, &strings.Builder{}, func(context.Context, string, ...string) error { called = true; return nil })
		if code == 0 || called {
			t.Fatalf("unsafe args accepted: %#v", args)
		}
	}
}

type exitCodeError int

func (e exitCodeError) Error() string { return "secret state" }
func (e exitCodeError) ExitCode() int { return int(e) }

func TestGitSSHHelperPropagatesExitCodeWithoutPrintingSecretState(t *testing.T) {
	values := map[string]string{"PORTY_GIT_SSH": "1", "PORTY_GIT_SSH_KEY": "/fixed/id", "PORTY_GIT_KNOWN_HOSTS": "/fixed/known_hosts"}
	lookup := func(name string) (string, bool) { value, ok := values[name]; return value, ok }
	var output strings.Builder
	_, code := runGitHelper(context.Background(), []string{"git@example.com", "git-receive-pack '/repo'"}, lookup, &output, func(context.Context, string, ...string) error { return exitCodeError(23) })
	if code != 23 || output.Len() != 0 {
		t.Fatalf("code/output = %d/%q", code, output.String())
	}
}
