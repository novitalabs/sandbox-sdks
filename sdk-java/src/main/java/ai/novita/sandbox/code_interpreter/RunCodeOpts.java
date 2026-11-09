package ai.novita.sandbox.code_interpreter;

import java.util.Map;
import java.util.function.Consumer;

/** Execution options; timeouts are seconds and zero disables the deadline. */
public class RunCodeOpts {
    private String language, contextId;
    private int timeout = 60;
    private Integer requestTimeoutSeconds;
    private Map<String,String> environment;
    private Consumer<String> onStdout, onStderr;
    private Consumer<CodeResult> onResult;
    private Consumer<ExecutionError> onError;
    public RunCodeOpts copy(){
        RunCodeOpts copy=new RunCodeOpts();copy.language=language;copy.contextId=contextId;copy.timeout=timeout;copy.requestTimeoutSeconds=requestTimeoutSeconds;
        copy.environment=environment==null?null:new java.util.LinkedHashMap<>(environment);copy.onStdout=onStdout;copy.onStderr=onStderr;copy.onResult=onResult;copy.onError=onError;return copy;
    }
    public String getLanguage(){return language;}
    public RunCodeOpts setLanguage(String value){language=value;return this;}
    public String getContextId(){return contextId;}
    public RunCodeOpts setContextId(String value){contextId=value;return this;}
    public int getTimeout(){return timeout;}
    public RunCodeOpts setTimeout(int value){if(value<0)throw new IllegalArgumentException("timeout must be non-negative");timeout=value;return this;}
    public Integer getRequestTimeoutSeconds(){return requestTimeoutSeconds;}
    public RunCodeOpts setRequestTimeoutSeconds(int value){if(value<0)throw new IllegalArgumentException("request timeout must be non-negative");requestTimeoutSeconds=value;return this;}
    public Map<String,String> getEnvironment(){return environment;}
    public RunCodeOpts setEnvironment(Map<String,String> value){environment=value;return this;}
    public Consumer<String> getOnStdout(){return onStdout;}
    public RunCodeOpts setOnStdout(Consumer<String> value){onStdout=value;return this;}
    public Consumer<String> getOnStderr(){return onStderr;}
    public RunCodeOpts setOnStderr(Consumer<String> value){onStderr=value;return this;}
    public Consumer<CodeResult> getOnResult(){return onResult;}
    public RunCodeOpts setOnResult(Consumer<CodeResult> value){onResult=value;return this;}
    public Consumer<ExecutionError> getOnError(){return onError;}
    public RunCodeOpts setOnError(Consumer<ExecutionError> value){onError=value;return this;}
}
