package core

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
)

// GitAuth carries credentials for a Git operation.
type GitAuth struct {
	// Username for HTTP basic auth.
	Username string
	// Password (or token) for HTTP basic auth.
	Password string
}

// GitRequestOpts are the common options for Git operations.
type GitRequestOpts struct {
	// User runs the Git command as this user.
	User Username
	// RequestTimeoutMs overrides the per-request timeout.
	RequestTimeoutMs *int
}

// GitCloneOpts are options for cloning a repository.
type GitCloneOpts struct {
	GitRequestOpts
	// Branch to check out after cloning.
	Branch string
	// Commit to check out after cloning.
	Commit string
	// Username for HTTP basic auth.
	Username string
	// Password for HTTP basic auth.
	Password string
}

// GitCommitOpts are options for creating a commit.
type GitCommitOpts struct {
	GitRequestOpts
	// Author overrides the commit author.
	Author *GitAuthor
	// Add stages all changes before committing.
	Add bool
}

// GitAuthor identifies a commit author.
type GitAuthor struct {
	Name  string
	Email string
}

// GitPushOpts are options for pushing.
type GitPushOpts struct {
	GitRequestOpts
	Path   string
	Remote string
	// Branch to push.
	Branch string
	// Force pushes with --force.
	Force bool
	// SetUpstream passes --set-upstream.
	SetUpstream bool
	Username    string
	Password    string
}

// GitPullOpts are options for pulling.
type GitPullOpts struct {
	GitRequestOpts
	Path   string
	Remote string
	// Branch to pull.
	Branch   string
	Username string
	Password string
}

// GitDeleteBranchOpts are options for deleting a branch.
type GitDeleteBranchOpts struct {
	GitRequestOpts
	// Force deletes the branch even when unmerged.
	Force bool
}

// GitConfigOpts are options for setting Git config values.
type GitConfigOpts struct {
	GitRequestOpts
	// Scope is "global", "local", or "system".
	Scope string
}
type GitResetOpts struct {
	GitRequestOpts
	Mode   string
	Target string
	Paths  []string
}
type GitRestoreOpts struct {
	GitRequestOpts
	Paths    []string
	Staged   bool
	Worktree bool
	Source   string
}

// Git exposes Git operations inside the sandbox.
//
// Every method shells out through the commands service, so Git must be
// installed in the sandbox template.
type Git struct {
	sandbox *Sandbox
}

// newGit wires up the git module for a sandbox.
func newGit(sandbox *Sandbox) *Git {
	return &Git{sandbox: sandbox}
}

// run executes a Git command inside the sandbox.
//
// Sub-commands and flags are emitted verbatim; only values that could contain
// shell metacharacters (paths, URLs, messages, credentials) are quoted.
func (g *Git) run(ctx context.Context, args []string, opts GitRequestOpts, cwd string) (string, error) {
	command := "git " + strings.Join(args, " ")

	startOpts := &CommandStartOpts{
		User:             opts.User,
		Cwd:              cwd,
		RequestTimeoutMs: opts.RequestTimeoutMs,
	}

	result, err := newCommands(g.sandbox).Run(ctx, command, startOpts)
	if err != nil {
		return "", err
	}
	return result.Stdout, nil
}

// Clone clones a repository into the sandbox.
func (g *Git) Clone(ctx context.Context, url, path string, opts *GitCloneOpts) error {
	if opts == nil {
		opts = &GitCloneOpts{}
	}

	args := []string{"clone"}
	if opts.Branch != "" {
		args = append(args, "--branch", shellQuote(opts.Branch))
	}
	args = append(args, shellQuote(url), shellQuote(path))

	if opts.Username != "" {
		// Authenticate through the credential helper rather than the URL so
		// credentials do not end up in the shell history.
		if err := g.DangerouslyAuthenticate(ctx, opts.Username, opts.Password, &opts.GitRequestOpts); err != nil {
			return err
		}
	}

	_, err := g.run(ctx, args, opts.GitRequestOpts, "")
	if err != nil {
		return err
	}
	if opts.Commit != "" {
		_, err = g.run(ctx, []string{"checkout", shellQuote(opts.Commit)}, opts.GitRequestOpts, path)
	}
	return err
}

// Init initialises a repository.
func (g *Git) Init(ctx context.Context, path string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"init", shellQuote(path)}, *opts, "")
	return err
}

// RemoteAdd adds a remote.
func (g *Git) RemoteAdd(ctx context.Context, name, url string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"remote", "add", shellQuote(name), shellQuote(url)}, *opts, "")
	return err
}
func (g *Git) RemoteGet(ctx context.Context, name, path string, opts *GitRequestOpts) (string, error) {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	out, err := g.run(ctx, []string{"remote", "get-url", shellQuote(name)}, *opts, path)
	return strings.TrimSpace(out), err
}
func (g *Git) RemoteSetURL(ctx context.Context, name, url, path string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"remote", "set-url", shellQuote(name), shellQuote(url)}, *opts, path)
	return err
}
func (g *Git) RemoteRemove(ctx context.Context, name, path string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"remote", "remove", shellQuote(name)}, *opts, path)
	return err
}

func (g *Git) Remotes(ctx context.Context, path string, opts *GitRequestOpts) ([]string, error) {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	out, err := g.run(ctx, []string{"remote"}, *opts, path)
	if err != nil {
		return nil, err
	}
	var remotes []string
	for _, remote := range strings.Split(out, "\n") {
		if remote = strings.TrimSpace(remote); remote != "" {
			remotes = append(remotes, remote)
		}
	}
	return remotes, nil
}
func (g *Git) Reset(ctx context.Context, path string, opts *GitResetOpts) error {
	if opts == nil {
		opts = &GitResetOpts{}
	}
	args := []string{"reset"}
	if opts.Mode != "" {
		args = append(args, "--"+opts.Mode)
	}
	if opts.Target != "" {
		args = append(args, shellQuote(opts.Target))
	}
	for _, p := range opts.Paths {
		args = append(args, shellQuote(p))
	}
	_, err := g.run(ctx, args, opts.GitRequestOpts, path)
	return err
}
func (g *Git) Restore(ctx context.Context, path string, opts *GitRestoreOpts) error {
	if opts == nil {
		opts = &GitRestoreOpts{}
	}
	args := []string{"restore"}
	if opts.Staged {
		args = append(args, "--staged")
	}
	if opts.Worktree {
		args = append(args, "--worktree")
	}
	if opts.Source != "" {
		args = append(args, "--source", shellQuote(opts.Source))
	}
	for _, p := range opts.Paths {
		args = append(args, shellQuote(p))
	}
	_, err := g.run(ctx, args, opts.GitRequestOpts, path)
	return err
}

// Add stages paths.
func (g *Git) Add(ctx context.Context, pathspec string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"add", shellQuote(pathspec)}, *opts, "")
	return err
}

// Commit creates a commit.
func (g *Git) Commit(ctx context.Context, message string, opts *GitCommitOpts) error {
	if opts == nil {
		opts = &GitCommitOpts{}
	}

	if opts.Add {
		if err := g.Add(ctx, ".", &opts.GitRequestOpts); err != nil {
			return err
		}
	}

	args := []string{"commit", "-m", shellQuote(message)}
	if opts.Author != nil {
		args = append(args, "--author",
			shellQuote(fmt.Sprintf("%s <%s>", opts.Author.Name, opts.Author.Email)))
	}

	_, err := g.run(ctx, args, opts.GitRequestOpts, "")
	return err
}

// Push pushes commits to the remote.
func (g *Git) Push(ctx context.Context, opts *GitPushOpts) error {
	if opts == nil {
		opts = &GitPushOpts{}
	}
	if opts.Password != "" && opts.Username == "" {
		return &InvalidArgumentError{Argument: "username", Reason: "is required when using a password or token for git push"}
	}
	if opts.Username != "" && opts.Password != "" {
		remote := opts.Remote
		if remote == "" {
			remote = "origin"
		}
		return g.withRemoteCredentials(ctx, opts.Path, remote, opts.Username, opts.Password, opts.GitRequestOpts, func() error {
			clone := *opts
			clone.Username, clone.Password = "", ""
			clone.Remote = remote
			return g.Push(ctx, &clone)
		})
	}

	args := []string{"push"}
	if opts.SetUpstream {
		args = append(args, "--set-upstream")
	}
	if opts.Force {
		args = append(args, "--force")
	}
	if opts.Remote != "" {
		args = append(args, shellQuote(opts.Remote))
	}
	if opts.Branch != "" {
		if opts.Remote == "" {
			args = append(args, "origin")
		}
		args = append(args, shellQuote(opts.Branch))
	}
	_, err := g.run(ctx, args, opts.GitRequestOpts, opts.Path)
	return err
}

// Pull pulls changes from the remote.
func (g *Git) Pull(ctx context.Context, opts *GitPullOpts) error {
	if opts == nil {
		opts = &GitPullOpts{}
	}
	if opts.Password != "" && opts.Username == "" {
		return &InvalidArgumentError{Argument: "username", Reason: "is required when using a password or token for git pull"}
	}
	if opts.Username != "" && opts.Password != "" {
		remote := opts.Remote
		if remote == "" {
			remote = "origin"
		}
		return g.withRemoteCredentials(ctx, opts.Path, remote, opts.Username, opts.Password, opts.GitRequestOpts, func() error {
			clone := *opts
			clone.Username, clone.Password = "", ""
			clone.Remote = remote
			return g.Pull(ctx, &clone)
		})
	}

	args := []string{"pull"}
	if opts.Remote != "" {
		args = append(args, shellQuote(opts.Remote))
	}
	if opts.Branch != "" {
		if opts.Remote == "" {
			args = append(args, "origin")
		}
		args = append(args, shellQuote(opts.Branch))
	}
	if opts.Remote == "" && opts.Branch == "" {
		upstream, err := g.HasUpstream(ctx, opts.Path, &opts.GitRequestOpts)
		if err != nil {
			return err
		}
		if !upstream {
			return &GitUpstreamError{Operation: "pull", Reason: "no upstream is configured; provide Remote and Branch"}
		}
	}
	_, err := g.run(ctx, args, opts.GitRequestOpts, opts.Path)
	return err
}

func (g *Git) withRemoteCredentials(ctx context.Context, path, remote, username, password string, opts GitRequestOpts, operation func() error) error {
	original, err := g.RemoteGet(ctx, remote, path, &opts)
	if err != nil {
		return err
	}
	parsed, err := url.Parse(original)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return &InvalidArgumentError{Argument: "remote", Reason: "credential authentication requires an HTTP(S) remote URL"}
	}
	parsed.User = url.UserPassword(username, password)
	if err := g.RemoteSetURL(ctx, remote, parsed.String(), path, &opts); err != nil {
		return err
	}
	operationErr := operation()
	restoreErr := g.RemoteSetURL(ctx, remote, original, path, &opts)
	if operationErr != nil {
		return operationErr
	}
	return restoreErr
}

func (g *Git) HasUpstream(ctx context.Context, path string, opts *GitRequestOpts) (bool, error) {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	out, err := g.run(ctx, []string{"rev-parse", "--abbrev-ref", "--symbolic-full-name", "@{u}"}, *opts, path)
	if err != nil {
		if _, ok := err.(*CommandExitError); ok {
			return false, nil
		}
		return false, err
	}
	return strings.TrimSpace(out) != "", nil
}

// Checkout switches branches or restores files.
func (g *Git) Checkout(ctx context.Context, branch string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"checkout", shellQuote(branch)}, *opts, "")
	return err
}

// CreateBranch creates a new branch.
func (g *Git) CreateBranch(ctx context.Context, name string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}
	_, err := g.run(ctx, []string{"branch", shellQuote(name)}, *opts, "")
	return err
}

// DeleteBranch deletes a branch.
func (g *Git) DeleteBranch(ctx context.Context, name string, opts *GitDeleteBranchOpts) error {
	if opts == nil {
		opts = &GitDeleteBranchOpts{}
	}

	args := []string{"branch", "-d"}
	if opts.Force {
		args = []string{"branch", "-D"}
	}
	args = append(args, shellQuote(name))

	_, err := g.run(ctx, args, opts.GitRequestOpts, "")
	return err
}

// Status returns the working tree status.
func (g *Git) Status(ctx context.Context, opts *GitRequestOpts) (*GitStatus, error) {
	return g.StatusAt(ctx, "", opts)
}

func (g *Git) StatusAt(ctx context.Context, path string, opts *GitRequestOpts) (*GitStatus, error) {
	if opts == nil {
		opts = &GitRequestOpts{}
	}

	out, err := g.run(ctx, []string{"status", "--porcelain", "--branch"}, *opts, path)
	if err != nil {
		return nil, err
	}

	status := &GitStatus{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" {
			continue
		}

		if strings.HasPrefix(line, "## ") {
			header := strings.TrimPrefix(line, "## ")
			status.Branch = parseBranchHeader(header)
			if open := strings.LastIndex(header, " ["); open >= 0 {
				for _, count := range strings.Split(strings.TrimSuffix(header[open+2:], "]"), ",") {
					fields := strings.Fields(count)
					if len(fields) == 2 {
						value, _ := strconv.Atoi(fields[1])
						if fields[0] == "ahead" {
							status.Ahead = value
						}
						if fields[0] == "behind" {
							status.Behind = value
						}
					}
				}
			}
			continue
		}

		if len(line) < 3 {
			continue
		}
		status.Files = append(status.Files, GitFileStatus{
			Status: strings.TrimSpace(line[:2]),
			Path:   strings.TrimSpace(line[3:]),
		})
	}

	status.HasUncommitted = len(status.Files) > 0
	return status, nil
}

// parseBranchHeader extracts the branch name from a
// `git status --branch` header line.
func parseBranchHeader(header string) string {
	header = strings.TrimSpace(header)
	// e.g. "main...origin/main [ahead 1, behind 2]"
	if idx := strings.Index(header, " ["); idx >= 0 {
		header = header[:idx]
	}
	if idx := strings.Index(header, "..."); idx >= 0 {
		return strings.TrimSpace(header[:idx])
	}
	if idx := strings.Index(header, " "); idx >= 0 {
		return strings.TrimSpace(header[:idx])
	}
	return header
}

// Branches lists local and remote branches.
func (g *Git) Branches(ctx context.Context, opts *GitRequestOpts) (*GitBranches, error) {
	return g.BranchesAt(ctx, "", opts)
}

func (g *Git) BranchesAt(ctx context.Context, path string, opts *GitRequestOpts) (*GitBranches, error) {
	if opts == nil {
		opts = &GitRequestOpts{}
	}

	out, err := g.run(ctx, []string{"branch", "--all", "--no-color"}, *opts, path)
	if err != nil {
		return nil, err
	}

	branches := &GitBranches{}
	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if strings.TrimSpace(line) == "" {
			continue
		}

		isCurrent := strings.HasPrefix(line, "*")
		name := strings.TrimSpace(strings.TrimPrefix(strings.TrimPrefix(line, "*"), " "))

		if strings.HasPrefix(name, "remotes/") {
			name = strings.TrimPrefix(name, "remotes/")
		}

		if strings.Contains(name, " -> ") {
			// symbolic ref such as origin/HEAD -> origin/main
			continue
		}

		if strings.HasPrefix(name, "origin/") {
			branches.Remote = append(branches.Remote, strings.TrimPrefix(name, "origin/"))
			continue
		}

		branches.Local = append(branches.Local, name)
		if isCurrent {
			branches.Current = name
		}
	}

	return branches, nil
}

// Config sets a Git config value.
func (g *Git) Config(ctx context.Context, key, value string, opts *GitConfigOpts) error {
	return g.SetConfig(ctx, key, value, "", opts)
}

func (g *Git) SetConfig(ctx context.Context, key, value, path string, opts *GitConfigOpts) error {
	if opts == nil {
		opts = &GitConfigOpts{}
	}

	args := []string{"config"}
	if opts.Scope != "" {
		args = append(args, "--"+opts.Scope)
	}
	args = append(args, shellQuote(key), shellQuote(value))

	if key == "" {
		return &InvalidArgumentError{Argument: "key", Reason: "cannot be empty"}
	}
	if opts.Scope == "local" && path == "" {
		return &InvalidArgumentError{Argument: "path", Reason: "is required for local config"}
	}
	_, err := g.run(ctx, args, opts.GitRequestOpts, path)
	return err
}

func (g *Git) GetConfig(ctx context.Context, key, path string, opts *GitConfigOpts) (string, bool, error) {
	if opts == nil {
		opts = &GitConfigOpts{}
	}
	if key == "" {
		return "", false, &InvalidArgumentError{Argument: "key", Reason: "cannot be empty"}
	}
	if opts.Scope == "local" && path == "" {
		return "", false, &InvalidArgumentError{Argument: "path", Reason: "is required for local config"}
	}
	args := []string{"config"}
	if opts.Scope != "" {
		args = append(args, "--"+opts.Scope)
	}
	args = append(args, "--get", shellQuote(key))
	out, err := g.run(ctx, args, opts.GitRequestOpts, path)
	if err != nil {
		if _, ok := err.(*CommandExitError); ok {
			return "", false, nil
		}
		return "", false, err
	}
	return strings.TrimSpace(out), true, nil
}

func (g *Git) ConfigureUser(ctx context.Context, author GitAuthor, path string, opts *GitConfigOpts) error {
	if author.Name == "" || author.Email == "" {
		return &InvalidArgumentError{Argument: "author", Reason: "name and email are required"}
	}
	if err := g.SetConfig(ctx, "user.name", author.Name, path, opts); err != nil {
		return err
	}
	return g.SetConfig(ctx, "user.email", author.Email, path, opts)
}

// DangerouslyAuthenticate stores credentials in the sandbox's Git credential
// store.
//
// This writes the password in plaintext inside the sandbox, which is why the
// method name warns against it. Prefer a short-lived token.
func (g *Git) DangerouslyAuthenticate(ctx context.Context, username, password string, opts *GitRequestOpts) error {
	if opts == nil {
		opts = &GitRequestOpts{}
	}

	command := fmt.Sprintf(
		"git config --global credential.helper store && "+
			"printf '%%s\\n' %s > ~/.git-credentials",
		shellQuote(fmt.Sprintf("https://%s:%s@github.com", username, password)),
	)

	_, err := newCommands(g.sandbox).Run(ctx, command, &CommandStartOpts{
		User:             opts.User,
		RequestTimeoutMs: opts.RequestTimeoutMs,
	})
	return err
}

// shellQuote wraps a value in single quotes for safe shell interpolation.
func shellQuote(value string) string {
	return "'" + strings.ReplaceAll(value, "'", `'\''`) + "'"
}
