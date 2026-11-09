package ai.novita.sandbox.core;

import java.util.Map;

public class CommandOpts {
    private String workDir;
    private Map<String, String> env;
    private int timeout = 60;
    private Integer requestTimeoutSeconds;
    /** User to run the command as, matching JS/Python {@code CommandStartOpts.user}. */
    private String user;
    public String getWorkDir() { return workDir; }
    public void setWorkDir(String workDir) { this.workDir = workDir; }
    public Map<String, String> getEnv() { return env; }
    public void setEnv(Map<String, String> env) { this.env = env; }
    public int getTimeout() { return timeout; }
    public void setTimeout(int timeout) { if(timeout<0)throw new IllegalArgumentException("timeout must be non-negative"); this.timeout = timeout; }
    public int getTimeoutSeconds() { return timeout; }
    public void setTimeoutSeconds(int value) { setTimeout(value); }
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public void setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;}
    public String getUser() { return user; }
    public void setUser(String user) { this.user = user; }
}
