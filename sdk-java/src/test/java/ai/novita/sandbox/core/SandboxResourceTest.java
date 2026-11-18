package ai.novita.sandbox.core;

import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/** Wire and validation tests for sandbox resource targets, creation and resize. */
class SandboxResourceTest {
    private MockWebServer server;
    private final List<Sandbox> created = new ArrayList<>();

    @AfterEach
    void stopServer() throws Exception {
        for (Sandbox sandbox : created) {
            try {
                server.enqueue(json(200, "{}"));
                sandbox.close();
            } catch (Exception ignored) {
                // The mock server is going away either way; keep teardown quiet.
            }
        }
        created.clear();
        if (server != null) server.shutdown();
    }

    private Sandbox create(String template, SandboxOpts opts) throws SandboxException {
        Sandbox sandbox = Sandbox.create(template, opts);
        created.add(sandbox);
        return sandbox;
    }

    @Test
    void createSendsGiBResourcesToCustomEndpointAsMiB() throws Exception {
        startServer();
        server.enqueue(createdSandbox());

        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets().setCpu(8).setMemoryGiB(2.0).setDiskGiB(4.0));
        create("base", opts);

        RecordedRequest request = takeRequest();
        assertEquals("/sandboxes/custom", request.getPath());
        assertEquals("POST", request.getMethod());
        JsonObject resources = parse(request).getAsJsonObject("resources");
        assertEquals(8, resources.get("vcpu").getAsInt());
        assertEquals(2048, resources.get("memory_mb").getAsLong());
        assertEquals(4096, resources.get("disk_size_mb").getAsLong());
    }

    @Test
    void createSendsOnlyTheResourcesTheCallerSet() throws Exception {
        startServer();
        server.enqueue(createdSandbox());

        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets().setMemoryGiB(2.0));
        create("base", opts);

        JsonObject resources = parse(takeRequest()).getAsJsonObject("resources");
        assertEquals(1, resources.size());
        assertEquals(2048, resources.get("memory_mb").getAsLong());
    }

    @Test
    void createKeepsATemplateDiskValueExpressedInMiB() throws Exception {
        startServer();
        server.enqueue(createdSandbox());

        SandboxOpts opts = options();
        // 2666 MiB is 2.603515625 GiB; the SDK forwards the exact MiB value.
        opts.setResources(new SandboxResourceTargets().setDiskGiB(2666 / 1024.0));
        create("base", opts);

        JsonObject resources = parse(takeRequest()).getAsJsonObject("resources");
        assertEquals(2666, resources.get("disk_size_mb").getAsLong());
    }

    @Test
    void createRejectsAnEmptyResourceObjectBeforeRequesting() throws Exception {
        startServer();
        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets());

        InvalidArgumentException error = assertThrows(InvalidArgumentException.class,
                () -> create("base", opts));
        assertTrue(error.getMessage().contains("at least one"));
        assertEquals(0, server.getRequestCount());
    }

    @Test
    void createRejectsGiBValuesThatAreNotExactMiB() throws Exception {
        startServer();
        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets().setMemoryGiB(1.1));

        InvalidArgumentException error = assertThrows(InvalidArgumentException.class,
                () -> create("base", opts));
        assertTrue(error.getMessage().contains("exact number of MiB"));
        assertEquals(0, server.getRequestCount());
    }

    @Test
    void createRejectsNonPositiveTargets() throws Exception {
        startServer();
        SandboxOpts cpu = options();
        cpu.setResources(new SandboxResourceTargets().setCpu(0));
        assertTrue(assertThrows(InvalidArgumentException.class, () -> create("base", cpu))
                .getMessage().contains("positive whole number"));

        SandboxOpts memory = options();
        memory.setResources(new SandboxResourceTargets().setMemoryGiB(0.0));
        assertTrue(assertThrows(InvalidArgumentException.class, () -> create("base", memory))
                .getMessage().contains("positive number"));
        assertEquals(0, server.getRequestCount());
    }

    @Test
    void createSurfacesTheBackendReasonForARejectedResource() throws Exception {
        startServer();
        server.enqueue(new MockResponse()
                .setResponseCode(400)
                .setHeader("Content-Type", "application/json")
                .setBody("{\"code\":400,\"message\":\"disk exceeds the sandbox limit\","
                        + "\"item\":\"disk\",\"reason\":\"exceeds_sandbox_limit\"}"));
        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets().setDiskGiB(6.0));

        SandboxException error = assertThrows(SandboxException.class, () -> create("base", opts));
        assertTrue(error.getMessage().contains("exceeds the sandbox limit"));
    }

    @Test
    void createRejectsResourceTotalsBeyondTheMiBLimit() throws Exception {
        startServer();
        SandboxOpts opts = options();
        opts.setResources(new SandboxResourceTargets().setDiskGiB(Math.scalb(1.0, 53)));

        InvalidArgumentException error = assertThrows(InvalidArgumentException.class,
                () -> create("base", opts));
        assertTrue(error.getMessage().contains("too large"));
    }

    @Test
    void createRejectsResourcesOnLegacyDomains() throws Exception {
        startServer();
        SandboxOpts opts = options();
        opts.setDomain("sandbox.novita.ai");
        opts.setResources(new SandboxResourceTargets().setCpu(2));

        SandboxException error = assertThrows(SandboxException.class, () -> create("base", opts));
        assertTrue(error.getMessage().contains("resources is not supported on legacy domains"));
        assertEquals(0, server.getRequestCount());
    }

    @Test
    void resizeConvertsGiBToMiBAndResultsBackToGiB() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "{\"cpu\":{\"status\":\"applied\",\"requested\":4,"
                + "\"previous\":2,\"current\":4,\"code\":200},"
                + "\"memory\":{\"status\":\"applied\",\"requested\":2048,"
                + "\"previous\":1024,\"current\":2048,\"code\":200},"
                + "\"disk\":{\"status\":\"applied\",\"requested\":4096,"
                + "\"previous\":2669,\"current\":4096,\"code\":200}}"));

        Sandbox sandbox = create("base", options());
        SandboxResizeResult result = sandbox.resize(
                new SandboxResourceTargets().setCpu(4).setMemoryGiB(2.0).setDiskGiB(4.0));

        RecordedRequest create = takeRequest();
        assertEquals("/sandboxes", create.getPath());
        RecordedRequest resize = takeRequest();
        assertEquals("/sandboxes/sb-1/resize", resize.getPath());
        assertEquals("POST", resize.getMethod());
        JsonObject body = parse(resize);
        assertEquals(4, body.get("vcpu").getAsInt());
        assertEquals(2048, body.get("memory_mb").getAsLong());
        assertEquals(4096, body.get("disk_size_mb").getAsLong());

        assertEquals("applied", result.getCpu().getStatus());
        assertEquals("cores", result.getCpu().getUnit());
        assertEquals(4.0, result.getCpu().getCurrent());
        assertEquals("GiB", result.getMemory().getUnit());
        assertEquals(2.0, result.getMemory().getRequested());
        assertEquals(1.0, result.getMemory().getPrevious());
        assertEquals(2.0, result.getMemory().getCurrent());
        assertEquals(2669 / 1024.0, result.getDisk().getPrevious(), 1e-9);
        assertEquals(4.0, result.getDisk().getCurrent());
    }

    @Test
    void resizeKeepsSkippedResourcesVisible() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "{\"cpu\":{\"status\":\"skipped\",\"requested\":2,"
                + "\"previous\":2,\"current\":2,\"code\":200,\"reason\":\"unchanged\"},"
                + "\"memory\":{\"status\":\"skipped\",\"requested\":2048,"
                + "\"previous\":2048,\"current\":2048,\"code\":200,\"reason\":\"unchanged\"}}"));

        Sandbox sandbox = create("base", options());
        SandboxResizeResult result = sandbox.resize(
                new SandboxResourceTargets().setCpu(2).setMemoryGiB(2.0));

        assertEquals("skipped", result.getCpu().getStatus());
        assertEquals("unchanged", result.getCpu().getReason());
        assertEquals(2.0, result.getMemory().getCurrent());
        assertNull(result.getDisk());
    }

    @Test
    void resizeReportsAPartial207WithoutThrowing() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(207, "{\"cpu\":{\"status\":\"applied\",\"requested\":6,"
                + "\"previous\":4,\"current\":6,\"code\":200},"
                + "\"memory\":{\"status\":\"failed\",\"requested\":9217,"
                + "\"previous\":1024,\"current\":1024,\"code\":400,"
                + "\"reason\":\"exceeds_sandbox_limit\","
                + "\"message\":\"requested 9217 MiB memory exceeds the maximum of 8192 MiB\"}}"));

        Sandbox sandbox = create("base", options());
        SandboxResizeResult result = sandbox.resize(
                new SandboxResourceTargets().setCpu(6).setMemoryGiB(9217 / 1024.0));

        assertEquals("applied", result.getCpu().getStatus());
        assertEquals("failed", result.getMemory().getStatus());
        assertEquals(400, result.getMemory().getCode());
        assertEquals("exceeds_sandbox_limit", result.getMemory().getReason());
        assertEquals(1.0, result.getMemory().getCurrent());
    }

    @Test
    void resizePreservesUnconfirmedTotalsAsNull() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(207, "{\"memory\":{\"status\":\"unknown\",\"requested\":2048,"
                + "\"previous\":null,\"current\":null,\"code\":207,\"reason\":\"record_pending\"}}"));

        Sandbox sandbox = create("base", options());
        SandboxResizeResult result = sandbox.resize(new SandboxResourceTargets().setMemoryGiB(2));

        assertEquals("unknown", result.getMemory().getStatus());
        assertEquals(2.0, result.getMemory().getRequested());
        assertNull(result.getMemory().getPrevious());
        assertNull(result.getMemory().getCurrent());
        assertEquals("record_pending", result.getMemory().getReason());
    }

    @Test
    void pauseSurfacesAResizeConflict() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(409, "{\"code\":409,\"message\":\"Sandbox sb-1 is being resized\"}"));

        Sandbox sandbox = create("base", options());
        SandboxException error = assertThrows(SandboxException.class, sandbox::pause);
        assertTrue(error.getMessage().contains("409"));
        assertTrue(error.getMessage().contains("being resized"));
    }

    @Test
    void pauseKeepsEscapedAlreadyPausedAsNoOp() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(409, "{\"code\":409,\"message\":\"Sandbox sb-1 is already\\u0020paused\"}"));

        Sandbox sandbox = create("base", options());
        assertFalse(sandbox.pause());
    }

    @Test
    void pauseSurfacesResizeConflictWhenAnotherFieldMentionsAlreadyPaused() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(409, "{\"code\":409,\"message\":\"Sandbox sb-1 is being resized\","
                + "\"previousError\":\"already paused\"}"));

        Sandbox sandbox = create("base", options());
        SandboxException error = assertThrows(SandboxException.class, sandbox::pause);
        assertTrue(error.getMessage().contains("being resized"));
    }

    @Test
    void pauseRaisesWhenTheConflictMessageIsUnusable() throws Exception {
        startServer();
        List<String> bodies = List.of(
                "{\"reason\":\"being resized\",\"previousError\":\"already paused\"}",
                "{\"message\":\"\",\"previousError\":\"already paused\"}",
                "{\"message\":null,\"previousError\":\"already paused\"}",
                "{\"message\":123,\"previousError\":\"already paused\"}");
        for (String body : bodies) {
            server.enqueue(createdSandbox());
            server.enqueue(json(409, body));
            Sandbox sandbox = create("base", options());
            SandboxException error = assertThrows(SandboxException.class, sandbox::pause);
            assertTrue(error.getMessage().contains("409"));
        }
    }

    @Test
    void pauseRejectsMalformedConflictJson() throws Exception {
        startServer();
        List<String> bodies = List.of(
                "{\"message\":\"Sandbox sb-1 is being resized\",\"previousError\":\"already paused\"",
                "{'message':'already paused'}",
                "{message:\"already paused\"}",
                "{\"message\":\"already paused\"/*comment*/}",
                "{\"message\":\"already paused\",\"extra\":TRUE}",
                "{\"message\":\"already paused\n\"}");
        for (String body : bodies) {
            server.enqueue(createdSandbox());
            server.enqueue(json(409, body));
            Sandbox sandbox = create("base", options());
            SandboxException error = assertThrows(SandboxException.class, sandbox::pause);
            assertTrue(error.getMessage().contains("409"));
        }
    }

    @Test
    void resizeWrapsMalformedResponsesAsSdkErrors() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "not json"));

        Sandbox sandbox = create("base", options());
        SandboxException error = assertThrows(SandboxException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(2)));
        assertTrue(error.getMessage().contains("resize response"));
    }

    @Test
    void resizeWrapsWrongShapedResponsesAsSdkErrors() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "[]"));

        Sandbox sandbox = create("base", options());
        SandboxException error = assertThrows(SandboxException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(2)));
        assertTrue(error.getMessage().contains("resize response"));
    }

    @Test
    void createWithResourcesHonorsAnExplicitRequestTimeout() throws Exception {
        startServer();
        server.enqueue(createdSandbox().setBodyDelay(2, TimeUnit.SECONDS));

        SandboxOpts opts = options();
        opts.setRequestTimeoutSeconds(1);
        opts.setResources(new SandboxResourceTargets().setCpu(2));
        SandboxException error = assertThrows(SandboxException.class,
                () -> create("base", opts));
        assertTrue(error.getMessage().toLowerCase().contains("create"));
    }

    @Test
    void ordinaryCreateHonorsAnExplicitRequestTimeout() throws Exception {
        startServer();
        server.enqueue(createdSandbox().setBodyDelay(2, TimeUnit.SECONDS));

        SandboxOpts opts = options();
        opts.setRequestTimeoutSeconds(1);
        SandboxException error = assertThrows(SandboxException.class,
                () -> create("base", opts));
        assertTrue(error.getMessage().toLowerCase().contains("create"));
    }

    @Test
    void resizeRejectsAResponseWithoutTheRequestedOutcome() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "{}"));

        Sandbox sandbox = create("base", options());
        SandboxException error = assertThrows(SandboxException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(2)));
        assertTrue(error.getMessage().contains("cpu"));
    }

    @Test
    void resizeRejectsInvalidTargetsBeforeRequesting() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        Sandbox sandbox = create("base", options());

        assertThrows(InvalidArgumentException.class,
                () -> sandbox.resize(new SandboxResourceTargets()));
        assertThrows(InvalidArgumentException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(1).setDiskGiB(1.1)));
        assertThrows(InvalidArgumentException.class, () -> sandbox.resize(null));
        assertEquals(1, server.getRequestCount());
    }

    @Test
    void resizeSurfacesAMissingSandboxAsNotFound() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(404, "{\"code\":404,\"message\":\"Sandbox \\\"sb-1\\\" doesn't exist\"}"));
        Sandbox sandbox = create("base", options());

        assertThrows(NotFoundException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(2)));
    }

    @Test
    void resizeSurfacesInvalidCredentialsAsUnauthorized() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(new MockResponse().setResponseCode(401).setBody("unauthorized"));
        Sandbox sandbox = create("base", options());

        assertThrows(AuthenticationException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(2)));
    }

    @Test
    void resizeSurfacesAnOperationAlreadyInProgress() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(409, "{\"code\":409,\"message\":\"another resize is in progress\"}"));
        Sandbox sandbox = create("base", options());

        SandboxException error = assertThrows(SandboxException.class,
                () -> sandbox.resize(new SandboxResourceTargets().setCpu(4)));
        assertTrue(error.getMessage().contains("409"));
        assertTrue(error.getMessage().contains("another resize is in progress"));
    }

    @Test
    void resizeAsyncReturnsTheResourceResult() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "{\"cpu\":{\"status\":\"applied\",\"requested\":2,"
                + "\"previous\":1,\"current\":2,\"code\":200}}"));
        Sandbox sandbox = create("base", options());

        SandboxResizeResult result = sandbox.resizeAsync(
                new SandboxResourceTargets().setCpu(2)).get(5, TimeUnit.SECONDS);
        assertNotNull(result.getCpu());
        assertEquals(2.0, result.getCpu().getCurrent());
    }

    @Test
    void getInfoExposesTheCurrentCPUAndMemoryTotals() throws Exception {
        startServer();
        server.enqueue(createdSandbox());
        server.enqueue(json(200, "{\"sandboxID\":\"sb-1\",\"templateID\":\"base\","
                + "\"state\":\"running\",\"envdVersion\":\"0.5.14\","
                + "\"cpuCount\":2,\"memoryMB\":8192,\"diskSizeMB\":40960}"));
        Sandbox sandbox = create("base", options());

        SandboxInfo info = sandbox.info();
        assertEquals(2, info.getCpuCount());
        assertEquals(8192, info.getMemoryMB());
        assertEquals(40960, info.getDiskSizeMB());
    }

    private SandboxOpts options() {
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        return opts;
    }

    private void startServer() throws Exception {
        server = new MockWebServer();
        server.start();
    }

    private RecordedRequest takeRequest() throws Exception {
        RecordedRequest request = server.takeRequest(5, TimeUnit.SECONDS);
        assertNotNull(request);
        return request;
    }

    private static MockResponse createdSandbox() {
        return json(201, "{\"sandboxID\":\"sb-1\",\"templateID\":\"base\","
                + "\"domain\":\"us-phx-1.sandbox.novita.ai\",\"envdVersion\":\"0.5.7\"}");
    }

    private static MockResponse json(int status, String body) {
        return new MockResponse()
                .setResponseCode(status)
                .setHeader("Content-Type", "application/json")
                .setBody(body);
    }

    private static JsonObject parse(RecordedRequest request) {
        return JsonParser.parseString(request.getBody().readUtf8()).getAsJsonObject();
    }
}
