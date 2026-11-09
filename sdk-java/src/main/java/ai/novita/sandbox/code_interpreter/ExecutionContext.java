package ai.novita.sandbox.code_interpreter;

import ai.novita.sandbox.core.*;
import java.util.concurrent.*;

public class ExecutionContext extends CodeContext implements AutoCloseable {
    private final CodeInterpreterSandbox owner;
    private boolean closed;
    ExecutionContext(CodeInterpreterSandbox owner){this.owner=owner;}
    public ExecutionResult execute(String code)throws SandboxException{return execute(code,new RunCodeOpts());}
    public ExecutionResult execute(String code,RunCodeOpts options)throws SandboxException{
        if(closed)throw new SandboxException("Execution context is closed");
        RunCodeOpts effective=options==null?new RunCodeOpts():options.copy();
        if(effective.getLanguage()!=null)throw new IllegalArgumentException("A context determines its language");
        return owner.runCode(code,effective.setContextId(getId()));
    }
    public CompletableFuture<ExecutionResult> executeAsync(String code){return AsyncSupport.supplyAsync(()->{try{return execute(code);}catch(SandboxException e){throw new CompletionException(e);}});}
    public void restart()throws SandboxException{owner.restartContext(getId());}
    @Override public void close()throws SandboxException{if(!closed&&getId()!=null)owner.removeContext(getId());closed=true;}
}
