package ai.novita.sandbox.core;

import com.google.gson.JsonObject;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.CompletableFuture;
import java.util.concurrent.CompletionException;

/** Cross-cluster migration. The configured domain/base URL selects the target region. */
public final class Job implements AutoCloseable {
    private final ApiClient client;
    private final boolean ownsClient;
    Job(ApiClient client) { this(client,false); }
    private Job(ApiClient client,boolean ownsClient) { this.client=client; this.ownsClient=ownsClient; }
    public static Job create(SandboxOpts options) throws SandboxException { return new Job(new ApiClient(options==null?new SandboxOpts():options),true); }
    public static Job create() throws SandboxException { return create(null); }
    @Override public void close() { if(ownsClient)client.close(); }

    public String migrateSandbox(String id,JobCreateOptions options) throws SandboxException { return accept("sandbox",Collections.singletonList(id),options); }
    public String migrateSandboxes(List<String> ids,JobCreateOptions options) throws SandboxException { return accept("sandbox",ids,options); }
    public String distributeTemplate(String id,JobCreateOptions options) throws SandboxException { return accept("template",Collections.singletonList(id),options); }
    public String distributeTemplates(List<String> ids,JobCreateOptions options) throws SandboxException { return accept("template",ids,options); }

    private String accept(String type,List<String> ids,JobCreateOptions options) throws SandboxException {
        if(options==null)throw new InvalidArgumentException("sourceRegion is required");
        String source=requireId(options.getSourceRegion(),"sourceRegion");
        if(ids==null||ids.isEmpty())throw new InvalidArgumentException("at least one resource id is required");
        Set<String> unique=new LinkedHashSet<>();for(String id:ids)unique.add(requireId(id,"resource id"));
        if(options.getTemplateAlias()!=null&&options.getAliasForResource()!=null)throw new InvalidArgumentException("set only one alias option");
        if("template".equals(type)&&unique.size()>1&&options.getTemplateAlias()!=null)throw new InvalidArgumentException("multiple templates require aliasForResource");
        List<Map<String,String>> resources=new ArrayList<>();
        for(String id:unique){
            Map<String,String> resource=new LinkedHashMap<>();resource.put("id",id);
            if("template".equals(type)||options.getTemplateAlias()!=null||options.getAliasForResource()!=null){
                String alias=options.getAliasForResource()!=null?options.getAliasForResource().apply(id):options.getTemplateAlias();
                if(alias==null||alias.isBlank())throw new InvalidArgumentException("template alias must not be blank for resource "+id);
                resource.put("template_alias",alias);
            }
            resources.add(resource);
        }
        Map<String,Object> body=new LinkedHashMap<>();body.put("resources",resources);body.put("resource_type",type);body.put("source_region",source);
        JsonObject result=request("POST","",null,body,JsonObject.class,0);
        if(result==null||!result.has("jobId")||!result.get("jobId").isJsonPrimitive()||!result.getAsJsonPrimitive("jobId").isString()||result.get("jobId").getAsString().isEmpty())throw new SandboxException("migration accept response is missing jobId");
        return result.get("jobId").getAsString();
    }
    public MigrationStatus get(String id) throws SandboxException { return get(id,0); }
    private MigrationStatus get(String id,long remainingNanos) throws SandboxException {
        JobPage page=request("GET","",Collections.singletonMap("job_id",requireId(id,"jobId")),null,JobPage.class,remainingNanos);
        if(page==null||page.getItems().isEmpty())throw new NotFoundException("RESOURCE_NOT_FOUND - no such job "+id);
        return page.getItems().get(0);
    }
    public JobPage list() throws SandboxException { return list(null); }
    public JobPage list(JobListOptions options) throws SandboxException {
        Map<String,String> query=new LinkedHashMap<>();
        if(options!=null){
            put(query,"resource_id",options.getResourceId());put(query,"type",options.getType());put(query,"status",options.getStatus());
            put(query,"source_region",options.getSourceRegion());put(query,"target_region",options.getTargetRegion());
            if(options.getLimit()!=null)query.put("limit",options.getLimit().toString());
            if(options.getCursor()!=null&&!options.getCursor().isEmpty())query.put("cursor",options.getCursor());
        }
        JobPage page=request("GET","",query,null,JobPage.class,0);return page==null?new JobPage():page;
    }
    /** Accept cooperative rollback; reverting is not a terminal status. */
    public MigrationStatus cancel(String id) throws SandboxException {
        MigrationStatus status=request("POST","/cancel",null,Collections.singletonMap("job_id",requireId(id,"jobId")),MigrationStatus.class,0);
        return status==null?new MigrationStatus():status;
    }
    public MigrationStatus waitFor(String id) throws SandboxException { return waitFor(id,null); }
    public MigrationStatus waitFor(String id,JobWaitOptions options) throws SandboxException {
        requireId(id,"jobId");JobWaitOptions opts=options==null?new JobWaitOptions():options;
        Duration timeout=opts.getTimeout(),interval=opts.getPollInterval();
        if(timeout==null||timeout.isNegative()||interval==null||interval.isNegative()||interval.isZero())throw new InvalidArgumentException("timeout must be nonnegative and pollInterval positive");
        long budget,poll;
        try{budget=timeout.toNanos();poll=interval.toNanos();}catch(ArithmeticException e){throw new InvalidArgumentException("wait duration is too large");}
        long start=System.nanoTime();MigrationStatus last=null;
        for(;;){
            AsyncSupport.throwIfCancelled();
            long remaining=budget-(System.nanoTime()-start);if(remaining<=0)throw timedOut(id,last,timeout);
            try{last=get(id,remaining);}catch(SandboxException e){if(System.nanoTime()-start>=budget)throw timedOut(id,last,timeout);throw e;}
            if(System.nanoTime()-start>=budget)throw timedOut(id,last,timeout);
            if(opts.getOnProgress()!=null)opts.getOnProgress().accept(last);
            AsyncSupport.throwIfCancelled();
            if(System.nanoTime()-start>=budget)throw timedOut(id,last,timeout);
            if(JobStatus.FAILED.equals(last.getStatus()))throw new JobFailedException(last);
            if(JobStatus.CANCELLED.equals(last.getStatus()))throw new JobCancelledException(last);
            if(JobStatus.COMPLETED.equals(last.getStatus()))return last;
            long delay=Math.min(poll,budget-(System.nanoTime()-start));
            if(delay>0)try{java.util.concurrent.TimeUnit.NANOSECONDS.sleep(delay);}catch(InterruptedException e){Thread.currentThread().interrupt();throw new java.util.concurrent.CancellationException("Job wait interrupted");}
        }
    }
    private TimeoutException timedOut(String id,MigrationStatus last,Duration timeout){
        return new TimeoutException("Timed out after "+timeout+" waiting for migration job "+id+(last==null?"":"; last status was "+last.getStatus()+" (completed="+last.getCompleted()+", failed="+last.getFailed()+", cancelled="+last.getCancelled()+", total="+last.getTotal()+")"));
    }
    private <T>T request(String method,String suffix,Map<String,String> query,Object body,Class<T> type,long remainingNanos)throws SandboxException{
        if(client.isLegacyApiDomain())throw new MigrationNotSupportedException("Job requires a modern domain");
        return client.requestMigration(method,"/v1/migrations"+suffix,query,body,type,remainingNanos);
    }
    private static String requireId(String id,String name)throws InvalidArgumentException{if(id==null||id.isEmpty())throw new InvalidArgumentException(name+" is required");return id;}
    private static void put(Map<String,String> map,String key,String value){if(value!=null)map.put(key,value);}
    @FunctionalInterface private interface Operation<T>{T run()throws SandboxException;}
    private static <T>CompletableFuture<T> async(Operation<T> operation){return AsyncSupport.supplyAsync(()->{try{return operation.run();}catch(SandboxException e){throw new CompletionException(e);}});}
    public CompletableFuture<String> migrateSandboxAsync(String id,JobCreateOptions options){return async(()->migrateSandbox(id,options));}
    public CompletableFuture<String> migrateSandboxesAsync(List<String> ids,JobCreateOptions options){return async(()->migrateSandboxes(ids,options));}
    public CompletableFuture<String> distributeTemplateAsync(String id,JobCreateOptions options){return async(()->distributeTemplate(id,options));}
    public CompletableFuture<String> distributeTemplatesAsync(List<String> ids,JobCreateOptions options){return async(()->distributeTemplates(ids,options));}
    public CompletableFuture<MigrationStatus> getAsync(String id){return async(()->get(id));}
    public CompletableFuture<JobPage> listAsync(JobListOptions options){return async(()->list(options));}
    public CompletableFuture<JobPage> listAsync(){return async(this::list);}
    public CompletableFuture<MigrationStatus> cancelAsync(String id){return async(()->cancel(id));}
    public CompletableFuture<MigrationStatus> waitForAsync(String id,JobWaitOptions options){return async(()->waitFor(id,options));}
    public CompletableFuture<MigrationStatus> waitForAsync(String id){return async(()->waitFor(id));}
}
