package ai.novita.sandbox.core;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okio.Buffer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.nio.charset.StandardCharsets;
import java.util.Arrays;
import java.util.Collections;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertThrows;

class CommandsProtocolTest {
    private MockWebServer server;

    @AfterEach
    void stopServer() throws Exception {
        if (server != null) {
            server.shutdown();
        }
    }

    @Test
    void runUsesLoginShellAndDecodesStreamedOutput() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":42}}}"),
                dataFrame("{\"event\":{\"data\":{\"stdout\":\"aGVsbG8=\"}}}"),
                dataFrame("{\"event\":{\"data\":{\"stderr\":\"d2FybmluZw==\"}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));

        Sandbox sandbox = connectSandbox();
        CommandResult result = sandbox.commands().runProcess("printf hello", new ProcessOptions());

        assertEquals("hello", result.getStdout());
        assertEquals("warning", result.getStderr());
        assertEquals(0, result.getExitCode());

        assertEquals("/sandboxes/sb-1/connect", server.takeRequest(1, TimeUnit.SECONDS).getPath());
        RecordedRequest streamRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/process.Process/Start", streamRequest.getPath());
        JsonObject request = firstEnvelopeJson(streamRequest);
        assertEquals("/bin/bash", request.getAsJsonObject("process").get("cmd").getAsString());
        assertEquals("-l", request.getAsJsonObject("process").getAsJsonArray("args").get(0).getAsString());
        assertEquals("-c", request.getAsJsonObject("process").getAsJsonArray("args").get(1).getAsString());
        assertEquals("printf hello", request.getAsJsonObject("process").getAsJsonArray("args").get(2).getAsString());
    }

    @Test
    void startReturnsOnlyAfterPidEventIsAvailable() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":321}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}"))
                .setBodyDelay(200, TimeUnit.MILLISECONDS));

        CommandHandle handle = connectSandbox().commands().start("true", new ProcessOptions());

        assertEquals(321, handle.getPid());
        assertEquals(0, handle.waitFor().getExitCode());
    }

    @Test
    void processInputIsRejectedAfterTerminalEvent() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":7}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));
        // If the implementation incorrectly sends input, let the call finish so
        // the assertion fails immediately instead of waiting for a network timeout.
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));

        CommandHandle handle = connectSandbox().commands().start("true", new ProcessOptions());
        handle.waitFor();

        assertThrows(SandboxException.class,
                () -> handle.sendStdin("late input".getBytes(StandardCharsets.UTF_8)));
    }

    @Test
    void listMapsProcessConfiguration() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"processes\":[{\"pid\":42,\"tag\":\"worker\",\"config\":{"
                        + "\"cmd\":\"python\",\"args\":[\"-u\",\"app.py\"],"
                        + "\"envs\":{\"MODE\":\"test\"},\"cwd\":\"/work\"}}]}"));

        ProcessInfo[] processes = connectSandbox().commands().list();

        assertEquals(1, processes.length);
        assertEquals(42, processes[0].getPid());
        assertEquals("worker", processes[0].getTag());
        assertEquals("python", processes[0].getCommand());
        assertEquals(Arrays.asList("-u", "app.py"), processes[0].getArgs());
        assertEquals(Collections.singletonMap("MODE", "test"), processes[0].getEnvironment());
        assertEquals("/work", processes[0].getWorkingDirectory());
    }

    @Test
    void killSendsSigkillAndReturnsTrue() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));

        assertEquals(true, connectSandbox().commands().kill(77));

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/process.Process/SendSignal", request.getPath());
        JsonObject payload = JsonParser.parseString(request.getBody().readUtf8()).getAsJsonObject();
        assertEquals(77, payload.getAsJsonObject("process").get("pid").getAsInt());
        assertEquals("SIGNAL_SIGKILL", payload.get("signal").getAsString());
    }

    @Test
    void closeStdinUsesDedicatedRpcOnSupportedEnvd() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));

        connectSandbox().commands().closeStdin(31);

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/process.Process/CloseStdin", request.getPath());
        JsonObject payload = JsonParser.parseString(request.getBody().readUtf8()).getAsJsonObject();
        assertEquals(31, payload.getAsJsonObject("process").get("pid").getAsInt());
    }

    @Test
    void nonZeroExitThrowsTypedExceptionWithCommandResult() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":9}}}"),
                dataFrame("{\"event\":{\"data\":{\"stderr\":\"YmFk\"}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":7,\"exited\":true,\"error\":\"boom\"}}}"),
                trailer("{}")));

        CommandExitException error = assertThrows(CommandExitException.class,
                () -> connectSandbox().commands().runProcess("false", new ProcessOptions()));

        assertEquals(7, error.getExitCode());
        assertEquals("bad", error.getStderr());
        assertEquals("boom", error.getCommandError());
    }

    @Test
    void startSerializesStdinOption() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":55}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));

        CommandHandle handle = connectSandbox().commands().start(
                "cat", new ProcessOptions().setStdin(true));
        handle.waitFor();

        server.takeRequest(1, TimeUnit.SECONDS);
        JsonObject payload = firstEnvelopeJson(server.takeRequest(1, TimeUnit.SECONDS));
        assertEquals(true, payload.get("stdin").getAsBoolean());
    }

    @Test
    void legacyStartUsesDefaultUserAuthenticationRegardlessOfDomain() throws Exception {
        startServer();
        enqueueSandboxInfo("0.3.9");
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":56}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));

        CommandHandle handle = connectSandbox().commands().start("true", new ProcessOptions());
        handle.waitFor();

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest start = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic dXNlcjo=", start.getHeader("Authorization"));
    }

    @Test
    void explicitCommandUserIsScopedToOneRequest() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":57}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":58}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));

        Sandbox sandbox = connectSandbox();
        sandbox.commands().start("id", new ProcessOptions().setUser("root")).waitFor();
        sandbox.commands().start("id", new ProcessOptions()).waitFor();

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest rootRequest = server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest defaultRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", rootRequest.getHeader("Authorization"));
        assertEquals(null, defaultRequest.getHeader("Authorization"));
    }

    @Test
    void explicitClosedStdinIsRejectedBeforeEnvdVersion030() throws Exception {
        startServer();
        enqueueSandboxInfo("0.2.9");
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":55}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));

        assertThrows(SandboxException.class, () -> connectSandbox().commands().start(
                "cat", new ProcessOptions().setStdin(false)));
    }

    @Test
    void runInvokesOutputCallbacksAndStillAccumulatesResult() throws Exception {
        startServer();
        enqueueSandboxInfo();
        server.enqueue(stream(
                dataFrame("{\"event\":{\"start\":{\"pid\":42}}}"),
                dataFrame("{\"event\":{\"data\":{\"stdout\":\"aGVsbG8=\"}}}"),
                dataFrame("{\"event\":{\"data\":{\"stderr\":\"d2FybmluZw==\"}}}"),
                dataFrame("{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                trailer("{}")));
        AtomicReference<String> stdout = new AtomicReference<>();
        AtomicReference<String> stderr = new AtomicReference<>();
        ProcessOptions options = new ProcessOptions()
                .setOnStdout(stdout::set)
                .setOnStderr(stderr::set);

        CommandResult result = connectSandbox().commands().runProcess("printf hello", options);

        assertEquals("hello", stdout.get());
        assertEquals("warning", stderr.get());
        assertEquals("hello", result.getStdout());
        assertEquals("warning", result.getStderr());
    }

    private Sandbox connectSandbox() throws Exception {
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        opts.setBaseUrl(server.url("/").toString());
        return Sandbox.connect("sb-1", opts);
    }

    private void startServer() throws Exception {
        server = new MockWebServer();
        server.start();
    }

    private void enqueueSandboxInfo() {
        enqueueSandboxInfo("0.5.7");
    }

    private void enqueueSandboxInfo(String envdVersion) {
        String domain = server.url("/").toString();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"sandboxID\":\"sb-1\",\"domain\":\"" + domain
                        + "\",\"envdVersion\":\"" + envdVersion
                        + "\",\"envdAccessToken\":\"token\"}"));
    }

    private static MockResponse stream(byte[]... frames) {
        Buffer body = new Buffer();
        for (byte[] frame : frames) {
            body.write(frame);
        }
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/connect+json")
                .setBody(body);
    }

    private static byte[] dataFrame(String json) {
        return frame(0, json);
    }

    private static byte[] trailer(String json) {
        return frame(2, json);
    }

    private static byte[] frame(int flags, String json) {
        byte[] payload = json.getBytes(StandardCharsets.UTF_8);
        Buffer frame = new Buffer();
        frame.writeByte(flags);
        frame.writeInt(payload.length);
        frame.write(payload);
        return frame.readByteArray();
    }

    private static JsonObject firstEnvelopeJson(RecordedRequest request) throws Exception {
        Buffer body = request.getBody();
        body.readByte();
        int length = body.readInt();
        return JsonParser.parseString(body.readUtf8(length)).getAsJsonObject();
    }
}
