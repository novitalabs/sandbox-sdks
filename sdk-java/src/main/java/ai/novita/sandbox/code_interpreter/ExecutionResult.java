package ai.novita.sandbox.code_interpreter;

import java.util.*;

/** Collected stdout/stderr, rich results, execution error, and kernel execution count. */
public class ExecutionResult {
    private String stdout="",stderr="",error;
    private ExecutionError executionError;
    private Integer executionCount;
    private final List<Map<String,Object>> results=new ArrayList<>();
    private final List<CodeResult> richResults=new ArrayList<>();
    private final List<String> messages=new ArrayList<>();
    public String getStdout(){return stdout;}
    public void setStdout(String value){stdout=value==null?"":value;}
    public String getStderr(){return stderr;}
    public void setStderr(String value){stderr=value==null?"":value;}
    public String getError(){return error;}
    public void setError(String value){error=value;}
    public ExecutionError getExecutionError(){return executionError;}
    void setExecutionError(ExecutionError value){executionError=value;error=value==null?null:value.toString();}
    public Integer getExecutionCount(){return executionCount;}
    void setExecutionCount(int value){executionCount=value;}
    public List<Map<String,Object>> getResults(){return Collections.unmodifiableList(results);}
    public List<CodeResult> getRichResults(){return Collections.unmodifiableList(richResults);}
    public List<String> getMessages(){return Collections.unmodifiableList(messages);}
    public String getText(){for(CodeResult result:richResults)if(result.isMainResult())return result.getText();return null;}
    void addResult(Map<String,Object> value){results.add(Collections.unmodifiableMap(new LinkedHashMap<>(value)));}
    void addResult(CodeResult value){richResults.add(value);addResult(value.getRaw());}
    void addMessage(String value){messages.add(value);}
}
