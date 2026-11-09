package ai.novita.sandbox.code_interpreter;

import ai.novita.sandbox.core.*;
import okhttp3.*;
import com.google.gson.*;
import java.io.*;
import java.util.*;
import java.util.concurrent.*;

/** Stateful Jupyter execution over the sandbox's authenticated NDJSON API. */
public class CodeInterpreterSandbox implements AutoCloseable {
    private static final int SERVICE_PORT=49999;
    private static final Gson GSON=new Gson();
    private static final MediaType JSON=MediaType.get("application/json");
    private final Sandbox sandbox;
    private CodeInterpreterSandbox(Sandbox sandbox){this.sandbox=Objects.requireNonNull(sandbox);}
    public static CodeInterpreterSandbox create(String template,SandboxOpts opts)throws SandboxException{return wrap(Sandbox.create(template,opts));}
    public static CodeInterpreterSandbox connect(String id,SandboxOpts opts)throws SandboxException{return wrap(Sandbox.connect(id,opts));}
    public static CodeInterpreterSandbox wrap(Sandbox sandbox){return new CodeInterpreterSandbox(sandbox);}
    public Sandbox getSandbox(){return sandbox;}
    private static String enc(String value){return java.net.URLEncoder.encode(value,java.nio.charset.StandardCharsets.UTF_8).replace("+","%20");}
    private Request.Builder request(String path)throws SandboxException{
        return new Request.Builder().url(sandbox.portUrl(SERVICE_PORT)+path).headers(Headers.of(sandbox.portHeaders())).header("Content-Type","application/json");
    }
    public ExecutionResult runCode(String code)throws SandboxException{return runCode(code,null);}
    public ExecutionResult runCode(String code,RunCodeOpts options)throws SandboxException{
        RunCodeOpts opts=options==null?new RunCodeOpts():options.copy();
        if(opts.getLanguage()!=null&&opts.getContextId()!=null)throw new IllegalArgumentException("Provide either context or language, not both");
        JsonObject body=new JsonObject();body.addProperty("code",code);
        if(opts.getLanguage()!=null)body.addProperty("language",opts.getLanguage());
        if(opts.getContextId()!=null)body.addProperty("context_id",opts.getContextId());
        if(opts.getEnvironment()!=null)body.add("env_vars",GSON.toJsonTree(opts.getEnvironment()));
        int requestTimeout=opts.getRequestTimeoutSeconds()==null?sandbox.getRequestTimeoutSeconds():opts.getRequestTimeoutSeconds();
        OkHttpClient http=sandbox.httpClient().newBuilder().callTimeout(opts.getTimeout(),TimeUnit.SECONDS)
                .readTimeout(requestTimeout,TimeUnit.SECONDS).writeTimeout(requestTimeout,TimeUnit.SECONDS).connectTimeout(requestTimeout,TimeUnit.SECONDS).build();
        Request req=request("/execute").post(RequestBody.create(GSON.toJson(body),JSON)).build();
        try(Response response=AsyncSupport.execute(http.newCall(req))){
            check(response);if(response.body()==null)throw new SandboxException("Code execution returned no response body");
            response.body().source().timeout().clearTimeout();
            ExecutionResult result=new ExecutionResult();String line;
            while((line=response.body().source().readUtf8Line())!=null){AsyncSupport.throwIfCancelled();if(!line.trim().isEmpty())consumeMessage(result,line,opts);}
            return result;
        }catch(java.io.InterruptedIOException e){throw new ai.novita.sandbox.core.TimeoutException("Code execution timed out: "+e.getMessage());}
        catch(IOException e){throw AsyncSupport.transportFailure("Code execution request failed",e);}
        catch(JsonParseException|IllegalStateException|NumberFormatException e){throw new SandboxException("Invalid code execution response",e);}
    }
    ExecutionResult runCodeInContext(String code,String id)throws SandboxException{return runCode(code,new RunCodeOpts().setContextId(id));}
    private static void consumeMessage(ExecutionResult out,String line){consumeMessage(out,line,new RunCodeOpts());}
    private static void consumeMessage(ExecutionResult out,String line,RunCodeOpts opts){
        JsonObject message=JsonParser.parseString(line).getAsJsonObject();
        String type=value(message,"type");
        if(type==null)throw new JsonParseException("Execution message is missing its type");
        switch(type){
            case "stdout": {String text=requiredValue(message,"text");out.setStdout(out.getStdout()+text);if(opts.getOnStdout()!=null)opts.getOnStdout().accept(text);break;}
            case "stderr": {String text=requiredValue(message,"text");out.setStderr(out.getStderr()+text);if(opts.getOnStderr()!=null)opts.getOnStderr().accept(text);break;}
            case "result": {
                boolean main=message.has("is_main_result")&&message.get("is_main_result").getAsBoolean();
                JsonObject data=message.deepCopy();data.remove("type");data.remove("is_main_result");
                java.lang.reflect.Type mapType=new com.google.gson.reflect.TypeToken<Map<String,Object>>(){}.getType();
                CodeResult result=new CodeResult(GSON.fromJson(data,mapType),main);out.addResult(result);if(opts.getOnResult()!=null)opts.getOnResult().accept(result);break;
            }
            case "error": {ExecutionError error=new ExecutionError(value(message,"name"),value(message,"value"),value(message,"traceback"));out.setExecutionError(error);if(opts.getOnError()!=null)opts.getOnError().accept(error);break;}
            case "number_of_executions":out.setExecutionCount(Integer.parseInt(requiredValue(message,"execution_count")));break;
            default:break;
        }
        out.addMessage(line);
    }
    private static String requiredValue(JsonObject message,String key){String value=value(message,key);if(value==null)throw new JsonParseException("Execution message is missing "+key);return value;}
    private static String value(JsonObject obj,String key){return !obj.has(key)||obj.get(key).isJsonNull()?null:obj.get(key).getAsString();}
    private static void check(Response response)throws IOException,SandboxException{
        if(response.isSuccessful())return;
        String body=response.body()==null?response.message():response.body().string();
        String message="Jupyter HTTP "+response.code()+": "+body;
        switch(response.code()){
            case 401:throw new AuthenticationException(message);
            case 403:throw new ForbiddenException(message);
            case 404:throw new NotFoundException(message);
            case 408:case 502:case 504:throw new ai.novita.sandbox.core.TimeoutException(message);
            case 429:throw new RateLimitException(message);
            default:throw new SandboxException(message);
        }
    }
    public ExecutionContext createContext()throws SandboxException{return createContext(null,null);}
    public ExecutionContext createContext(String language,String cwd)throws SandboxException{
        JsonObject body=new JsonObject();if(language!=null)body.addProperty("language",language);if(cwd!=null)body.addProperty("cwd",cwd);
        try(Response response=AsyncSupport.execute(sandbox.httpClient().newCall(request("/contexts").post(RequestBody.create(body.toString(),JSON)).build()))){
            check(response);CodeContext info=GSON.fromJson(response.body().string(),CodeContext.class);
            ExecutionContext context=new ExecutionContext(this);context.setId(info.getId());context.setLanguage(info.getLanguage());context.setCwd(info.getCwd());return context;
        }catch(IOException e){throw AsyncSupport.transportFailure("Context creation failed",e);}
    }
    public CodeContext[] listContexts()throws SandboxException{
        try(Response response=AsyncSupport.execute(sandbox.httpClient().newCall(request("/contexts").get().build()))){
            check(response);JsonElement data=JsonParser.parseString(response.body().string());
            JsonElement contexts=data.isJsonArray()?data:data.getAsJsonObject().get("contexts");return contexts==null?new CodeContext[0]:GSON.fromJson(contexts,CodeContext[].class);
        }catch(IOException e){throw AsyncSupport.transportFailure("Context list failed",e);}
    }
    public void removeContext(String id)throws SandboxException{contextRequest("/contexts/"+enc(id),false);}
    public void restartContext(String id)throws SandboxException{contextRequest("/contexts/"+enc(id)+"/restart",true);}
    public void deleteContext(String id)throws SandboxException{removeContext(id);}
    private void contextRequest(String path,boolean restart)throws SandboxException{
        Request.Builder builder=request(path);if(restart)builder.post(RequestBody.create(new byte[0],null));else builder.delete();
        try(Response response=AsyncSupport.execute(sandbox.httpClient().newCall(builder.build()))){check(response);}catch(IOException e){throw AsyncSupport.transportFailure("Context operation failed",e);}
    }
    public CompletableFuture<ExecutionResult> runCodeAsync(String code,RunCodeOpts opts){return AsyncSupport.supplyAsync(()->{try{return runCode(code,opts);}catch(SandboxException e){throw new CompletionException(e);}});}
    public CompletableFuture<ExecutionContext> createContextAsync(String language,String cwd){return AsyncSupport.supplyAsync(()->{try{return createContext(language,cwd);}catch(SandboxException e){throw new CompletionException(e);}});}
    public CompletableFuture<CodeContext[]> listContextsAsync(){return AsyncSupport.supplyAsync(()->{try{return listContexts();}catch(SandboxException e){throw new CompletionException(e);}});}
    public CompletableFuture<ExecutionResult> runCodeAsync(String code){return runCodeAsync(code,null);}
    public CompletableFuture<ExecutionContext> createContextAsync(){return createContextAsync(null,null);}
    public CompletableFuture<Void> removeContextAsync(String id){return AsyncSupport.runAsync(()->{try{removeContext(id);}catch(SandboxException e){throw new CompletionException(e);}});}
    public CompletableFuture<Void> restartContextAsync(String id){return AsyncSupport.runAsync(()->{try{restartContext(id);}catch(SandboxException e){throw new CompletionException(e);}});}
    public void kill()throws SandboxException{sandbox.kill();}
    @Override public void close()throws SandboxException{kill();}
}
