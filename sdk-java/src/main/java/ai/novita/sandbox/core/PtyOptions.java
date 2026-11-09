package ai.novita.sandbox.core;

import java.util.Collections;
import java.util.HashMap;
import java.util.Map;
import java.util.function.Consumer;

/** Options for creating an interactive pseudo-terminal. */
public final class PtyOptions {
    private PtySize size;
    private int timeoutSeconds=60;
    private Integer requestTimeoutSeconds;
    public int getTimeoutSeconds(){return timeoutSeconds;}
    public PtyOptions setTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("timeout must be non-negative");timeoutSeconds=value;return this;}
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public PtyOptions setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}
    private String user;
    private String workingDirectory;
    private Map<String,String> environment=new HashMap<>();
    private Consumer<byte[]> onData;

    public PtySize getSize(){return size;}
    public PtyOptions setSize(PtySize value){size=value;return this;}
    public String getUser(){return user;}
    public PtyOptions setUser(String value){user=value;return this;}
    public String getWorkingDirectory(){return workingDirectory;}
    public PtyOptions setWorkingDirectory(String value){workingDirectory=value;return this;}
    public Map<String,String> getEnvironment(){return Collections.unmodifiableMap(environment);}
    public PtyOptions setEnvironment(Map<String,String> value){environment=value==null?new HashMap<>():new HashMap<>(value);return this;}
    public Consumer<byte[]> getOnData(){return onData;}
    public PtyOptions setOnData(Consumer<byte[]> value){onData=value;return this;}
}
