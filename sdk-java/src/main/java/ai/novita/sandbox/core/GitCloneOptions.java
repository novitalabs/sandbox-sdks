package ai.novita.sandbox.core;
public class GitCloneOptions {
    private String branch;
    private GitAuth auth;
    private Integer depth;
    public String getBranch(){return branch;}
    public GitCloneOptions setBranch(String value){branch=value;return this;}
    public GitAuth getAuth(){return auth;}
    public GitCloneOptions setAuth(GitAuth value){auth=value;return this;}
    public Integer getDepth(){return depth;}
    /** Null leaves the clone depth unrestricted. */
    public GitCloneOptions setDepth(Integer value){if(value!=null&&value<=0)throw new IllegalArgumentException("depth must be positive");depth=value;return this;}
}
