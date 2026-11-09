package ai.novita.sandbox.core;

/** Optional push target, one-command authentication and upstream tracking. */
public class GitPushOptions {
    private String remote, branch;
    private GitAuth auth;
    private boolean setUpstream;
    public String getRemote() { return remote; }
    public GitPushOptions setRemote(String value) { remote = value; return this; }
    public String getBranch() { return branch; }
    public GitPushOptions setBranch(String value) { branch = value; return this; }
    public GitAuth getAuth() { return auth; }
    public GitPushOptions setAuth(GitAuth value) { auth = value; return this; }
    public boolean isSetUpstream() { return setUpstream; }
    public GitPushOptions setSetUpstream(boolean value) { setUpstream = value; return this; }
}
