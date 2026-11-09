package ai.novita.sandbox.core;

import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okio.Buffer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.time.Duration;
import java.util.Collections;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertInstanceOf;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class ConnectRpcClientTest {
    private MockWebServer server;
    private ConnectRpcClient client;

    @AfterEach
    void closeResources() throws Exception {
        if (client != null) {
            client.close();
        }
        if (server != null) {
            server.shutdown();
        }
    }

    @Test
    void unaryDoesNotInjectBasicAuthenticationOnModernClusters() throws Exception {
        startServer();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"processes\":[]}"));

        client = new ConnectRpcClient(baseUrl(), "envd-token", "sb-1", Duration.ofSeconds(2));
        client.call("process.Process", "List", Collections.emptyMap(), Object.class);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/process.Process/List", request.getPath());
        assertEquals(null, request.getHeader("Authorization"));
        assertEquals("envd-token", request.getHeader("X-Access-Token"));
        assertEquals("sb-1", request.getHeader("Novita-Sandbox-Id"));
        assertEquals("1", request.getHeader("Connect-Protocol-Version"));
    }

    @Test
    void unaryCombinesAccessTokenWithRequestScopedBasicAuthentication() throws Exception {
        startServer();
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));

        client = new ConnectRpcClient(baseUrl(), "envd-token", "sb-1", Duration.ofSeconds(2));
        client.call("process.Process", "List", Collections.emptyMap(), Object.class,
                Collections.singletonMap("Authorization", "Basic cm9vdDo="));

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", request.getHeader("Authorization"));
        assertEquals("envd-token", request.getHeader("X-Access-Token"));
    }

    @Test
    void requestHeadersDoNotLeakIntoLaterUnaryCalls() throws Exception {
        startServer();
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));
        server.enqueue(new MockResponse().setResponseCode(200).setBody("{}"));

        client = new ConnectRpcClient(baseUrl(), "envd-token", "sb-1", Duration.ofSeconds(2));
        client.call("filesystem.Filesystem", "Stat", Collections.emptyMap(), Object.class,
                Collections.singletonMap("Authorization", "Basic cm9vdDo="));
        client.call("process.Process", "List", Collections.emptyMap(), Object.class);

        RecordedRequest userRequest = server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest defaultRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", userRequest.getHeader("Authorization"));
        assertEquals(null, defaultRequest.getHeader("Authorization"));
    }

    @Test
    void streamDeliversDataAndAcceptsEmptyTrailer() throws Exception {
        startServer();
        byte[] data = "{\"event\":{\"start\":{\"pid\":42}}}".getBytes(StandardCharsets.UTF_8);
        server.enqueue(connectStream(frame(0, data), frame(2, "{}".getBytes(StandardCharsets.UTF_8))));

        client = new ConnectRpcClient(baseUrl(), "token", Duration.ofSeconds(2));
        AtomicReference<String> value = new AtomicReference<>();
        client.stream("process.Process", "Start", Collections.emptyMap(),
                bytes -> value.set(new String(bytes, StandardCharsets.UTF_8)));

        assertEquals(new String(data, StandardCharsets.UTF_8), value.get());
    }

    @Test
    void streamSurfacesErrorTrailer() throws Exception {
        startServer();
        server.enqueue(connectStream(frame(2,
                "{\"code\":\"not_found\",\"message\":\"missing process\"}"
                        .getBytes(StandardCharsets.UTF_8))));

        client = new ConnectRpcClient(baseUrl(), "token", Duration.ofSeconds(2));
        ConnectRpcException error = assertThrows(ConnectRpcException.class,
                () -> client.stream("process.Process", "Connect", Collections.emptyMap(), ignored -> { }));

        assertEquals("not_found", error.getCode());
        assertTrue(error.getMessage().contains("missing process"));
    }

    @Test
    void streamRejectsTruncatedFrameHeader() throws Exception {
        startServer();
        server.enqueue(connectStream(new byte[]{0, 0, 0}));

        client = new ConnectRpcClient(baseUrl(), "token", Duration.ofSeconds(2));
        SandboxException error = assertThrows(SandboxException.class,
                () -> client.stream("process.Process", "Start", Collections.emptyMap(), ignored -> { }));

        assertInstanceOf(java.io.EOFException.class, error.getCause());
    }

    @Test
    void streamRejectsCompressedFrameWithoutNegotiatedEncoding() throws Exception {
        startServer();
        server.enqueue(connectStream(frame(1, "compressed".getBytes(StandardCharsets.UTF_8))));

        client = new ConnectRpcClient(baseUrl(), "token", Duration.ofSeconds(2));
        SandboxException error = assertThrows(SandboxException.class,
                () -> client.stream("process.Process", "Start", Collections.emptyMap(), ignored -> { }));

        assertTrue(error.getMessage().contains("compressed"));
    }

    private void startServer() throws Exception {
        server = new MockWebServer();
        server.start();
    }

    private String baseUrl() {
        return server.url("/").toString();
    }

    private static MockResponse connectStream(byte[]... frames) {
        Buffer body = new Buffer();
        for (byte[] frame : frames) {
            body.write(frame);
        }
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/connect+json")
                .setBody(body);
    }

    private static byte[] frame(int flags, byte[] payload) throws Exception {
        ByteArrayOutputStream output = new ByteArrayOutputStream();
        output.write(flags);
        output.write(new byte[]{
                (byte) (payload.length >>> 24),
                (byte) (payload.length >>> 16),
                (byte) (payload.length >>> 8),
                (byte) payload.length
        });
        output.write(payload);
        return output.toByteArray();
    }
}
