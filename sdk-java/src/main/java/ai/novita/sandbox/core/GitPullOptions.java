package ai.novita.sandbox.core;

/** Optional pull target, one-command authentication and explicit rebase policy. */
public class GitPullOptions {
    private String remote, branch;
    private GitAuth auth;
    private Boolean rebase;
    public String getRemote() { return remote; }
    public GitPullOptions setRemote(String value) { remote = value; return this; }
    public String getBranch() { return branch; }
    public GitPullOptions setBranch(String value) { branch = value; return this; }
    public GitAuth getAuth() { return auth; }
    public GitPullOptions setAuth(GitAuth value) { auth = value; return this; }
    public Boolean getRebase() { return rebase; }
    public GitPullOptions setRebase(Boolean value) { rebase = value; return this; }
}
