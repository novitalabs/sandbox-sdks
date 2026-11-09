package ai.novita.sandbox.core;

import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import okio.Buffer;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.io.ByteArrayInputStream;
import java.io.ByteArrayOutputStream;
import java.nio.charset.StandardCharsets;
import java.util.Collections;
import java.util.List;
import java.util.concurrent.TimeUnit;
import java.util.zip.GZIPInputStream;
import java.util.zip.GZIPOutputStream;

import static org.junit.jupiter.api.Assertions.assertArrayEquals;
import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Gzip transport tests for the envd file endpoints: compressed octet-stream
 * uploads and transparent decompression of gzipped downloads.
 */
class FilesystemGzipTest {
    private MockWebServer server;

    @AfterEach
    void stopServer() throws Exception {
        if (server != null) {
            server.shutdown();
        }
    }

    @Test
    void octetUploadSendsGzippedBody() throws Exception {
        startServer();
        enqueueConnectInfo("0.5.7");
        byte[] original = "hello gzip upload".getBytes(StandardCharsets.UTF_8);
        server.enqueue(json("[{\"path\":\"/work/data.txt\",\"mode\":420,\"size\":17}]"));

        FilesystemWriteOptions opts = new FilesystemWriteOptions();
        opts.setUseOctetStream(true);
        opts.setGzip(true);
        connectSandbox().files().writeFiles(
                Collections.singletonList(new WriteEntry("/work/data.txt", original)), opts);

        server.takeRequest(1, TimeUnit.SECONDS); // discard the connect POST
        RecordedRequest upload = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("POST", upload.getMethod());
        assertEquals("gzip", upload.getHeader("Content-Encoding"));
        assertEquals("application/octet-stream", upload.getHeader("Content-Type"));
        // The request body must decompress back to the original content.
        assertArrayEquals(original, gunzip(upload.getBody().readByteArray()));
    }

    @Test
    void readDecompressesGzippedResponse() throws Exception {
        startServer();
        enqueueConnectInfo("0.5.7");
        byte[] original = "hello gzip download".getBytes(StandardCharsets.UTF_8);
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/octet-stream")
                .setHeader("Content-Encoding", "gzip")
                .setBody(new Buffer().write(gzip(original))));

        byte[] read = connectSandbox().files().read("/work/data.txt");

        assertArrayEquals(original, read);
        server.takeRequest(1, TimeUnit.SECONDS); // discard the connect POST
        RecordedRequest download = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("GET", download.getMethod());
        assertTrue(download.getPath().startsWith("/files"));
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

    private void enqueueConnectInfo(String envdVersion) {
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

    private static byte[] gzip(byte[] data) throws Exception {
        ByteArrayOutputStream out = new ByteArrayOutputStream();
        try (GZIPOutputStream g = new GZIPOutputStream(out)) {
            g.write(data);
        }
        return out.toByteArray();
    }

    private static byte[] gunzip(byte[] data) throws Exception {
        try (GZIPInputStream g = new GZIPInputStream(new ByteArrayInputStream(data));
             ByteArrayOutputStream out = new ByteArrayOutputStream()) {
            byte[] buffer = new byte[8192];
            int n;
            while ((n = g.read(buffer)) >= 0) out.write(buffer, 0, n);
            return out.toByteArray();
        }
    }
}
