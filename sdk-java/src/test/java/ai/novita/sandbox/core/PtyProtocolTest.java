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
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;

class PtyProtocolTest {
    private MockWebServer server;

    @AfterEach
    void stopServer() throws Exception {
        if (server != null) server.shutdown();
    }

    @Test
    void createStartsInteractiveShellWithTerminalDefaults() throws Exception {
        server = new MockWebServer();
        server.start();
        enqueueSandboxInfo();
        server.enqueue(stream(
                frame(0, "{\"event\":{\"start\":{\"pid\":81}}}"),
                frame(0, "{\"event\":{\"data\":{\"pty\":\"aGk=\"}}}"),
                frame(0, "{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                frame(2, "{}")));
        AtomicReference<byte[]> output = new AtomicReference<>();
        PtyOptions options = new PtyOptions()
                .setSize(new PtySize(80, 24))
                .setOnData(output::set);

        CommandHandle handle = connectSandbox().pty().create(options);
        handle.waitFor();

        assertEquals(81, handle.getPid());
        assertArrayEquals("hi".getBytes(StandardCharsets.UTF_8), output.get());
        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        JsonObject payload = firstEnvelopeJson(request);
        assertEquals("/bin/bash", payload.getAsJsonObject("process").get("cmd").getAsString());
        assertEquals("-i", payload.getAsJsonObject("process").getAsJsonArray("args").get(0).getAsString());
        assertEquals("-l", payload.getAsJsonObject("process").getAsJsonArray("args").get(1).getAsString());
        assertEquals("xterm-256color", payload.getAsJsonObject("process")
                .getAsJsonObject("envs").get("TERM").getAsString());
        assertEquals(80, payload.getAsJsonObject("pty").getAsJsonObject("size").get("cols").getAsInt());
        assertEquals(24, payload.getAsJsonObject("pty").getAsJsonObject("size").get("rows").getAsInt());
    }

    @Test
    void createUsesExplicitUserAuthentication() throws Exception {
        server = new MockWebServer();
        server.start();
        enqueueSandboxInfo();
        server.enqueue(stream(
                frame(0, "{\"event\":{\"start\":{\"pid\":82}}}"),
                frame(0, "{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"),
                frame(2, "{}")));

        connectSandbox().pty().create(new PtyOptions()
                .setSize(new PtySize(80, 24))
                .setUser("root"))
                .waitFor();

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest create = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", create.getHeader("Authorization"));
    }

    private Sandbox connectSandbox() throws Exception {
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        opts.setBaseUrl(server.url("/").toString());
        return Sandbox.connect("sb-1", opts);
    }

    private void enqueueSandboxInfo() {
        server.enqueue(new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"sandboxID\":\"sb-1\",\"domain\":\"" + server.url("/")
                        + "\",\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"token\"}"));
    }

    private static MockResponse stream(byte[]... frames) {
        Buffer body = new Buffer();
        for (byte[] frame : frames) body.write(frame);
        return new MockResponse().setResponseCode(200)
                .setHeader("Content-Type", "application/connect+json").setBody(body);
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
