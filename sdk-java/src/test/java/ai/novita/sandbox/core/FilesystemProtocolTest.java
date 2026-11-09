package ai.novita.sandbox.core;

import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okio.Buffer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.time.Instant;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

class FilesystemProtocolTest {
    private MockWebServer server;

    @AfterEach
    void stopServer() throws Exception {
        if (server != null) {
            server.shutdown();
        }
    }

    @Test
    void listParsesProtobufJsonEnumNames() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{\"entries\":[{\"name\":\"data\",\"path\":\"/data\","
                + "\"type\":\"FILE_TYPE_DIRECTORY\",\"modifiedTime\":\"2026-09-22T00:00:00Z\"}]}"));

        EntryInfo[] entries = connectSandbox().files().list("/", 1);

        assertEquals(1, entries.length);
        assertTrue(entries[0].isDir());
        assertEquals(Instant.parse("2026-09-22T00:00:00Z"), entries[0].getModifiedTime());
    }

    @Test
    void filesystemRpcUsesExplicitUserAuthentication() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{\"entries\":[]}"));

        connectSandbox().files().list("/", 1,
                new FilesystemRequestOptions().setUser("root"));

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest listRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", listRequest.getHeader("Authorization"));
    }

    @Test
    void legacyFilesystemRpcUsesDefaultUserAuthentication() throws Exception {
        startServer();
        enqueueSandboxInfo("0.3.9");
        server.enqueue(json("{\"entries\":[]}"));

        connectSandbox().files().list("/", 1);

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest listRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic dXNlcjo=", listRequest.getHeader("Authorization"));
    }

    @Test
    void filesystemRequestOptionsAuthenticateEveryUnaryOperation() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{}"));
        server.enqueue(json("{\"entry\":{\"name\":\"new.txt\",\"path\":\"/new.txt\","
                + "\"type\":\"FILE_TYPE_FILE\"}}"));
        server.enqueue(json("{}"));
        server.enqueue(json("{\"entry\":{\"name\":\"new.txt\",\"path\":\"/new.txt\","
                + "\"type\":\"FILE_TYPE_FILE\"}}"));
        server.enqueue(json("{\"entry\":{\"name\":\"new.txt\",\"path\":\"/new.txt\","
                + "\"type\":\"FILE_TYPE_FILE\"}}"));
        FilesystemRequestOptions options = new FilesystemRequestOptions().setUser("root");

        Sandbox sandbox = connectSandbox();
        sandbox.files().makeDir("/new", options);
        sandbox.files().rename("/old.txt", "/new.txt", options);
        sandbox.files().remove("/new.txt", options);
        sandbox.files().getInfo("/new.txt", options);
        assertTrue(sandbox.files().exists("/new.txt", options));

        server.takeRequest(1, TimeUnit.SECONDS);
        for (int i = 0; i < 5; i++) {
            assertEquals("Basic cm9vdDo=",
                    server.takeRequest(1, TimeUnit.SECONDS).getHeader("Authorization"));
        }
    }

    @Test
    void filesystemWatchUsesExplicitUserAuthentication() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(stream(frame(0, "{\"event\":{\"start\":{}}}"), frame(2, "{}")));

        WatchHandle handle = connectSandbox().files().watch(
                "/work", false, ignored -> { },
                new FilesystemRequestOptions().setUser("root"));
        handle.close();

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest watchRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", watchRequest.getHeader("Authorization"));
    }

    @Test
    void modernEnvdDoesNotDefaultFilesystemUser() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(new MockResponse().setResponseCode(200).setBody("hello"));

        assertEquals("hello", connectSandbox().files().readText("/hello.txt"));

        assertEquals("/sandboxes/sb-1/connect", server.takeRequest(1, TimeUnit.SECONDS).getPath());
        RecordedRequest readRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/files?path=%2Fhello.txt", readRequest.getPath());
    }

    @Test
    void watchWaitsForStartAndDecodesNestedFilesystemEvent() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(stream(
                frame(0, "{\"event\":{\"start\":{}}}"),
                frame(0, "{\"event\":{\"filesystem\":{\"name\":\"a.txt\","
                        + "\"type\":\"EVENT_TYPE_WRITE\"}}}"),
                frame(2, "{}")));

        CountDownLatch received = new CountDownLatch(1);
        AtomicReference<FilesystemEvent> event = new AtomicReference<>();
        WatchHandle handle = connectSandbox().files().watch("/work", false, value -> {
            event.set(value);
            received.countDown();
        });

        assertTrue(received.await(1, TimeUnit.SECONDS));
        assertEquals("a.txt", event.get().getName());
        assertEquals(FilesystemEvent.EventType.WRITE, event.get().getEventType());
        handle.close();
    }

    @Test
    void createWatcherReadsProtobufJsonWatcherId() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{\"watcherId\":\"watch-123\"}"));

        assertEquals("watch-123", connectSandbox().files().createWatcher("/work", true));
    }

    @Test
    void writeFilesUsesMultipartByDefaultAndReturnsServerMetadata() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("[{\"name\":\"canonical.txt\",\"path\":\"/canonical.txt\","
                + "\"type\":\"file\",\"size\":99}]"));

        WriteInfo[] written = connectSandbox().files().writeFiles(
                Collections.singletonList(new WriteEntry("/input.txt", "hello".getBytes(StandardCharsets.UTF_8))),
                new FilesystemWriteOptions());

        assertEquals(1, written.length);
        assertEquals("/canonical.txt", written[0].getPath());
        assertEquals(99, written[0].getSize());

        assertEquals("/sandboxes/sb-1/connect", server.takeRequest(1, TimeUnit.SECONDS).getPath());
        RecordedRequest upload = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/files?path=%2Finput.txt", upload.getPath());
        assertTrue(upload.getHeader("Content-Type").startsWith("multipart/form-data; boundary="));
    }

    @Test
    void writeReturnsServerMetadata() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("[{\"name\":\"note.txt\",\"path\":\"/note.txt\",\"type\":\"file\"}]"));

        WriteInfo written = connectSandbox().files().write(
                "/note.txt",
                "hello".getBytes(StandardCharsets.UTF_8),
                new FilesystemWriteOptions());

        assertEquals("note.txt", written.getName());
        assertEquals("/note.txt", written.getPath());
    }

    @Test
    void explicitOctetStreamGzipSetsWireHeaders() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("[{\"name\":\"note.txt\",\"path\":\"/note.txt\",\"type\":\"file\"}]"));
        FilesystemWriteOptions options = new FilesystemWriteOptions()
                .setUseOctetStream(true)
                .setGzip(true);

        connectSandbox().files().write("/note.txt", "hello".getBytes(StandardCharsets.UTF_8), options);

        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest upload = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("application/octet-stream", upload.getHeader("Content-Type"));
        assertEquals("gzip", upload.getHeader("Content-Encoding"));
    }

    @Test
    void makeDirReturnsTrueWhenDirectoryWasCreated() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{}"));

        boolean created = connectSandbox().files().makeDir("/new-dir");

        assertTrue(created);
        server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/filesystem.Filesystem/MakeDir",
                server.takeRequest(1, TimeUnit.SECONDS).getPath());
    }

    @Test
    void makeDirReturnsFalseWhenDirectoryAlreadyExists() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(new MockResponse()
                .setResponseCode(409)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":\"already_exists\",\"message\":\"directory exists\"}"));

        boolean created = connectSandbox().files().makeDir("/existing");

        assertFalse(created);
    }

    @Test
    void renameReturnsMovedEntryMetadata() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{\"entry\":{\"name\":\"new.txt\",\"path\":\"/new.txt\","
                + "\"type\":\"FILE_TYPE_FILE\",\"size\":5,"
                + "\"modifiedTime\":\"2026-09-22T00:00:00Z\"}}"));

        EntryInfo moved = connectSandbox().files().rename("/old.txt", "/new.txt");

        assertEquals("new.txt", moved.getName());
        assertEquals("/new.txt", moved.getPath());
        assertFalse(moved.isDir());
        assertEquals(5, moved.getSize());
    }

    @Test
    void getInfoMapsDirectoryTypeFromProtobufJson() throws Exception {
        startServer();
        enqueueSandboxInfo("0.5.7");
        server.enqueue(json("{\"entry\":{\"name\":\"data\",\"path\":\"/data\","
                + "\"type\":\"FILE_TYPE_DIRECTORY\","
                + "\"modifiedTime\":\"2026-09-22T00:00:00Z\"}}"));

        EntryInfo entry = connectSandbox().files().getInfo("/data");

        assertTrue(entry.isDir());
        assertEquals(Instant.parse("2026-09-22T00:00:00Z"), entry.getModifiedTime());
    }

    @Test
    void recursiveWatchIsRejectedBeforeEnvdVersion014() throws Exception {
        startServer();
        enqueueSandboxInfo("0.1.3");
        server.enqueue(stream(frame(0, "{\"event\":{\"start\":{}}}"), frame(2, "{}")));

        assertThrows(SandboxException.class,
                () -> connectSandbox().files().watch("/work", true, ignored -> { }));
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

    private void enqueueSandboxInfo(String envdVersion) {
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"domain\":\"" + server.url("/")
                + "\",\"envdVersion\":\"" + envdVersion
                + "\",\"envdAccessToken\":\"token\"}"));
    }

    private static MockResponse json(String body) {
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody(body);
    }

    private static MockResponse stream(byte[]... frames) {
        Buffer body = new Buffer();
        for (byte[] frame : frames) body.write(frame);
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/connect+json")
                .setBody(body);
    }

    private static byte[] frame(int flags, String json) {
        byte[] payload = json.getBytes(StandardCharsets.UTF_8);
        Buffer frame = new Buffer();
        frame.writeByte(flags);
        frame.writeInt(payload.length);
        frame.write(payload);
        return frame.readByteArray();
    }
}
