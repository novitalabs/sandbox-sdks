package example.customer;

import ai.novita.sandbox.core.*;
import ai.novita.sandbox.code_interpreter.*;
import com.google.gson.JsonParser;
import com.sun.net.httpserver.HttpExchange;
import com.sun.net.httpserver.HttpServer;
import org.junit.jupiter.api.*;

import java.io.*;
import java.net.InetSocketAddress;
import java.nio.charset.StandardCharsets;
import java.nio.file.Path;
import java.time.Duration;
import java.util.Properties;
import java.util.concurrent.CountDownLatch;
import java.util.concurrent.ExecutorService;
import java.util.concurrent.Executors;
import java.util.concurrent.TimeUnit;
import java.util.concurrent.atomic.AtomicReference;
import java.util.jar.JarFile;

import static org.junit.jupiter.api.Assertions.*;

/** Independent consumer: no reflection, source paths, or SDK package-private APIs. */
@Timeout(15)
class PackageConsumerTest {
    private HttpServer server;
    private ExecutorService executor;
    private final AtomicReference<Throwable> fixtureFailure = new AtomicReference<>();
    private SandboxOptions options;

    @BeforeEach void startServer() throws Exception {
        server = HttpServer.create(new InetSocketAddress("127.0.0.1", 0), 0);
        executor = Executors.newCachedThreadPool(r -> { Thread t = new Thread(r); t.setDaemon(true); return t; });
        server.setExecutor(executor);
        server.createContext("/", exchange -> {
            try { respond(exchange); }
            catch (Throwable failure) { fixtureFailure.compareAndSet(null, failure); }
            finally { exchange.close(); }
        });
        server.start();
        String url = "http://127.0.0.1:" + server.getAddress().getPort();
        options = new SandboxOptions().apiKey("consumer-api-key").baseUrl(url).domain("local.invalid");
        options.setSandboxUrl(url);
    }

    @AfterEach void stopServer() {
        server.stop(0);
        executor.shutdownNow();
        assertNull(fixtureFailure.get(), () -> "mock service assertion failed: " + fixtureFailure.get());
    }

    private void respond(HttpExchange request) throws Exception {
        String path = request.getRequestURI().getPath();
        if (path.equals("/sandboxes") || path.equals("/sandboxes/consumer-sandbox/connect")) {
            assertEquals("consumer-api-key", request.getRequestHeaders().getFirst("X-API-Key"));
            if (path.endsWith("/connect")) {
                String body = new String(request.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
                assertEquals(300, JsonParser.parseString(body).getAsJsonObject().get("timeout").getAsInt());
            }
            json(request, 200, "{\"sandboxID\":\"consumer-sandbox\",\"templateID\":\"test-template\",\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"consumer-envd\",\"trafficAccessToken\":\"consumer-traffic\",\"domain\":\"local.invalid\"}");
        } else if (path.equals("/sandboxes/consumer-sandbox")) {
            request.sendResponseHeaders(204, -1);
        } else if (path.equals("/files")) {
            assertEquals("consumer-envd", request.getRequestHeaders().getFirst("X-Access-Token"));
            if (request.getRequestMethod().equals("GET")) {
                byte[] bytes = new byte[]{'o', 'k', 0, (byte) 255};
                request.sendResponseHeaders(200, bytes.length);
                request.getResponseBody().write(bytes);
            } else {
                byte[] body = request.getRequestBody().readAllBytes();
                // Binary payload must survive either multipart or octet-stream encoding.
                assertTrue(new String(body, StandardCharsets.ISO_8859_1).contains("ok\u0000\u00ff"));
                json(request, 200, "[{\"path\":\"/tmp/consumer.bin\",\"name\":\"consumer.bin\",\"type\":\"file\"}]");
            }
        } else if (path.equals("/filesystem.Filesystem/Stat")) {
            json(request, 404, "{\"code\":\"not_found\",\"message\":\"missing\"}");
        } else if (path.equals("/filesystem.Filesystem/WatchDir")) {
            request.getResponseHeaders().set("Content-Type", "application/connect+json");
            request.sendResponseHeaders(200, 0);
            frame(request.getResponseBody(), 0, "{\"start\":{}}");
            frame(request.getResponseBody(), 0, "{\"filesystem\":{\"name\":\"created.txt\",\"type\":\"EVENT_TYPE_CREATE\"}}");
            frame(request.getResponseBody(), 2, "{\"metadata\":{}}");
        } else if (path.equals("/process.Process/Start")) {
            String body = new String(request.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            request.getResponseHeaders().set("Content-Type", "application/connect+json");
            request.sendResponseHeaders(200, 0);
            frame(request.getResponseBody(), 0, "{\"event\":{\"start\":{\"pid\":41}}}");
            request.getResponseBody().flush();
            if (body.contains("consumer-stall")) {
                try { Thread.sleep(2000); }
                catch (InterruptedException expectedDuringTeardown) { Thread.currentThread().interrupt(); }
                return;
            }
            if (body.contains("consumer-denied")) {
                frame(request.getResponseBody(), 2, "{\"error\":{\"code\":\"permission_denied\",\"message\":\"denied\"},\"metadata\":{}}");
            } else {
                frame(request.getResponseBody(), 0, "{\"event\":{\"data\":{\"stdout\":\"Y29uc3VtZXItb2sK\"}}}");
                frame(request.getResponseBody(), 0, "{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}");
                frame(request.getResponseBody(), 2, "{\"metadata\":{\"x-fixture\":[\"ok\"]}}");
            }
        } else if (path.equals("/execute")) {
            assertEquals("consumer-envd", request.getRequestHeaders().getFirst("X-Access-Token"));
            assertEquals("consumer-traffic", request.getRequestHeaders().getFirst("E2B-Traffic-Access-Token"));
            String body = new String(request.getRequestBody().readAllBytes(), StandardCharsets.UTF_8);
            if (body.contains("context_id")) assertFalse(JsonParser.parseString(body).getAsJsonObject().has("language"));
            request.getResponseHeaders().set("Content-Type", "application/x-ndjson");
            request.sendResponseHeaders(200, 0);
            request.getResponseBody().write(("{\"type\":\"stdout\",\"text\":\"consumer-ok\\n\"}\n"
                + "{\"type\":\"result\",\"text\":\"42\",\"is_main_result\":true}\n").getBytes(StandardCharsets.UTF_8));
        } else if (path.equals("/templates/t/builds/b/status")) {
            json(request, 200, "{\"templateID\":\"t\",\"buildID\":\"b\",\"status\":\"error\",\"logs\":[],\"reason\":{\"message\":\"failed\",\"step\":\"RUN npm install\",\"logEntries\":[]}}");
        } else {
            fail("unexpected request: " + request.getRequestMethod() + " " + path);
        }
    }

    private static void json(HttpExchange exchange, int status, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        exchange.getResponseHeaders().set("Content-Type", "application/json");
        exchange.sendResponseHeaders(status, bytes.length);
        exchange.getResponseBody().write(bytes);
    }
    private static void frame(OutputStream stream, int flag, String body) throws IOException {
        byte[] bytes = body.getBytes(StandardCharsets.UTF_8);
        DataOutputStream output = new DataOutputStream(stream);
        output.writeByte(flag); output.writeInt(bytes.length); output.write(bytes);
    }

    @Test void consumesTheInstalledJarAndExpectedVersion() throws Exception {
        Path origin = Path.of(Novita.class.getProtectionDomain().getCodeSource().getLocation().toURI());
        assertTrue(origin.toString().endsWith(".jar"), "SDK must load from a JAR, not a classes directory");
        try (JarFile jar = new JarFile(origin.toFile())) {
            assertNotNull(jar.getJarEntry("META-INF/LICENSE"), "Published JAR must carry its license");
            try (InputStream license = jar.getInputStream(jar.getJarEntry("META-INF/LICENSE"))) {
                String text = new String(license.readAllBytes(), StandardCharsets.UTF_8);
                assertTrue(text.contains("MIT License") && text.contains("Copyright") && text.contains("Permission is hereby granted"));
            }
            Properties props = new Properties();
            try (InputStream input = jar.getInputStream(jar.getJarEntry("META-INF/maven/ai.novita/novita-sandbox/pom.properties"))) { props.load(input); }
            assertEquals(System.getProperty("novita.version"), props.getProperty("version"));
            System.out.println("SDK package origin: " + origin);
        }
    }

    @Test void facadeFilesCommandsAndReconnectWorkFromTheJar() throws Exception {
        try (Novita client = Novita.create(options)) {
            Sandbox sandbox = client.createSandbox("base");
            try {
                sandbox.files().writeStream("/tmp/consumer.bin", new ByteArrayInputStream(new byte[]{'o', 'k', 0, (byte)255}));
                try (InputStream stream = sandbox.files().readStream("/tmp/consumer.bin")) {
                    assertArrayEquals(new byte[]{'o', 'k', 0, (byte)255}, stream.readAllBytes());
                }
                assertFalse(sandbox.files().exists("/absent"));
                assertEquals("consumer-ok\n", sandbox.commands().run("printf consumer-ok", null).getStdout());
                try (Sandbox connected = client.connectSandbox("consumer-sandbox")) {
                    assertEquals("consumer-sandbox", connected.getId());
                }
            } finally { sandbox.kill(); }
        }
    }

    @Test void failedAndTimedOutCommandsNeverReportSuccess() throws Exception {
        try (Novita client = Novita.create(options);
             Sandbox sandbox = client.createSandbox("base")) {
            assertThrows(SandboxException.class, () -> sandbox.commands().run("consumer-denied", null));
            CommandOpts timeout = new CommandOpts(); timeout.setTimeout(1);
            assertTimeout(Duration.ofMillis(1800), () ->
                assertThrows(SandboxException.class, () -> sandbox.commands().run("consumer-stall", timeout)));
        }
    }

    @Test void interpreterParsesOutputAndReconnectsThroughFacade() throws Exception {
        try (Novita client = Novita.create(options);
             CodeInterpreterSandbox sandbox = CodeInterpreterSandbox.wrap(client.createSandbox("code-interpreter-v1"))) {
            StringBuilder streamed = new StringBuilder();
            RunCodeOpts opts = new RunCodeOpts().setOnStdout(streamed::append);
            ExecutionResult result = sandbox.runCode("print('consumer-ok'); 6 * 7", opts);
            assertEquals("consumer-ok\n", result.getStdout());
            assertEquals("consumer-ok\n", streamed.toString());
            assertEquals("42", result.getRichResults().get(0).getText());
            try (CodeInterpreterSandbox reconnected = CodeInterpreterSandbox.wrap(client.connectSandbox("consumer-sandbox"))) {
                assertEquals("42", reconnected.runCode("6 * 7", new RunCodeOpts().setContextId("ctx")).getRichResults().get(0).getText());
            }
        }
    }

    @Test void templatePublicModelPreservesCopyOwnerAndBuildFailure() throws Exception {
        String spec = TemplateBuilder.create().fromDockerfile("FROM debian:12\nCOPY --chown=app:app a.txt /app/\n")
                .inheritImageConfig(false).toJson();
        com.google.gson.JsonObject copy = null;
        for (com.google.gson.JsonElement step : JsonParser.parseString(spec).getAsJsonObject().getAsJsonArray("steps")) {
            if ("COPY".equals(step.getAsJsonObject().get("type").getAsString())) copy = step.getAsJsonObject();
        }
        assertNotNull(copy);
        assertEquals("app:app", copy.getAsJsonArray("args").get(2).getAsString());
        try (Novita client = Novita.create(options)) {
            BuildStatus status = client.templates().getBuildStatus("t", "b");
            assertEquals("error", status.getStatus());
            assertEquals("RUN npm install", status.getReason().getStep());
        }
    }

    @Test void desktopConnectIsAvailableFromTheJar() throws Exception {
        // Compatibility coverage for the lower-level static entry point.
        try (ai.novita.sandbox.desktop.DesktopSandbox desktop =
                 ai.novita.sandbox.desktop.DesktopSandbox.connect("consumer-sandbox", options)) {
            assertEquals("consumer-sandbox", desktop.getSandbox().getId());
        }
    }

    @Test void watchConsumesCanonicalServerEvents() throws Exception {
        CountDownLatch received = new CountDownLatch(1);
        AtomicReference<FilesystemEvent> event = new AtomicReference<>();
        try (Novita client = Novita.create(options);
             Sandbox sandbox = client.createSandbox("base")) {
            try (WatchHandle watch = sandbox.files().watch("/tmp", false, value -> {
                event.set(value); received.countDown();
            }, new FilesystemRequestOptions().setRequestTimeoutSeconds(1))) {
                assertTrue(received.await(1, TimeUnit.SECONDS));
                assertEquals("created.txt", event.get().getName());
                assertEquals(FilesystemEvent.EventType.CREATE, event.get().getEventType());
                assertNull(watch.getFailure());
            }
        }
    }
}
