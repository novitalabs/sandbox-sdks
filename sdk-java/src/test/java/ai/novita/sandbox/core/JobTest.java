package ai.novita.sandbox.core;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.*;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.*;
import static org.junit.jupiter.api.Assertions.*;

class JobTest {
    MockWebServer server;
    Novita client;
    @BeforeEach void setup() throws Exception {
        server = new MockWebServer(); server.start();
        SandboxOpts opts = new SandboxOpts(); opts.setApiKey("fixture");
        opts.setDomain("us-phx-1.sandbox.novita.ai"); opts.setBaseUrl(server.url("/").newBuilder().host("127.0.0.1").build().toString());
        client = Novita.create(opts);
    }
    @AfterEach void close() throws Exception { client.close(); server.shutdown(); }
    void enqueue(String json) { server.enqueue(new MockResponse().setHeader("Content-Type","application/json").setBody(json)); }
    JsonObject body(RecordedRequest r) { return JsonParser.parseString(r.getBody().readUtf8()).getAsJsonObject(); }

    @Test void createDeduplicatesAndUsesOptionalSandboxAliases() throws Exception {
        enqueue("{\"jobId\":\"j\"}");
        assertEquals("j",client.job().migrateSandboxes(Arrays.asList("a","a","b"),new JobCreateOptions().setSourceRegion("s")));
        RecordedRequest r=server.takeRequest();
        assertEquals("/v1/migrations",r.getPath()); assertEquals("fixture",r.getHeader("X-API-Key"));
        assertEquals(JsonParser.parseString("{\"resource_type\":\"sandbox\",\"source_region\":\"s\",\"resources\":[{\"id\":\"a\"},{\"id\":\"b\"}]}"),body(r));
        enqueue("{\"jobId\":\"j2\"}");
        client.jobs().distributeTemplates(Arrays.asList("a","b"),new JobCreateOptions().setSourceRegion("s").setAliasForResource(id -> " "+id+" "));
        assertEquals(" a ",body(server.takeRequest()).getAsJsonArray("resources").get(0).getAsJsonObject().get("template_alias").getAsString());
        assertThrows(InvalidArgumentException.class,()->client.job().distributeTemplates(Arrays.asList("a","b"),new JobCreateOptions().setSourceRegion("s").setTemplateAlias("same")));
        assertThrows(InvalidArgumentException.class,()->client.job().distributeTemplate("a",new JobCreateOptions().setSourceRegion("s")));
        assertThrows(InvalidArgumentException.class,()->client.job().migrateSandbox("a",new JobCreateOptions().setSourceRegion("s").setTemplateAlias(" \t")));
        assertEquals(2,server.getRequestCount());
    }
    @Test void getListCancelUseContractRoutesAndKeepUnknownStatuses() throws Exception {
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"future\",\"total\":2}]}");
        MigrationStatus s=client.job().get("a/b ?"); assertEquals("future",s.getStatus()); assertEquals(2,s.getTotal()); assertTrue(s.getItems().isEmpty());
        assertEquals("a/b ?",server.takeRequest().getRequestUrl().queryParameter("job_id"));
        enqueue("{\"items\":[{\"jobId\":\"j\"}],\"nextCursor\":\"next\"}");
        JobPage page=client.job().list(new JobListOptions().setSourceRegion("s").setTargetRegion("t").setLimit(0).setType(JobType.CUSTOMER).setStatus(JobStatus.REVERTING).setResourceId("r").setCursor("cursor"));
        RecordedRequest r=server.takeRequest(); assertEquals("s",r.getRequestUrl().queryParameter("source_region")); assertEquals("t",r.getRequestUrl().queryParameter("target_region"));assertEquals("0",r.getRequestUrl().queryParameter("limit"));assertEquals("customer",r.getRequestUrl().queryParameter("type"));assertEquals("reverting",r.getRequestUrl().queryParameter("status"));assertEquals("r",r.getRequestUrl().queryParameter("resource_id"));assertEquals("cursor",r.getRequestUrl().queryParameter("cursor"));assertEquals("next",page.getNextCursor());
        enqueue("{\"jobId\":\"j\",\"status\":\"reverting\",\"items\":[{\"itemId\":\"i\",\"resourceType\":\"base\",\"resourceId\":\"tpl\",\"status\":\"future\"}]}");
        s=client.job().cancel("j");assertEquals("future",s.getItems().get(0).getStatus());r=server.takeRequest();assertEquals("/v1/migrations/cancel",r.getPath());assertEquals("j",body(r).get("job_id").getAsString());
    }
    @Test void reasonTakesPriorityOverHttpStatus() throws Exception {
        Object[][] cases={{400,"RESOURCE_NOT_FOUND",NotFoundException.class},{400,"MIGRATION_TEMPLATE_ALIAS_CONFLICT",TemplateAliasConflictException.class},{409,"MIGRATION_RESOURCE_ALREADY_MIGRATING",ResourceAlreadyMigratingException.class},{409,"MIGRATION_PAST_CANCEL_LINE",CancelTooLateException.class},{409,"MIGRATION_SHARED_DEPENDENCY",SharedDependencyException.class},{409,"FUTURE",MigrationConflictException.class},{501,"",MigrationNotSupportedException.class},{400,"VALIDATOR_PARAM",InvalidArgumentException.class},{401,"",AuthenticationException.class},{429,"",RateLimitException.class}};
        for(Object[] c:cases){server.enqueue(new MockResponse().setResponseCode((int)c[0]).setBody("{\"reason\":\""+c[1]+"\",\"message\":\"detail\"}"));
            SandboxException ex=assertThrows(SandboxException.class,()->client.job().get("j"));assertEquals(c[2],ex.getClass());
            if("FUTURE".equals(c[1]))assertEquals("FUTURE",((MigrationConflictException)ex).getReason());}
    }
    @Test void waitPollsRollbackAndReportsTerminalDetails() throws Exception {
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"reverting\"}]}");
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"completed\",\"failed\":1}]}");
        List<String> progress=new ArrayList<>();
        MigrationStatus result=client.job().waitFor("j",new JobWaitOptions().setPollInterval(Duration.ofMillis(1)).setOnProgress(s -> progress.add(s.getStatus())));
        assertEquals(1,result.getFailed());assertEquals(Arrays.asList("reverting","completed"),progress);
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"failed\",\"items\":[{\"itemId\":\"i\"}]}]}");
        JobFailedException failed=assertThrows(JobFailedException.class,()->client.job().waitFor("j"));assertEquals("i",failed.getStatus().getItems().get(0).getItemId());
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"cancelled\"}]}");assertThrows(JobCancelledException.class,()->client.job().waitFor("j"));
    }
    @Test void waitBudgetIncludesRequestAndCallback() throws Exception {
        server.enqueue(new MockResponse().setBody("{\"items\":[]}").setBodyDelay(2,TimeUnit.SECONDS));
        long start=System.nanoTime();assertThrows(TimeoutException.class,()->client.job().waitFor("j",new JobWaitOptions().setTimeout(Duration.ofMillis(60))));assertTrue(System.nanoTime()-start<TimeUnit.SECONDS.toNanos(1));
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"completed\"}]}");
        assertThrows(TimeoutException.class,()->client.job().waitFor("j",new JobWaitOptions().setTimeout(Duration.ofMillis(80)).setOnProgress(s->{try{Thread.sleep(100);}catch(InterruptedException e){Thread.currentThread().interrupt();}})));
        int before=server.getRequestCount();assertThrows(TimeoutException.class,()->client.job().waitFor("j",new JobWaitOptions().setTimeout(Duration.ZERO)));assertEquals(before,server.getRequestCount());
    }
    @Test void asyncFacadeAndStandaloneClientWork() throws Exception {
        enqueue("{\"jobId\":\"async\"}");assertEquals("async",client.job().migrateSandboxAsync("a",new JobCreateOptions().setSourceRegion("s")).get(1,TimeUnit.SECONDS));
        enqueue("{\"items\":[{\"jobId\":\"j\"}]}");SandboxOpts opts=new SandboxOpts();opts.setApiKey("fixture");opts.setBaseUrl(server.url("/").newBuilder().host("127.0.0.1").build().toString());opts.setDomain("modern.example");
        try(Job job=Job.create(opts)){assertEquals("j",job.getAsync("j").get(1,TimeUnit.SECONDS).getJobId());}
    }
    @Test void acceptFailureIsNotAutomaticallyReplayed() throws Exception {
        enqueue("{\"items\":[]}");client.job().list();
        server.enqueue(new MockResponse().setResponseCode(503).setHeader("Retry-After","0").setBody("{\"message\":\"unavailable\"}"));
        enqueue("{\"jobId\":\"replayed\"}");
        assertThrows(SandboxException.class,()->client.job().migrateSandbox("a",new JobCreateOptions().setSourceRegion("s")));
        assertEquals(2,server.getRequestCount());
    }
    @Test void compressedErrorsKeepReasonMapping() throws Exception {
        for(String reason:Arrays.asList("RESOURCE_NOT_FOUND","MIGRATION_PAST_CANCEL_LINE")){
            java.io.ByteArrayOutputStream bytes=new java.io.ByteArrayOutputStream();
            try(java.util.zip.GZIPOutputStream gzip=new java.util.zip.GZIPOutputStream(bytes)){gzip.write(("{\"reason\":\""+reason+"\"}").getBytes(java.nio.charset.StandardCharsets.UTF_8));}
            server.enqueue(new MockResponse().setResponseCode(400).setHeader("Content-Encoding","gzip").setBody(new okio.Buffer().write(bytes.toByteArray())));
            SandboxException error=assertThrows(SandboxException.class,()->client.job().get("j"));
            assertEquals("RESOURCE_NOT_FOUND".equals(reason)?NotFoundException.class:CancelTooLateException.class,error.getClass());
        }
    }

    @Test void asyncCancellationReachesActiveHttpCallAndSharedCloseKeepsOwnerOpen() throws Exception {
        SandboxOpts opts=new SandboxOpts();opts.setAccessToken("token");opts.setDomain("modern.example");
        opts.setBaseUrl(server.url("/").newBuilder().host("127.0.0.1").build().toString());
        opts.setHeaders(Collections.singletonMap("X-Custom","custom"));
        try(ApiClient api=new ApiClient(opts)){
            Job job=new Job(api);
            job.close();
            assertFalse(api.httpClient().dispatcher().executorService().isShutdown());
            server.enqueue(new MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE));
            CompletableFuture<MigrationStatus> result=job.waitForAsync("j");
            RecordedRequest request=server.takeRequest(1,TimeUnit.SECONDS);
            assertNotNull(request);assertEquals("Bearer token",request.getHeader("Authorization"));
            assertEquals("custom",request.getHeader("X-Custom"));
            List<okhttp3.Call> active=api.httpClient().dispatcher().runningCalls();
            assertEquals(1,active.size());
            assertTrue(result.cancel(true));assertTrue(active.get(0).isCanceled());
            assertThrows(CancellationException.class,result::join);
        }
    }

    @Test void waitClipsLongPollingDelayWithoutAnotherRequest() throws Exception {
        enqueue("{\"items\":[{\"jobId\":\"j\",\"status\":\"migrating\"}]}");
        long start=System.nanoTime();
        assertThrows(TimeoutException.class,()->client.job().waitFor("j",new JobWaitOptions()
            .setTimeout(Duration.ofMillis(100)).setPollInterval(Duration.ofMinutes(1))));
        assertTrue(System.nanoTime()-start<TimeUnit.SECONDS.toNanos(1));
        assertEquals(1,server.getRequestCount());
    }
}
