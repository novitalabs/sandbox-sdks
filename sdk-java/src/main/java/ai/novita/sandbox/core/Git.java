package ai.novita.sandbox.core;

import java.util.Map;

/**
 * Git operations for a sandbox.
 *
 * <p>All commands are executed through {@link Commands#run} and every
 * user-supplied argument is passed through {@link Shell#quote} so that
 * metacharacters cannot break out of the intended command.
 */
public class Git {
    private final Sandbox sandbox;
    private final CommandOpts commandOptions;
    private volatile String workingDirectory;
    private String inRepo(String command) { return workingDirectory == null ? command : "cd " + Shell.quote(workingDirectory) + " && " + command; }
    private static <T> java.util.concurrent.CompletableFuture<T> async(java.util.concurrent.Callable<T> c){return AsyncSupport.supplyAsync(()->{try{return c.call();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    Git(Sandbox sandbox) {
        this(sandbox, new CommandOpts(), null);
    }

    private Git(Sandbox sandbox, CommandOpts options, String directory) {
        this.sandbox = sandbox;
        this.commandOptions = copyOptions(options);
        this.workingDirectory = directory;
    }

    /**
     * Returns an independent view using a snapshot of the supplied command options.
     * An explicit cwd selects this view's repository; an omitted cwd inherits the
     * current repository. Calling init on the view does not change its parent.
     * Timeouts, user and environment apply to every synchronous and async operation.
     */
    public Git withOptions(CommandOpts options) {
        CommandOpts copy = copyOptions(options);
        String directory = workingDirectory;
        if (copy.getWorkDir() != null) directory = null;
        else copy.setWorkDir(commandOptions.getWorkDir());
        return new Git(sandbox, copy, directory);
    }

    private static CommandOpts copyOptions(CommandOpts options) {
        CommandOpts copy = new CommandOpts();
        if (options == null) return copy;
        copy.setWorkDir(options.getWorkDir()); copy.setUser(options.getUser());
        copy.setTimeout(options.getTimeout());
        if (options.getRequestTimeoutSeconds() != null) copy.setRequestTimeoutSeconds(options.getRequestTimeoutSeconds());
        if (options.getEnv() != null) copy.setEnv(new java.util.LinkedHashMap<>(options.getEnv()));
        return copy;
    }

    private CommandResult run(String command, GitAuth auth) throws SandboxException {
        CommandOpts options = copyOptions(commandOptions);
        String username = auth == null ? null : auth.getUsername();
        String password = auth == null ? null : auth.getPassword();
        if (auth != null) {
            Map<String,String> env = options.getEnv() == null ? new java.util.LinkedHashMap<>() : options.getEnv();
            env.put("NOVITA_GIT_USER", username); env.put("NOVITA_GIT_PASS", password);
            options.setEnv(env);
        }
        try {
            return sandbox.commands().run(command, options);
        } catch (CommandExitException failure) {
            if (auth == null) throw failure;
            // Git or a remote helper can echo authentication in its failure
            // output. Preserve exit semantics without retaining that raw output.
            CommandResult result = new CommandResult(); result.setExitCode(failure.getExitCode());
            result.setStdout(redact(failure.getStdout(), username, password));
            result.setStderr(redact(failure.getStderr(), username, password));
            result.setError(redact(failure.getCommandError(), username, password));
            throw new CommandExitException(result);
        } catch (SandboxException failure) {
            if (auth != null) {
                for (Throwable cause = failure; cause != null; cause = cause.getCause()) {
                    if (!java.util.Objects.equals(cause.getMessage(), redact(cause.getMessage(), username, password)))
                        throw new GitException(redact(failure.getMessage(), username, password));
                }
            }
            throw failure;
        }
    }

    private static String redact(String value, String username, String password) {
        if (value == null) return null;
        if (password != null && !password.isEmpty()) value = value.replace(password, "[REDACTED]");
        if (username != null && !username.isEmpty()) value = value.replace(username, "[REDACTED]");
        return value;
    }

    private static String gitPrefix(GitAuth auth) throws GitException {
        if (auth == null) return "git";
        if (auth.getUsername() == null || auth.getPassword() == null)
            throw new GitException("git auth requires both username and password");
        // Reset configured helpers for this command only: supplied credentials
        // must not be persisted by a user's global credential store.
        return "git -c credential.helper= -c credential.helper=" + Shell.quote(
                "!f() { printf 'username=%s\\npassword=%s\\n' \"$NOVITA_GIT_USER\" \"$NOVITA_GIT_PASS\"; }; f");
    }

    /**
     * Clones a Git repository.
     *
     * <p>When {@link GitCloneOptions#getAuth()} is set, credentials are supplied
     * through a credential helper that reads them from environment variables, so
     * the password never appears in the process argument list.
     *
     * @param url Repository URL
     * @param path Target path in sandbox
     * @param opts Optional clone options
     * @throws SandboxException if the operation fails
     */
    public void clone(String url, String path, GitCloneOptions opts) throws SandboxException {
        if (url == null || url.isEmpty() || path == null || path.isEmpty())
            throw new GitException("repository URL and target path are required");

        GitAuth auth = opts == null ? null : opts.getAuth();
        StringBuilder command = new StringBuilder(gitPrefix(auth));
        command.append(" clone");
        if (opts != null && opts.getBranch() != null)
            command.append(" --branch ").append(Shell.quote(opts.getBranch()));
        if (opts != null && opts.getDepth() != null) command.append(" --depth ").append(opts.getDepth());
        command.append(" -- ").append(Shell.quote(url)).append(' ').append(Shell.quote(path));
        run(inRepo(command.toString()), auth);
    }

    /**
     * Initializes a new Git repository.
     *
     * @param path Repository path
     * @throws SandboxException if the operation fails
     */
    public void init(String path) throws SandboxException {
        if (path == null || path.isEmpty()) throw new GitException("repository path is required");
        run("git init " + Shell.quote(path), null);
        workingDirectory = path;
    }

    /**
     * Creates a commit.
     *
     * @param message Commit message
     * @throws SandboxException if the operation fails
     */
    public void commit(String message) throws SandboxException {
        if (message == null || message.isEmpty()) throw new GitException("commit message is required");
        run(inRepo("git commit -m " + Shell.quote(message)), null);
    }

    /**
     * Pushes commits to remote.
     *
     * @throws SandboxException if the operation fails
     */
    public void push() throws SandboxException {
        push(null);
    }

    /** Pushes to the selected remote/branch without persisting supplied credentials. */
    public void push(GitPushOptions options) throws SandboxException {
        GitAuth auth = options == null ? null : options.getAuth();
        StringBuilder command = new StringBuilder(gitPrefix(auth)).append(" push");
        if (options != null) {
            if (options.isSetUpstream()) command.append(" --set-upstream");
            appendRemote(command, options.getRemote(), options.getBranch());
        }
        run(inRepo(command.toString()), auth);
    }

    /**
     * Pulls changes from remote.
     *
     * @throws SandboxException if the operation fails
     */
    public void pull() throws SandboxException {
        pull(null);
    }

    /** Null rebase preserves Git's configured behavior; false explicitly disables it. */
    public void pull(GitPullOptions options) throws SandboxException {
        GitAuth auth = options == null ? null : options.getAuth();
        StringBuilder command = new StringBuilder(gitPrefix(auth)).append(" pull");
        if (options != null) {
            if (options.getRebase() != null) command.append(options.getRebase() ? " --rebase" : " --no-rebase");
            appendRemote(command, options.getRemote(), options.getBranch());
        }
        run(inRepo(command.toString()), auth);
    }

    private static void appendRemote(StringBuilder command, String remote, String branch) throws GitException {
        if (remote != null && remote.isEmpty()) throw new GitException("remote cannot be empty");
        if (branch != null && branch.isEmpty()) throw new GitException("branch cannot be empty");
        if (remote == null && branch != null) remote = "origin";
        if (remote != null) command.append(" -- ").append(Shell.quote(remote));
        if (branch != null) command.append(' ').append(Shell.quote(branch));
    }
    public java.util.concurrent.CompletableFuture<GitStatus> statusAsync(){return async(this::status);}
    public java.util.concurrent.CompletableFuture<GitBranches> branchesAsync(){return async(this::branches);}
    public java.util.concurrent.CompletableFuture<Void> commitAsync(String message){return async(()->{commit(message);return null;});}
    public java.util.concurrent.CompletableFuture<Void> pushAsync(){return async(()->{push();return null;});}
    public java.util.concurrent.CompletableFuture<Void> pullAsync(){return async(()->{pull();return null;});}
    public java.util.concurrent.CompletableFuture<Void> pushAsync(GitPushOptions options){return async(()->{push(options);return null;});}
    public java.util.concurrent.CompletableFuture<Void> pullAsync(GitPullOptions options){return async(()->{pull(options);return null;});}
    public java.util.concurrent.CompletableFuture<Void> cloneAsync(String url,String path,GitCloneOptions opts){return async(()->{clone(url,path,opts);return null;});}
    public java.util.concurrent.CompletableFuture<Void> initAsync(String path){return async(()->{init(path);return null;});}
    public java.util.concurrent.CompletableFuture<Void> addAsync(String... paths){return async(()->{add(paths);return null;});}
    public java.util.concurrent.CompletableFuture<Void> checkoutBranchAsync(String name){return async(()->{checkoutBranch(name);return null;});}
    public java.util.concurrent.CompletableFuture<Void> createBranchAsync(String name){return async(()->{createBranch(name);return null;});}
    public java.util.concurrent.CompletableFuture<Void> resetAsync(String mode,String ref){return async(()->{reset(mode,ref);return null;});}
    public java.util.concurrent.CompletableFuture<Void> deleteBranchAsync(String name,boolean force){return async(()->{deleteBranch(name,force);return null;});}
    public java.util.concurrent.CompletableFuture<Void> remoteAddAsync(String name,String url){return async(()->{remoteAdd(name,url);return null;});}
    public java.util.concurrent.CompletableFuture<String> remoteGetAsync(String name){return async(()->remoteGet(name));}
    public java.util.concurrent.CompletableFuture<Void> restoreAsync(String path){return async(()->{restore(path);return null;});}
    public java.util.concurrent.CompletableFuture<Void> setConfigAsync(String key,String value){return async(()->{setConfig(key,value);return null;});}
    public java.util.concurrent.CompletableFuture<String> getConfigAsync(String key){return async(()->getConfig(key));}
    public void add(String... paths) throws SandboxException { if(paths==null||paths.length==0)throw new GitException("at least one path is required"); StringBuilder c=new StringBuilder("git add --"); for(String p:paths){if(p==null||p.isEmpty())throw new GitException("path is required"); c.append(' ').append(Shell.quote(p));} run(inRepo(c.toString()),null); }
    public void createBranch(String name) throws SandboxException { requireName(name); run(inRepo("git checkout -b "+Shell.quote(name)),null); }
    public void checkoutBranch(String name) throws SandboxException { requireName(name); run(inRepo("git checkout "+Shell.quote(name)),null); }
    public void deleteBranch(String name,boolean force) throws SandboxException { requireName(name); run(inRepo("git branch "+(force?"-D ":"-d ")+Shell.quote(name)),null); }
    public void remoteAdd(String name,String url) throws SandboxException { requireName(name); if(url==null||url.isEmpty())throw new GitException("remote URL is required"); run(inRepo("git remote add "+Shell.quote(name)+" "+Shell.quote(url)),null); }
    public String remoteGet(String name) throws SandboxException { requireName(name); return run(inRepo("git remote get-url "+Shell.quote(name)),null).getStdout().trim(); }
    public void reset(String mode,String ref) throws SandboxException { if(mode==null||!java.util.Arrays.asList("soft","mixed","hard","merge","keep").contains(mode))throw new GitException("invalid reset mode: "+mode); if(ref==null||ref.isEmpty())throw new GitException("reset ref is required"); run(inRepo("git reset --"+mode+" "+Shell.quote(ref)),null); }
    public void restore(String path) throws SandboxException { if(path==null||path.isEmpty())throw new GitException("path is required"); run(inRepo("git restore -- "+Shell.quote(path)),null); }
    public void setConfig(String key,String value) throws SandboxException { requireName(key); if(value==null)throw new GitException("config value is required"); run(inRepo("git config "+Shell.quote(key)+" "+Shell.quote(value)),null); }
    public String getConfig(String key) throws SandboxException { requireName(key); return run(inRepo("git config --get "+Shell.quote(key)),null).getStdout().trim(); }

    /**
     * Reads the working tree status.
     *
     * <p>{@link GitStatus#getBranch()} returns the bare branch name; any
     * {@code ...upstream} suffix and ahead/behind annotation is stripped.
     */
    public GitStatus status() throws SandboxException {
        CommandResult r = run(inRepo("git status --porcelain -b"), null);
        GitStatus s = new GitStatus();
        String[] lines = r.getStdout().split("\\n");
        if (lines.length > 0 && lines[0].startsWith("## ")) s.setBranch(parseBranch(lines[0].substring(3)));
        for (int i = 1; i < lines.length; i++) {
            if (lines[i].length() >= 3) s.getFiles().add(new GitFileStatus(lines[i].substring(3), lines[i].substring(0, 2).trim()));
        }
        return s;
    }

    /** Extracts the branch name from a porcelain {@code ##} header line. */
    private static String parseBranch(String header) {
        String value = header.trim();
        int upstream = value.indexOf("...");
        if (upstream >= 0) value = value.substring(0, upstream);
        int annotation = value.indexOf(" [");
        if (annotation >= 0) value = value.substring(0, annotation);
        return value.trim();
    }

    public GitBranches branches() throws SandboxException {
        CommandResult r = run(inRepo("git branch -a"), null);
        GitBranches b = new GitBranches();
        for (String l : r.getStdout().split("\\n")) {
            String x = l.trim();
            if (x.isEmpty()) continue;
            boolean current = x.startsWith("* ");
            if (current) x = x.substring(2).trim();
            if (x.startsWith("remotes/")) b.getRemote().add(x.substring(8));
            else b.getLocal().add(x);
            if (current) b.setCurrent(x);
        }
        return b;
    }

    private static void requireName(String value) throws GitException { if(value==null||value.isEmpty()) throw new GitException("value is required"); }
}
