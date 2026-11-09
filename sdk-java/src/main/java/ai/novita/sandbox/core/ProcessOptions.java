package ai.novita.sandbox.core;
import java.util.*;
public class ProcessOptions {
    private List<String> args=new ArrayList<>();
    private Map<String,String> environment=new HashMap<>();
    private String workingDirectory,tag,user;
    private PtySize pty;
    private Boolean stdin;
    private int timeoutSeconds = 60;
    private Integer requestTimeoutSeconds;
    public int getTimeoutSeconds(){return timeoutSeconds;}
    public ProcessOptions setTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("timeout must be non-negative");timeoutSeconds=value;return this;}
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public ProcessOptions setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}
    private java.util.function.Consumer<String> onStdout,onStderr;
    private java.util.function.Consumer<byte[]> onPty;
    /** Returns an unmodifiable view so callers cannot mutate the option in place. */
    public List<String> getArgs(){return Collections.unmodifiableList(args);}
    public ProcessOptions setArgs(List<String> v){args=v==null?new ArrayList<>():new ArrayList<>(v);return this;}
    /** Returns an unmodifiable view so callers cannot mutate the option in place. */
    public Map<String,String> getEnvironment(){return Collections.unmodifiableMap(environment);}
    public ProcessOptions setEnvironment(Map<String,String> v){environment=v==null?new HashMap<>():new HashMap<>(v);return this;}
    public String getWorkingDirectory(){return workingDirectory;}
    public ProcessOptions setWorkingDirectory(String v){workingDirectory=v;return this;}
    public String getTag(){return tag;}
    public ProcessOptions setTag(String v){tag=v;return this;}
    public String getUser(){return user;}
    public ProcessOptions setUser(String v){user=v;return this;}
    public PtySize getPty(){return pty;}
    public ProcessOptions setPty(PtySize v){pty=v;return this;}
    public Boolean getStdin(){return stdin;}
    public ProcessOptions setStdin(boolean value){stdin=value;return this;}
    public java.util.function.Consumer<String> getOnStdout(){return onStdout;}
    public ProcessOptions setOnStdout(java.util.function.Consumer<String> value){onStdout=value;return this;}
    public java.util.function.Consumer<String> getOnStderr(){return onStderr;}
    public ProcessOptions setOnStderr(java.util.function.Consumer<String> value){onStderr=value;return this;}
    public java.util.function.Consumer<byte[]> getOnPty(){return onPty;}
    public ProcessOptions setOnPty(java.util.function.Consumer<byte[]> value){onPty=value;return this;}
}
