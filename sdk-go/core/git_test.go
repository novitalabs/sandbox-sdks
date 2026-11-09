package core

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
)

// gitStreamServer replies to process.start with fixed stdout and a zero exit.
func gitStreamServer(t *testing.T, stdout string, capture *string) http.Handler {
	t.Helper()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasSuffix(r.URL.Path, "/Start") {
			body := readStreamRequest(t, r)
			if capture != nil {
				if process, ok := body["process"].(map[string]interface{}); ok {
					if args, ok := process["args"].([]interface{}); ok && len(args) == 3 {
						*capture = args[2].(string)
					}
				}
			}

			writeStreamFrames(t, w,
				map[string]interface{}{
					"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}},
				},
				map[string]interface{}{
					"event": map[string]interface{}{
						"data": map[string]interface{}{"stdout": []byte(stdout)},
					},
				},
				map[string]interface{}{
					"event": map[string]interface{}{
						"end": map[string]interface{}{"exitCode": 0, "exited": true},
					},
				},
			)
			return
		}
		http.NotFound(w, r)
	})
}

func TestGitStatusParsesPorcelainOutput(t *testing.T) {
	output := "## main...origin/main\n" +
		" M internal/foo.go\n" +
		"A  internal/bar.go\n" +
		"?? internal/new.go\n"

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, output, nil))

	status, err := sandbox.Git.Status(context.Background(), nil)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}

	if status.Branch != "main" {
		t.Errorf("Branch = %q, want main", status.Branch)
	}
	if len(status.Files) != 3 {
		t.Fatalf("got %d files, want 3", len(status.Files))
	}
	if status.Files[0].Path != "internal/foo.go" || status.Files[0].Status != "M" {
		t.Errorf("file 0 = %+v", status.Files[0])
	}
	if status.Files[1].Path != "internal/bar.go" || status.Files[1].Status != "A" {
		t.Errorf("file 1 = %+v", status.Files[1])
	}
	if !status.HasUncommitted {
		t.Error("expected HasUncommitted to be true")
	}
}

func TestGitStatusCleanTree(t *testing.T) {
	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "## main...origin/main\n", nil))

	status, err := sandbox.Git.Status(context.Background(), nil)
	if err != nil {
		t.Fatalf("Status failed: %v", err)
	}
	if len(status.Files) != 0 {
		t.Errorf("got %d files, want 0", len(status.Files))
	}
	if status.HasUncommitted {
		t.Error("expected HasUncommitted to be false")
	}
}

func TestParseBranchHeader(t *testing.T) {
	cases := map[string]string{
		"main":                         "main",
		"main...origin/main":           "main",
		"main...origin/main [ahead 1]": "main",
		"feature/x...origin/feature/x [ahead 2, behind 3]": "feature/x",
		"No commits yet on main":                           "No",
	}

	for header, want := range cases {
		if got := parseBranchHeader(header); got != want {
			t.Errorf("parseBranchHeader(%q) = %q, want %q", header, got, want)
		}
	}
}

func TestGitBranches(t *testing.T) {
	output := "* main\n" +
		"  feature/login\n" +
		"  remotes/origin/main\n" +
		"  remotes/origin/develop\n" +
		"  remotes/origin/HEAD -> origin/main\n"

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, output, nil))

	branches, err := sandbox.Git.Branches(context.Background(), nil)
	if err != nil {
		t.Fatalf("Branches failed: %v", err)
	}

	if branches.Current != "main" {
		t.Errorf("Current = %q, want main", branches.Current)
	}
	if len(branches.Local) != 2 {
		t.Errorf("Local = %v, want 2 entries", branches.Local)
	}
	if len(branches.Remote) != 2 {
		t.Errorf("Remote = %v, want 2 entries (the symbolic HEAD must be skipped)", branches.Remote)
	}
}

func TestGitCloneBuildsCommand(t *testing.T) {
	var command string

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "", &command))

	err := sandbox.Git.Clone(context.Background(), "https://github.com/org/repo.git", "/workspace/repo",
		&GitCloneOpts{Branch: "develop"})
	if err != nil {
		t.Fatalf("Clone failed: %v", err)
	}

	if !strings.HasPrefix(command, "git clone") {
		t.Errorf("command = %q, want it to start with 'git clone'", command)
	}
	if !strings.Contains(command, "--branch 'develop'") {
		t.Errorf("command = %q, want --branch develop", command)
	}
	if !strings.Contains(command, "https://github.com/org/repo.git") {
		t.Errorf("command = %q, want the repository URL", command)
	}
}

func TestGitCloneChecksOutCommit(t *testing.T) {
	var commands []string
	sandbox := newTestSandbox(t, "0.5.7", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/Start") {
			http.NotFound(w, r)
			return
		}
		body := readStreamRequest(t, r)
		process := body["process"].(map[string]interface{})
		args := process["args"].([]interface{})
		commands = append(commands, args[2].(string))
		writeStreamFrames(t, w,
			map[string]interface{}{"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}}},
			map[string]interface{}{"event": map[string]interface{}{"end": map[string]interface{}{"exitCode": 0, "exited": true}}},
		)
	}))
	if err := sandbox.Git.Clone(context.Background(), "https://example.com/repo.git", "/repo", &GitCloneOpts{Commit: "abc123"}); err != nil {
		t.Fatal(err)
	}
	if len(commands) != 2 || !strings.Contains(commands[1], "checkout 'abc123'") {
		t.Fatalf("commands = %#v", commands)
	}
}

func TestGitCommitWithAuthor(t *testing.T) {
	var command string

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "", &command))

	err := sandbox.Git.Commit(context.Background(), "initial commit", &GitCommitOpts{
		Author: &GitAuthor{Name: "Ada", Email: "ada@example.com"},
	})
	if err != nil {
		t.Fatalf("Commit failed: %v", err)
	}

	if !strings.Contains(command, "commit -m 'initial commit'") {
		t.Errorf("command = %q, want the commit message", command)
	}
	if !strings.Contains(command, "--author 'Ada <ada@example.com>'") {
		t.Errorf("command = %q, want the author override", command)
	}
}

func TestGitPushOptions(t *testing.T) {
	var command string

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "", &command))

	err := sandbox.Git.Push(context.Background(), &GitPushOpts{
		Branch:      "main",
		Force:       true,
		SetUpstream: true,
	})
	if err != nil {
		t.Fatalf("Push failed: %v", err)
	}

	for _, want := range []string{"git push", "--set-upstream", "--force", "origin", "main"} {
		if !strings.Contains(command, want) {
			t.Errorf("command = %q, want it to contain %q", command, want)
		}
	}
}

func TestGitDeleteBranchForce(t *testing.T) {
	var command string

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "", &command))

	if err := sandbox.Git.DeleteBranch(context.Background(), "stale", &GitDeleteBranchOpts{Force: true}); err != nil {
		t.Fatalf("DeleteBranch failed: %v", err)
	}
	if !strings.Contains(command, "branch -D 'stale'") {
		t.Errorf("command = %q, want branch -D 'stale'", command)
	}
}

func TestShellQuoteEscapesSingleQuotes(t *testing.T) {
	cases := map[string]string{
		"simple":     "'simple'",
		"with space": "'with space'",
		"it's":       `'it'\''s'`,
		"$(whoami)":  "'$(whoami)'",
	}

	for in, want := range cases {
		if got := shellQuote(in); got != want {
			t.Errorf("shellQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestGitCommandFailureReturnsExitError(t *testing.T) {
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		writeStreamFrames(t, w,
			map[string]interface{}{
				"event": map[string]interface{}{"start": map[string]interface{}{"pid": 1}},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"data": map[string]interface{}{"stderr": []byte("fatal: not a git repository")},
				},
			},
			map[string]interface{}{
				"event": map[string]interface{}{
					"end": map[string]interface{}{"exitCode": 128, "exited": true},
				},
			},
		)
	})

	sandbox := newTestSandbox(t, "0.5.7", handler)

	err := sandbox.Git.Init(context.Background(), "/tmp/repo", nil)
	if err == nil {
		t.Fatal("expected an error for a failing git command")
	}

	exitErr, ok := err.(*CommandExitError)
	if !ok {
		t.Fatalf("error = %T, want *CommandExitError", err)
	}
	if exitErr.ExitCode != 128 {
		t.Errorf("ExitCode = %d, want 128", exitErr.ExitCode)
	}
}

func TestGitDangerouslyAuthenticateWritesCredentials(t *testing.T) {
	var command string

	sandbox := newTestSandbox(t, "0.5.7", gitStreamServer(t, "", &command))

	if err := sandbox.Git.DangerouslyAuthenticate(context.Background(), "user", "token123", nil); err != nil {
		t.Fatalf("DangerouslyAuthenticate failed: %v", err)
	}

	if !strings.Contains(command, "credential.helper store") {
		t.Errorf("command = %q, want the credential helper to be configured", command)
	}
	if !strings.Contains(command, "user:token123@github.com") {
		t.Errorf("command = %q, want the credentials to be written", command)
	}
}

// ensure json stays referenced by helpers in this file.
var _ = json.Marshal
