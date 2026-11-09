package ai.novita.sandbox.core;

import ai.novita.sandbox.code_interpreter.*;
import ai.novita.sandbox.desktop.DesktopSandbox;
import com.google.gson.*;
import okhttp3.mockwebserver.*;
import okio.Buffer;
import org.junit.jupiter.api.*;
import java.lang.reflect.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.time.Duration;
import java.util.*;
import java.util.concurrent.TimeUnit;
import static org.junit.jupiter.api.Assertions.*;

/** Wire-level regressions for runtime parity with the Python and JavaScript SDKs. */
class RuntimeParityTest {
    MockWebServer server;
    final List<ApiClient> clients = new ArrayList<>();
    final List<ConnectRpcClient> rpcs = new ArrayList<>();
    @BeforeEach void setup() throws Exception { server = new MockWebServer(); server.start(); }
    @AfterEach void teardown() throws Exception { rpcs.forEach(ConnectRpcClient::close); clients.forEach(ApiClient::close); server.shutdown(); }
    ApiClient api() throws Exception { ApiClient c = new ApiClient("audit-placeholder", server.url("/").toString(), 3, "modern.example"); clients.add(c); return c; }
    SandboxInfo info() { SandboxInfo i=new SandboxInfo(); i.setId("sb-audit"); i.setDomain(server.url("/").toString()); i.setEnvdVersion("0.5.7"); i.setEnvdAccessToken("envd-placeholder"); i.setTrafficAccessToken("traffic-placeholder"); return i; }
    Sandbox sandbox() throws Exception { Sandbox s=new Sandbox("sb-audit",api(),info()); rpcs.add(s.rpc()); return s; }
    CodeInterpreterSandbox interpreter() throws Exception {
        // Only replaces URL routing so the unchanged production request/parser code uses loopback.
        Sandbox s=new Sandbox("sb-audit",api(),info()) { @Override public String portUrl(int port) { return server.url("/").toString().replaceAll("/+$", ""); } };
        Constructor<CodeInterpreterSandbox> c=CodeInterpreterSandbox.class.getDeclaredConstructor(Sandbox.class); c.setAccessible(true); return c.newInstance(s);
    }
    static Buffer frame(int flag, String json) { byte[] b=json.getBytes(StandardCharsets.UTF_8); return new Buffer().writeByte(flag).writeInt(b.length).write(b); }
    static MockResponse process(int pid) throws Exception { Buffer b=frame(0,"{\"event\":{\"start\":{\"pid\":"+pid+"}}}"); b.writeAll(frame(0,"{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}")); b.writeAll(frame(2,"{}")); return new MockResponse().setHeader("Content-Type","application/connect+json").setBody(b); }
    static JsonObject requestJson(RecordedRequest r) throws Exception { Buffer b=r.getBody(); b.readByte(); int n=b.readInt(); return JsonParser.parseString(b.readUtf8(n)).getAsJsonObject(); }
    static String command(RecordedRequest r) throws Exception { return requestJson(r).getAsJsonObject("process").getAsJsonArray("args").get(2).getAsString(); }

    @Test void codeInterpreterSendsSecurityHeaders() throws Exception {
        server.enqueue(new MockResponse().setBody("{\"type\":\"stdout\",\"text\":\"hello\"}\n"));
        interpreter().runCode("print('hello')",null);
        RecordedRequest req=server.takeRequest();
        assertAll(() -> assertEquals("envd-placeholder",req.getHeader("X-Access-Token")), () -> assertEquals("traffic-placeholder",req.getHeader("E2B-Traffic-Access-Token")));
    }
    @Test void codeInterpreterParsesRealNdjson() throws Exception {
        server.enqueue(new MockResponse().setBody("{\"type\":\"stdout\",\"text\":\"hello\\n\"}\n{\"type\":\"result\",\"text\":\"2\",\"is_main_result\":true}\n"));
        ExecutionResult result=interpreter().runCode("print('hello'); 1+1",null);
        assertAll(() -> assertEquals("hello\n",result.getStdout()), () -> assertEquals(1,result.getResults().size()));
    }
    @Test void codeInterpreterRecognizesSingleProtocolMessages() throws Exception {
        Method consume=CodeInterpreterSandbox.class.getDeclaredMethod("consumeMessage",ExecutionResult.class,String.class); consume.setAccessible(true);
        ExecutionResult result=new ExecutionResult();
        consume.invoke(null,result,"{\"type\":\"stdout\",\"text\":\"hello\"}");
        consume.invoke(null,result,"{\"type\":\"result\",\"png\":\"aW1hZ2U=\",\"is_main_result\":false}");
        consume.invoke(null,result,"{\"type\":\"error\",\"name\":\"ValueError\",\"value\":\"bad\",\"traceback\":\"trace\"}");
        assertAll(() -> assertEquals("hello",result.getStdout()), () -> assertEquals(1,result.getResults().size()), () -> assertNotNull(result.getError()));
    }
    @Test void contextExecutionDoesNotSendConflictingLanguage() throws Exception {
        server.enqueue(new MockResponse().setBody(""));
        interpreter().runCode("x",new RunCodeOpts().setContextId("ctx"));
        JsonObject body=JsonParser.parseString(server.takeRequest().getBody().readUtf8()).getAsJsonObject();
        assertFalse(body.has("language"),body.toString());
    }
    @Test void missingFileExistsReturnsFalse() throws Exception {
        server.enqueue(new MockResponse().setResponseCode(404).setBody("{\"code\":\"not_found\",\"message\":\"missing\"}"));
        assertFalse(sandbox().files().exists("/absent"));
    }
    @Test void successfulConnectTrailerMayContainMetadata() throws Exception {
        server.enqueue(new MockResponse().setHeader("Content-Type","application/connect+json").setBody(frame(2,"{\"metadata\":{\"x-test\":[\"ok\"]}}")));
        ConnectRpcClient c=new ConnectRpcClient(server.url("/").toString(),null,Duration.ofSeconds(2)); rpcs.add(c);
        assertDoesNotThrow(() -> c.stream("process.Process","Start",Collections.emptyMap(),b -> {}));
    }
    @Test void connectErrorReadsNestedTrailerError() throws Exception {
        server.enqueue(new MockResponse().setHeader("Content-Type","application/connect+json").setBody(frame(2,"{\"error\":{\"code\":\"not_found\",\"message\":\"missing\"}}")));
        ConnectRpcClient c=new ConnectRpcClient(server.url("/").toString(),null,Duration.ofSeconds(2)); rpcs.add(c);
        ConnectRpcException e=assertThrows(ConnectRpcException.class,() -> c.stream("process.Process","Connect",Collections.emptyMap(),b -> {}));
        assertEquals("not_found",e.getCode());
    }
    @Test void commandTimeoutIsEnforced() throws Exception {
        server.enqueue(process(1).setBodyDelay(1500,TimeUnit.MILLISECONDS));
        CommandOpts o=new CommandOpts(); o.setTimeout(1);
        assertThrows(SandboxException.class,() -> sandbox().commands().run("sleep 5",o));
    }
    @Test void desktopLaunchKeepsXvfbRunning() throws Exception {
        Sandbox s=sandbox(); Constructor<DesktopSandbox> ctor=DesktopSandbox.class.getDeclaredConstructor(Sandbox.class); ctor.setAccessible(true); DesktopSandbox desktop=ctor.newInstance(s);
        server.enqueue(process(101)); server.enqueue(process(102)); server.enqueue(process(104));
        Method initialize=DesktopSandbox.class.getDeclaredMethod("initializeDisplay"); initialize.setAccessible(true); initialize.invoke(desktop);
        server.takeRequest(); server.takeRequest(); server.takeRequest();
        server.enqueue(process(103)); desktop.launch("firefox");
        RecordedRequest next=server.takeRequest(); assertEquals("/process.Process/Start",next.getPath(),"launch must not signal the Xvfb process");
    }
    @Test void vncUsesDesktopDisplay() throws Exception {
        Sandbox s=sandbox(); Constructor<DesktopSandbox> ctor=DesktopSandbox.class.getDeclaredConstructor(Sandbox.class); ctor.setAccessible(true); DesktopSandbox desktop=ctor.newInstance(s);
        Field display=DesktopSandbox.class.getDeclaredField("display");display.setAccessible(true);display.set(desktop,":99");
        server.enqueue(process(1)); server.enqueue(process(2)); server.enqueue(process(3)); desktop.getStream().start();
        assertTrue(command(server.takeRequest()).contains("-display :99"));
    }

}
