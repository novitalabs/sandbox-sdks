package ai.novita.sandbox.core;

import ai.novita.sandbox.code_interpreter.*;
import ai.novita.sandbox.desktop.*;
import com.google.gson.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.*;
import java.io.*;
import java.util.*;
import java.util.concurrent.*;
import static org.junit.jupiter.api.Assertions.*;

/** Public-entry-point coverage for shared runtime capabilities. All traffic is loopback. */
class RuntimeCapabilitiesTest {
    MockWebServer server;
    final List<Sandbox> sandboxes = new ArrayList<>();
    @BeforeEach void setup() throws Exception { server = new MockWebServer(); server.start(); }
    @AfterEach void cleanup() throws Exception {
        for (Sandbox s : sandboxes) { s.rpc().close(); s.getClient().close(); }
        server.shutdown();
    }
    SandboxOptions options() {
        SandboxOptions o = new SandboxOptions().apiKey("local-placeholder").domain("modern.example").baseUrl(server.url("/").toString());
        o.setSandboxUrl(server.url("/").toString()); o.setRequestTimeoutSeconds(5); return o;
    }
    Sandbox connect() throws Exception {
        server.enqueue(json("{\"sandboxID\":\"sb\",\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"envd-local\",\"trafficAccessToken\":\"traffic-local\"}"));
        Sandbox s=Sandbox.connect("sb",options());sandboxes.add(s);server.takeRequest();return s;
    }
    static MockResponse json(String value) { return new MockResponse().setHeader("Content-Type","application/json").setBody(value); }
    @Test void fileStreamsPreserveBinaryAndCallerOwnership() throws Exception {
        Sandbox s=connect(); byte[] bytes={0,1,(byte)255,10};
        server.enqueue(new MockResponse().setBody(new okio.Buffer().write(bytes)));
        try(InputStream stream=s.files().readStream("/binary")){assertArrayEquals(bytes,stream.readAllBytes());}
        assertEquals("/files?path=%2Fbinary",server.takeRequest().getPath());
        class OwnedInput extends ByteArrayInputStream {boolean closed;OwnedInput(){super(bytes);}@Override public void close(){closed=true;}}
        OwnedInput input=new OwnedInput();server.enqueue(json("[{\"path\":\"/binary\",\"name\":\"binary\"}]"));
        s.files().writeStream("/binary",input,new FilesystemWriteOptions().setUseOctetStream(true));
        assertFalse(input.closed);RecordedRequest req=server.takeRequest();assertArrayEquals(bytes,req.getBody().readByteArray());
        assertEquals("application/octet-stream",req.getHeader("Content-Type"));
    }
    @Test void filesystemUnaryHonorsRequestTimeout() throws Exception {
        Sandbox s=connect(); server.enqueue(json("{\"entry\":{\"path\":\"/slow\"}}").setHeadersDelay(2,TimeUnit.SECONDS));
        assertThrows(ai.novita.sandbox.core.TimeoutException.class,()->s.files().stat("/slow",new FilesystemRequestOptions().setRequestTimeoutSeconds(1)));
    }
    @Test void persistentWatchOperationsCarryUser() throws Exception {
        Sandbox s=connect();FilesystemRequestOptions o=new FilesystemRequestOptions().setUser("root");
        server.enqueue(json("{\"watcherId\":\"watch-1\"}"));server.enqueue(json("{\"events\":[]}"));server.enqueue(json("{}"));
        assertEquals("watch-1",s.files().createWatcher("/tmp",true,o));s.files().getWatcherEvents("watch-1",o);s.files().removeWatcher("watch-1",o);
        for(int i=0;i<3;i++)assertEquals("Basic "+Base64.getEncoder().encodeToString("root:".getBytes(java.nio.charset.StandardCharsets.UTF_8)),server.takeRequest().getHeader("Authorization"));
    }
    @Test void existsPropagatesAuthorizationFailures() throws Exception {
        Sandbox s=connect();server.enqueue(json("{\"code\":\"permission_denied\",\"message\":\"denied\"}").setResponseCode(403));
        assertThrows(ConnectRpcException.class,()->s.files().exists("/private"));
    }
    @Test void codeExecutionKeepsRichResultsAndContextOptions() throws Exception {
        Sandbox s=connect();CodeInterpreterSandbox ci=CodeInterpreterSandbox.wrap(s);
        server.enqueue(json("{\"id\":\"ctx\",\"language\":\"python\",\"cwd\":\"/work\"}"));
        ExecutionContext context=ci.createContext("python","/work");RecordedRequest create=server.takeRequest();
        assertEquals("envd-local",create.getHeader("X-Access-Token"));assertTrue(create.getBody().readUtf8().contains("/work"));
        List<String> output=new ArrayList<>();List<CodeResult> results=new ArrayList<>();List<ExecutionError> errors=new ArrayList<>();
        RunCodeOpts opts=new RunCodeOpts().setOnStdout(output::add).setOnResult(results::add).setOnError(errors::add);
        server.enqueue(json("{\"type\":\"stdout\",\"text\":\"hello\"}\n{\"type\":\"result\",\"text\":\"42\",\"html\":\"<b>42</b>\",\"chart\":{\"type\":\"line\",\"title\":\"Trend\",\"elements\":[]},\"is_main_result\":true}\n{\"type\":\"number_of_executions\",\"execution_count\":7}\n{\"type\":\"error\",\"name\":\"ValueError\",\"value\":\"bad\",\"traceback\":\"trace\"}\n"));
        ExecutionResult result=context.execute("42",opts);
        assertNull(opts.getContextId(),"context execution must not mutate caller options");assertEquals(7,result.getExecutionCount());assertEquals("42",result.getText());
        assertEquals(Collections.singletonList("hello"),output);assertEquals("<b>42</b>",results.get(0).getHtml());assertEquals("ValueError",errors.get(0).getName());
        assertEquals("Trend",results.get(0).getChart().getTitle());assertEquals(Chart.Type.LINE,results.get(0).getChart().getType());
        JsonObject request=JsonParser.parseString(server.takeRequest().getBody().readUtf8()).getAsJsonObject();assertEquals("ctx",request.get("context_id").getAsString());assertFalse(request.has("language"));
    }
    @Test void codeCallbacksArriveBeforeStreamCompletion() throws Exception {
        Sandbox s=connect();CountDownLatch output=new CountDownLatch(1);
        String first="{\"type\":\"stdout\",\"text\":\"first\"}\n";
        server.enqueue(json(first+"{\"type\":\"stdout\",\"text\":\"last\"}\n").throttleBody(first.getBytes(java.nio.charset.StandardCharsets.UTF_8).length,2,TimeUnit.SECONDS));
        CompletableFuture<ExecutionResult> future=CodeInterpreterSandbox.wrap(s).runCodeAsync("x",new RunCodeOpts().setOnStdout(v->output.countDown()));
        assertTrue(output.await(1,TimeUnit.SECONDS));assertFalse(future.isDone());assertEquals("firstlast",future.get(4,TimeUnit.SECONDS).getStdout());
    }
    @Test void asyncCancellationCancelsUnderlyingHttpRequest() throws Exception {
        Sandbox s=connect();server.enqueue(new MockResponse().setSocketPolicy(SocketPolicy.NO_RESPONSE));
        CompletableFuture<ExecutionResult> future=CodeInterpreterSandbox.wrap(s).runCodeAsync("sleep()",new RunCodeOpts().setTimeout(0));
        assertNotNull(server.takeRequest(2,TimeUnit.SECONDS));assertTrue(future.cancel(true));
        long deadline=System.nanoTime()+TimeUnit.SECONDS.toNanos(2);
        while(s.httpClient().dispatcher().runningCallsCount()!=0&&System.nanoTime()<deadline)Thread.sleep(10);
        assertEquals(0,s.httpClient().dispatcher().runningCallsCount(),"cancel must release the actual HTTP call");assertTrue(future.isCancelled());
    }
    @Test void facadeTraversesPagesAndPreservesFilters() throws Exception {
        try(Novita sdk=Novita.create(options())) {
            server.enqueue(json("[{\"sandboxID\":\"first\"}]").setHeader("x-next-token","next"));server.enqueue(json("[{\"sandboxID\":\"second\"}]"));
            SandboxListOptions query=new SandboxListOptions().setStates(Collections.singletonList("running")).setLimit(1);
            SandboxInfo[] all=sdk.listAllSandboxes(query);assertEquals(2,all.length);assertNull(query.getNextToken());
            assertEquals("running",server.takeRequest().getRequestUrl().queryParameter("state"));RecordedRequest next=server.takeRequest();assertEquals("next",next.getRequestUrl().queryParameter("nextToken"));assertEquals("1",next.getRequestUrl().queryParameter("limit"));
        }
    }
    @Test void tokenOnlyAuthenticationAndCloneInheritConnectionOptions() throws Exception {
        SandboxOptions opts=options();opts.setApiKey(null);opts.setAccessToken("token-local");opts.setHeaders(Collections.singletonMap("X-Customer","customer"));opts.setRequestTimeoutSeconds(7);
        server.enqueue(json("{\"sandboxes\":[{\"sandboxID\":\"clone\",\"envdVersion\":\"0.5.7\"}]}"));
        Sandbox clone=Sandbox.clone("source",new CloneOptions(),opts).getSandboxes()[0];sandboxes.add(clone);
        RecordedRequest request=server.takeRequest();assertEquals("Bearer token-local",request.getHeader("Authorization"));assertEquals("customer",request.getHeader("X-Customer"));
        assertEquals(7,clone.getRequestTimeoutSeconds());assertEquals(server.url("/").toString().replaceAll("/+$",""),clone.portUrl(1234));
        server.enqueue(json("{\"sandboxID\":\"clone\"}"));clone.info();request=server.takeRequest();assertEquals("Bearer token-local",request.getHeader("Authorization"));assertEquals("customer",request.getHeader("X-Customer"));
    }
    @Test void legacySecretsFailBeforeNetwork() throws Exception {
        SandboxOptions opts=options().domain("sandbox.novita.ai");
        try(Novita sdk=Novita.create(opts)){assertThrows(SecretException.class,()->sdk.secrets().list());assertNull(server.takeRequest(50,TimeUnit.MILLISECONDS));}
    }
    @Test void desktopConnectPreservesDisplayWithoutStartingServer() throws Exception {
        DesktopOptions opts=new DesktopOptions().setDisplay(":7").setResolution(1440,900).setDpi(110);opts.setApiKey("local-placeholder");opts.setBaseUrl(server.url("/").toString());opts.setSandboxUrl(server.url("/").toString());
        server.enqueue(json("{\"sandboxID\":\"desktop\",\"envdVersion\":\"0.5.7\"}"));
        DesktopSandbox desktop=DesktopSandbox.connect("desktop",opts);sandboxes.add(desktop.getSandbox());assertEquals(":7",desktop.getDisplay());server.takeRequest();
        assertNull(server.takeRequest(50,TimeUnit.MILLISECONDS));
        server.enqueue(RuntimeParityTest.process(1));desktop.focusWindow("123");assertTrue(RuntimeParityTest.command(server.takeRequest()).contains("windowactivate --sync '123'"));
        String url=desktop.getStream().getUrl(new VncUrlOptions().setAutoConnect(false).setViewOnly(true).setResize("remote").setAuthKey("a&b"));
        okhttp3.HttpUrl parsed=okhttp3.HttpUrl.get(url);assertNull(parsed.queryParameter("autoconnect"));assertEquals("true",parsed.queryParameter("view_only"));assertEquals("a&b",parsed.queryParameter("password"));
    }
    @Test void commandReconnectWaitsForStartAndSupportsOutputAndUser() throws Exception {
        Sandbox s=connect();
        okio.Buffer body=RuntimeParityTest.frame(0,"{\"event\":{\"start\":{\"pid\":42}}}");
        body.writeAll(RuntimeParityTest.frame(0,"{\"event\":{\"data\":{\"stdout\":\"aGVsbG8=\"}}}"));
        body.writeAll(RuntimeParityTest.frame(0,"{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"));body.writeAll(RuntimeParityTest.frame(2,"{}"));
        server.enqueue(new MockResponse().setBody(body));List<String> output=new ArrayList<>();
        CommandHandle handle=s.commands().connect(42,new ProcessOptions().setTimeoutSeconds(0).setUser("root").setOnStdout(output::add));
        assertEquals(42,handle.getPid());assertEquals("hello",handle.waitFor().getStdout());assertEquals("hello",handle.getStdout());assertEquals(Collections.singletonList("hello"),output);
        RecordedRequest request=server.takeRequest();assertEquals("Basic cm9vdDo=",request.getHeader("Authorization"));
        handle.disconnect();assertNull(server.takeRequest(50,TimeUnit.MILLISECONDS),"disconnect must not send a signal");
    }
    @Test void commandReconnectRejectsMissingProcessBeforeReturningHandle() throws Exception {
        Sandbox s=connect();server.enqueue(new MockResponse().setBody(RuntimeParityTest.frame(2,"{\"error\":{\"code\":\"not_found\",\"message\":\"gone\"}}")));
        ConnectRpcException error=assertThrows(ConnectRpcException.class,()->s.commands().connect(404));assertEquals("not_found",error.getCode());
    }
    @Test void filesystemWatchHonorsStartupDeadline() throws Exception {
        Sandbox s=connect();server.enqueue(new MockResponse().setBody(RuntimeParityTest.frame(0,"{\"event\":{\"start\":{}}}")).setBodyDelay(2,TimeUnit.SECONDS));
        assertThrows(ai.novita.sandbox.core.TimeoutException.class,()->s.files().watch("/tmp",false,event->{},new FilesystemRequestOptions().setRequestTimeoutSeconds(1)));
    }
    @Test void desktopCreationPassesDisplayAndResolutionWithoutMutatingOptions() throws Exception {
        DesktopOptions opts=new DesktopOptions().setDisplay(":8").setResolution(1600,1000).setDpi(120);
        opts.setApiKey("local-placeholder");opts.setBaseUrl(server.url("/").toString());opts.setSandboxUrl(server.url("/").toString());opts.setEnvVars(Collections.singletonMap("APP","test"));
        server.enqueue(json("{\"sandboxID\":\"desktop\",\"envdVersion\":\"0.5.7\"}"));
        server.enqueue(RuntimeParityTest.process(1));server.enqueue(RuntimeParityTest.process(2));server.enqueue(RuntimeParityTest.process(3));
        DesktopSandbox desktop=DesktopSandbox.create("desktop",opts);sandboxes.add(desktop.getSandbox());
        JsonObject create=JsonParser.parseString(server.takeRequest().getBody().readUtf8()).getAsJsonObject();assertEquals(":8",create.getAsJsonObject("envVars").get("DISPLAY").getAsString());assertFalse(opts.getEnvVars().containsKey("DISPLAY"));
        assertTrue(RuntimeParityTest.command(server.takeRequest()).contains("Xvfb :8 -ac -screen 0 1600x1000x24 -retro -dpi 120"));server.takeRequest();assertEquals("startxfce4",RuntimeParityTest.command(server.takeRequest()));
    }
    @Test void reconnectRefreshesRuntimeAndTrafficTokens() throws Exception {
        Sandbox s=connect();server.enqueue(json("{\"entry\":{\"path\":\"/first\"}}"));s.files().stat("/first");assertEquals("envd-local",server.takeRequest().getHeader("X-Access-Token"));
        server.enqueue(json("{\"sandboxID\":\"sb\",\"envdVersion\":\"0.5.8\",\"envdAccessToken\":\"rotated-envd\",\"trafficAccessToken\":\"rotated-traffic\"}"));s.reconnect();server.takeRequest();
        server.enqueue(json("{\"entry\":{\"path\":\"/second\"}}"));s.files().stat("/second");assertEquals("rotated-envd",server.takeRequest().getHeader("X-Access-Token"));assertEquals("rotated-traffic",s.portHeaders().get("E2B-Traffic-Access-Token"));
    }
    @Test void controlPlaneUsesTypedTimeout() throws Exception {
        SandboxOptions opts=options().requestTimeoutSeconds(1);server.enqueue(json("[]").setHeadersDelay(2,TimeUnit.SECONDS));
        try(Novita sdk=Novita.create(opts)){assertThrows(ai.novita.sandbox.core.TimeoutException.class,()->sdk.listSandboxes());}
    }
    @Test void requestLoggerCoversControlAndRuntimeWithoutCredentials() throws Exception {
        List<String> messages=new ArrayList<>();SandboxOptions opts=options();opts.setLogger(messages::add);
        server.enqueue(json("{\"sandboxID\":\"sb\",\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"envd-local\"}"));Sandbox s=Sandbox.connect("sb",opts);sandboxes.add(s);
        server.enqueue(json("{\"entry\":{\"path\":\"/tmp/test\"}}"));s.files().stat("/tmp/test");
        assertEquals(Arrays.asList("POST /sandboxes/sb/connect","POST /filesystem.Filesystem/Stat"),messages);
        assertFalse(String.join(" ",messages).contains("local-placeholder"));assertFalse(String.join(" ",messages).contains("envd-local"));
    }
    @Test void legacyCreateSupportsPublicTrafficOnlyNetworkConfiguration() throws Exception {
        SandboxOptions opts=options().domain("sandbox.novita.ai").network(new NetworkOptions().setAllowPublicTraffic(false));
        server.enqueue(json("{\"sandboxID\":\"legacy\",\"envdVersion\":\"0.5.7\"}"));Sandbox s=Sandbox.create("base",opts);sandboxes.add(s);
        JsonObject body=JsonParser.parseString(server.takeRequest().getBody().readUtf8()).getAsJsonObject();
        assertEquals(JsonParser.parseString("{\"allowPublicTraffic\":false}"),body.get("network"));
    }
    @Test void legacyCreateStillRejectsModernNetworkRulesBeforeRequest() throws Exception {
        for(NetworkOptions network:Arrays.asList(new NetworkOptions().setAllowOut(Collections.emptyList()),new NetworkOptions().setDenyOut(Collections.singletonList("example.com")),new NetworkOptions().setMaskRequestHost("example.com"))){
            SandboxOptions opts=options().domain("sandbox.novita.ai").network(network.setAllowPublicTraffic(false));
            assertThrows(SandboxException.class,()->Sandbox.create("base",opts));
        }
        assertNull(server.takeRequest(50,TimeUnit.MILLISECONDS));
    }
}
