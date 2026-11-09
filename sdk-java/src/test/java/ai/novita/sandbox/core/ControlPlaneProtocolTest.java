package ai.novita.sandbox.core;

import com.google.gson.JsonElement;
import com.google.gson.JsonObject;
import com.google.gson.JsonParser;
import okhttp3.mockwebserver.MockResponse;
import okhttp3.mockwebserver.MockWebServer;
import okhttp3.mockwebserver.RecordedRequest;
import org.junit.jupiter.api.AfterEach;
import org.junit.jupiter.api.Test;

import java.util.LinkedHashMap;
import java.util.Map;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertEquals;
import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertNotNull;
import static org.junit.jupiter.api.Assertions.assertThrows;
import static org.junit.jupiter.api.Assertions.assertTrue;

/**
 * Wire-format tests for the control-plane requests the Java SDK shares with
 * the JS and Python SDKs: create, connect, reset, volume mounts, clone,
 * commit, and the sandbox event log.
 */
class ControlPlaneProtocolTest {
    private MockWebServer server;

    @AfterEach
    void stopServer() throws Exception {
        if (server != null) {
            server.shutdown();
        }
    }

    @Test
    void createSandboxSendsSpecBody() throws Exception {
        startServer();
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"domain\":\"modern.example.com\","
                + "\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"token\"}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        Map<String, String> metadata = new LinkedHashMap<>();
        metadata.put("team", "dev");
        opts.setMetadata(metadata);
        Map<String, String> envVars = new LinkedHashMap<>();
        envVars.put("FOO", "bar");
        opts.setEnvVars(envVars);
        opts.setTimeout(600);
        opts.setAutoPause(false);
        opts.setAutoResumeEnabled(true);
        opts.setSecure(true);
        opts.setAllowInternetAccess(false);
        opts.setNodeId("node-1");
        Map<String, String> secretEnvs = new LinkedHashMap<>();
        secretEnvs.put("DB_PASSWORD", "secret-vol:KEY");
        opts.setSecretEnvs(secretEnvs);
        Map<String, String> volumeMounts = new LinkedHashMap<>();
        volumeMounts.put("/data", "data-vol");
        opts.setVolumeMounts(volumeMounts);
        Map<String, Object> mcp = new LinkedHashMap<>();
        mcp.put("enabled", true);
        opts.setMcp(mcp);

        Sandbox sandbox = Sandbox.create("tpl-1", opts);

        assertEquals("sb-1", sandbox.getId());
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes", request.getPath());
        assertEquals("api-key", request.getHeader("X-API-Key"));
        JsonObject body = parse(request);
        assertEquals("tpl-1", body.get("templateID").getAsString());
        assertEquals("bar", body.getAsJsonObject("envVars").get("FOO").getAsString());
        assertEquals("dev", body.getAsJsonObject("metadata").get("team").getAsString());
        assertEquals(600, body.get("timeout").getAsInt());
        assertEquals(false, body.get("autoPause").getAsBoolean());
        assertTrue(body.getAsJsonObject("autoResume").get("enabled").getAsBoolean());
        assertEquals(true, body.get("secure").getAsBoolean());
        assertEquals(false, body.get("allow_internet_access").getAsBoolean());
        assertEquals("node-1", body.get("nodeID").getAsString());
        assertEquals("secret-vol:KEY", body.getAsJsonObject("secrets").get("DB_PASSWORD").getAsString());
        assertEquals("data-vol", body.getAsJsonArray("volumeMounts").get(0).getAsJsonObject().get("name").getAsString());
        assertEquals("/data", body.getAsJsonArray("volumeMounts").get(0).getAsJsonObject().get("path").getAsString());
        assertTrue(body.getAsJsonObject("mcp").get("enabled").getAsBoolean());
    }

    @Test
    void connectSandboxSendsSecureAndPublicTraffic() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");

        SandboxOpts opts = connectOpts();
        opts.setSecure(true);
        opts.setAllowPublicTraffic(true);
        Sandbox.connect("sb-1", opts);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/connect", request.getPath());
        JsonObject body = parse(request);
        assertEquals(true, body.get("secure").getAsBoolean());
        assertEquals(true, body.get("allowPublicTraffic").getAsBoolean());
        assertFalse(body.has("network"));
    }

    @Test
    void resetSendsResumeTrueByDefault() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");
        server.enqueue(json("{\"success\":true}"));

        connectSandbox().reset();

        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/reset", request.getPath());
        JsonObject body = parse(request);
        assertEquals(true, body.get("resume").getAsBoolean());
        assertEquals(1, body.size());
    }

    @Test
    void resetSendsOptionsBody() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");
        server.enqueue(json("{\"success\":true,\"trafficAccessToken\":\"rotated-traffic\"}"));

        ResetOptions options = new ResetOptions();
        options.setResume(false);
        options.setSecure(true);
        options.setAllowPublicTraffic(true);
        options.setTimeoutSeconds(600);
        connectSandbox().reset(options);

        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/reset", request.getPath());
        JsonObject body = parse(request);
        assertEquals(false, body.get("resume").getAsBoolean());
        assertEquals(true, body.get("secure").getAsBoolean());
        assertEquals(true, body.get("allowPublicTraffic").getAsBoolean());
        assertEquals(600, body.get("timeout").getAsInt());
        assertFalse(body.has("network"));
        assertFalse(body.has("templateID"));
        assertFalse(body.has("image"));
    }

    @Test
    void mountVolumePostsVolumeMountsEndpoint() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"state\":\"RUNNING\"}"));

        SandboxInfo info = connectSandbox().mountVolume("data-vol", "/data");

        assertEquals("sb-1", info.getId());
        assertEquals("RUNNING", info.getState());
        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/volume-mounts", request.getPath());
        assertEquals("POST", request.getMethod());
        JsonObject body = parse(request);
        assertEquals("data-vol", body.get("name").getAsString());
        assertEquals("/data", body.get("path").getAsString());
    }

    @Test
    void unmountVolumeDeletesWithForceQuery() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"state\":\"PAUSED\"}"));

        SandboxInfo info = connectSandbox().unmountVolume("/data", true);

        assertEquals("PAUSED", info.getState());
        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/volume-mounts?path=%2Fdata&force=true", request.getPath());
        assertEquals("DELETE", request.getMethod());
    }

    @Test
    void volumeMountsAreRejectedOnLegacyControlPlanes() throws Exception {
        // The rejection follows the configured control-plane domain, matching
        // JS/Python raiseIfLegacy(config, ...).
        ApiClient client = new ApiClient("api-key", "https://api.sandbox.novita.ai", 30);
        try {
            SandboxInfo info = new SandboxInfo();
            info.setDomain("sandbox.novita.ai");
            Sandbox sandbox = new Sandbox("sb-1", client, info);
            assertThrows(SandboxException.class, () -> sandbox.mountVolume("data-vol", "/data"));
            assertThrows(SandboxException.class, () -> sandbox.unmountVolume("/data", false));
        } finally {
            client.close();
        }
    }

    @Test
    void cloneSendsSpecBodyAndParsesResponse() throws Exception {
        startServer();
        server.enqueue(json("{\"sandboxes\":[{\"sandboxID\":\"sb-a\",\"clientID\":\"c1\","
                + "\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"envd-token\","
                + "\"trafficAccessToken\":\"traffic-token\"}],\"snapshotTemplateId\":\"tpl-9\"}"));
        server.enqueue(json("{}"));

        CloneOptions options = new CloneOptions();
        options.setCount(2);
        options.setStrict(true);
        options.setNodeId("node-1");
        options.setTimeoutSeconds(120);
        CloneResult result = Sandbox.clone("sb-1", options, cloneOpts());

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/clone", request.getPath());
        JsonObject body = parse(request);
        assertEquals(2, body.get("count").getAsInt());
        assertEquals(true, body.get("strict").getAsBoolean());
        assertEquals("node-1", body.get("nodeID").getAsString());
        assertEquals(120, body.get("timeout").getAsInt());

        assertEquals(1, result.getCount());
        assertEquals("tpl-9", result.getSnapshotTemplateId());
        assertEquals(1, result.getSandboxes().length);
        Sandbox clone = result.getSandboxes()[0];
        // JS/Python concatenate the client ID only on legacy control planes;
        // this mock server is a modern control plane, so the ID stays as-is.
        assertEquals("sb-a", clone.getId());
        // Each clone owns its control-plane client and can act independently.
        clone.kill();
        RecordedRequest killRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-a", killRequest.getPath());
        assertEquals("DELETE", killRequest.getMethod());
    }

    @Test
    void cloneConcatenatesClientIDOnlyOnLegacyControlPlanes() throws Exception {
        startServer();
        com.google.gson.JsonObject response = new com.google.gson.JsonObject();
        com.google.gson.JsonArray items = new com.google.gson.JsonArray();
        com.google.gson.JsonObject item = new com.google.gson.JsonObject();
        item.addProperty("sandboxID", "sb-a");
        item.addProperty("clientID", "c1");
        items.add(item);
        response.add("sandboxes", items);
        final String baseUrl = server.url("/").toString();

        // Legacy control plane: sandboxID-clientID, like JS/Python.
        CloneResult legacy = Sandbox.buildCloneResult(response,
                () -> new ApiClient("k", baseUrl, 30), "sandbox.novita.ai", true);
        assertEquals("sb-a-c1", legacy.getSandboxes()[0].getId());
        server.enqueue(json("{}"));
        legacy.getSandboxes()[0].kill();
        RecordedRequest killRequest = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-a-c1", killRequest.getPath());
    }

    @Test
    void legacyDetectionFollowsConfiguredDomainNotBaseUrl() throws Exception {
        startServer();
        // JS/Python derive legacy behavior from the configured domain
        // (config.isLegacyDomain), never from the API URL: a legacy domain
        // behind a custom base URL still concatenates clientID, and a modern
        // domain never does even when the response carries a clientID.
        SandboxOpts legacyOpts = new SandboxOpts();
        legacyOpts.setApiKey("api-key");
        legacyOpts.setDomain("sandbox.novita.ai");
        legacyOpts.setBaseUrl(server.url("/").toString());
        server.enqueue(json("{\"sandboxID\":\"sb-x\",\"clientID\":\"c9\","
                + "\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"envd-t\"}"));
        Sandbox legacy = Sandbox.create("tpl-1", legacyOpts);
        assertEquals("sb-x-c9", legacy.getId());

        SandboxOpts modernOpts = new SandboxOpts();
        modernOpts.setApiKey("api-key");
        modernOpts.setDomain("us-phx-1.sandbox.novita.ai");
        modernOpts.setBaseUrl(server.url("/").toString());
        server.enqueue(json("{\"sandboxID\":\"sb-y\",\"clientID\":\"c8\","
                + "\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"envd-t\"}"));
        Sandbox modern = Sandbox.create("tpl-1", modernOpts);
        assertEquals("sb-y", modern.getId());
    }

    @Test
    void commitIsRejectedOnModernDomains() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");

        assertThrows(SandboxException.class, () -> connectSandbox().commit("my-alias"));
    }

    @Test
    void commitSendsAliasOnLegacyDomains() throws Exception {
        startServer();
        enqueueConnectInfo("sandbox.novita.ai", "0.5.7");
        server.enqueue(json("{\"templateID\":\"tpl-77\",\"name\":\"snapshot\",\"status\":\"READY\"}"));

        SandboxOpts legacyOpts = connectOpts();
        legacyOpts.setDomain("sandbox.novita.ai");
        TemplateInfo info = Sandbox.connect("sb-1", legacyOpts).commit("my-alias");

        assertEquals("tpl-77", info.getTemplateId());
        // JS: toTemplateInfo(data, 'snapshot_template') defaults the type.
        assertEquals("snapshot_template", info.getTemplateType());
        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/commit", request.getPath());
        JsonObject body = parse(request);
        assertEquals("my-alias", body.get("alias").getAsString());
    }

    @Test
    void getEventsSendsQueryParameters() throws Exception {
        startServer();
        server.enqueue(json("{\"items\":[{\"eventID\":\"e-1\",\"recordAt\":1700000000,"
                + "\"templateID\":\"tpl-1\",\"templateName\":\"base\",\"sandboxID\":\"sb-1\","
                + "\"eventName\":\"created\",\"state\":\"COMPLETED\",\"errorMsg\":\"\","
                + "\"statusCode\":200}],\"total\":1,\"hasMore\":false}"));

        SandboxEventsOptions options = new SandboxEventsOptions();
        options.setApiKey("api-key");
        options.setBaseUrl(server.url("/").toString());
        options.setStartTime(1699999000L);
        options.setEndTime(1700001000L);
        options.setSandboxID("sb-1");
        options.setTemplateID("tpl-1");
        options.setEvents("created");
        options.setState("COMPLETED");
        options.setOrderAsc(true);
        options.setOffset(0);
        options.setLimit(10);
        SandboxEventsResult result = Sandbox.getEvents(options);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("GET", request.getMethod());
        assertEquals("/sandboxes/events"
                + "?startTime=1699999000&endTime=1700001000&sandboxID=sb-1&templateID=tpl-1"
                + "&events=created&state=COMPLETED&orderAsc=true&offset=0&limit=10",
                request.getPath());
        assertEquals(1, result.getTotal());
        assertFalse(result.isHasMore());
        assertEquals(1, result.getItems().length);
        assertEquals("e-1", result.getItems()[0].getEventID());
        assertEquals("created", result.getItems()[0].getEventName());
    }

    @Test
    void signatureMatchesReferenceVectors() {
        // sha256("/etc/hosts:read:root:token") -> v1_2E0EPM+...
        Signature.Result read = Signature.get("/etc/hosts", "read", "root", "token", null);
        assertEquals("v1_2E0EPM+9lzDMapNl187EQ7tAyoxuwPzB0OF3MhlcZPA", read.signature);
        assertEquals(null, read.expiration);
        // null user is signed as the empty string.
        Signature.Result write = Signature.get("/work/file.txt", "write", null, "token", null);
        assertEquals("v1_DLwaM1w0gILZVkEoVvyMreiW1CGIspNJ8SbJpEPin6k", write.signature);
    }

    @Test
    void signatureExpirationSetsUnixTimestamp() {
        long before = System.currentTimeMillis() / 1000L;
        Signature.Result result = Signature.get("/etc/hosts", "read", "root", "token", 60);
        assertNotNull(result.expiration);
        assertTrue(result.expiration >= before + 60 && result.expiration <= before + 61);
    }

    @Test
    void downloadUrlSignsSecureSandboxFiles() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");

        String url = connectSandbox().downloadUrl("/etc/hosts", "root", null);

        // '+' in the base64 signature is percent-encoded, matching
        // URLSearchParams (JS) and quote_via=quote (Python).
        assertEquals(server.url("/").toString().replaceAll("/+$", "")
                + "/files?path=%2Fetc%2Fhosts&username=root"
                + "&signature=v1_2E0EPM%2B9lzDMapNl187EQ7tAyoxuwPzB0OF3MhlcZPA",
                url);
    }

    @Test
    void downloadUrlDefaultsLegacyEnvdUser() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.1.4");

        String url = connectSandbox().downloadUrl("/etc/hosts", null, null);

        // envd < 0.4.0 must request the default "user" explicitly.
        assertTrue(url.contains("username=user"));
    }

    @Test
    void downloadUrlRejectsExpirationWithoutToken() throws Exception {
        startServer();
        enqueueConnectInfoNoToken(server.url("/").toString(), "0.5.7");

        SandboxException error = assertThrows(SandboxException.class,
                () -> connectSandbox().downloadUrl("/etc/hosts", "root", 60));
        assertTrue(error.getMessage().contains("secured"));
    }

    @Test
    void isRunningTreats502AsStopped() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(new MockResponse().setResponseCode(502));
        server.enqueue(json("{\"status\":\"ok\"}"));

        Sandbox sandbox = connectSandbox();
        assertFalse(sandbox.isRunning());
        assertTrue(sandbox.isRunning());

        discardConnectRequest();
        RecordedRequest health = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/health", health.getPath());
        assertEquals("token", health.getHeader("X-Access-Token"));
    }

    @Test
    void getMcpUrlUsesMcpPortHost() throws Exception {
        startServer();
        enqueueConnectInfo("modern.example.com", "0.5.7");

        String url = connectSandbox().getMcpUrl();

        assertEquals("https://50005-sb-1.modern.example.com/mcp", url);
    }

    @Test
    void getMcpUrlIsRejectedOnLegacyControlPlanes() throws Exception {
        // JS/Python raiseIfLegacy checks the configured control-plane domain,
        // not the sandbox's runtime domain, so a legacy cluster that omits
        // the domain from its response still rejects modern-only operations.
        ApiClient client = new ApiClient("api-key", "https://api.sandbox.novita.ai", 30);
        try {
            SandboxInfo info = new SandboxInfo();
            info.setDomain("sandbox.novita.ai");
            Sandbox sandbox = new Sandbox("sb-1", client, info);
            assertThrows(SandboxException.class, sandbox::getMcpUrl);
            assertThrows(SandboxException.class, sandbox::createSnapshot);
            assertThrows(SandboxException.class, () -> sandbox.setNetwork(new NetworkOptions()));
            assertThrows(SandboxException.class, () -> sandbox.mountVolume("v", "/data"));
        } finally {
            client.close();
        }
    }

    @Test
    void getMcpTokenReadsGatewayTokenAsRoot() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        // The token file is read through the envd /files endpoint as root.
        server.enqueue(new MockResponse().setResponseCode(200).setBody("mcp-token-value"));

        String token = connectSandbox().getMcpToken();

        assertEquals("mcp-token-value", token);
        discardConnectRequest();
        RecordedRequest read = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("GET", read.getMethod());
        assertEquals("/files?path=%2Fetc%2Fmcp-gateway%2F.token&username=root", read.getPath());
    }

    @Test
    void commandsRunSendsUserOption() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(connectStream("{\"event\":{\"start\":{\"pid\":7}}}", "{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"));

        CommandOpts opts = new CommandOpts();
        opts.setUser("root");
        connectSandbox().commands().run("ls", opts);

        discardConnectRequest();
        RecordedRequest start = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("Basic cm9vdDo=", start.getHeader("Authorization"));
        JsonObject body = parseEnvelope(start);
        // The public run() API must go through the shell-wrapped StartRequest.
        assertEquals("/bin/bash", body.getAsJsonObject("process").get("cmd").getAsString());
    }

    @Test
    void createSandboxDefaultsSecureAndInternetAccess() throws Exception {
        startServer();
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"domain\":\"modern.example.com\","
                + "\"envdVersion\":\"0.5.7\",\"envdAccessToken\":\"token\"}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        Sandbox.create("tpl-1", opts);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        JsonObject body = parse(request);
        // Modern clusters default both to true, matching JS/Python.
        assertEquals(true, body.get("secure").getAsBoolean());
        assertEquals(true, body.get("allow_internet_access").getAsBoolean());
        assertEquals(300, body.get("timeout").getAsInt());
    }

    @Test
    void createSandboxKillsStaleTemplates() throws Exception {
        startServer();
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"envdVersion\":\"0.0.9\"}"));
        server.enqueue(json("{}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        TemplateException error = assertThrows(TemplateException.class, () -> Sandbox.create("tpl-1", opts));
        assertTrue(error.getMessage().contains("update the template"));

        // Request 1: POST /sandboxes (stale template). Request 2: the cleanup kill.
        server.takeRequest(1, TimeUnit.SECONDS);
        RecordedRequest kill = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1", kill.getPath());
        assertEquals("DELETE", kill.getMethod());
    }

    @Test
    void listSendsV2QueryParametersAndPagingHeader() throws Exception {
        startServer();
        server.enqueue(new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setHeader("x-next-token", "next-page-token")
                .setBody("[{\"sandboxID\":\"sb-1\",\"templateID\":\"tpl-1\",\"state\":\"RUNNING\","
                        + "\"alias\":\"my-sandbox\",\"startedAt\":\"2026-01-01T00:00:00Z\","
                        + "\"cpuCount\":2,\"memoryMB\":1024,\"envdVersion\":\"0.5.7\"}]"));

        SandboxListOptions options = new SandboxListOptions();
        options.setApiKey("api-key");
        options.setBaseUrl(server.url("/").toString());
        options.setQueryMetadata(java.util.Collections.singletonMap("team", "dev ops"));
        options.setStates(java.util.Arrays.asList("running", "paused"));
        options.setLimit(50);
        options.setNextToken("prev-token");
        SandboxListResult result = Sandbox.list(options);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/v2/sandboxes"
                + "?metadata=team%3Ddev%2520ops"
                + "&state=running%2Cpaused&limit=50&nextToken=prev-token",
                request.getPath());
        assertEquals(1, result.getItems().length);
        assertEquals("sb-1", result.getItems()[0].getId());
        assertEquals("my-sandbox", result.getItems()[0].getAlias());
        assertEquals(Integer.valueOf(2), result.getItems()[0].getCpuCount());
        assertEquals("next-page-token", result.getNextToken());
        assertTrue(result.isHasMore());
    }

    @Test
    void listAppendsClientIDOnLegacyClusters() {
        SandboxInfo info = new com.google.gson.Gson().fromJson(
                "{\"sandboxID\":\"sb-1\",\"clientID\":\"c1\",\"state\":\"RUNNING\"}", SandboxInfo.class);

        ApiClient.appendLegacyClientIds(new SandboxInfo[]{info});

        // JS/Python concatenate the client ID onto the sandbox ID only when
        // the control plane is a legacy cluster.
        assertEquals("sb-1-c1", info.getId());
    }

    @Test
    void setNetworkSendsPutWithOnlyExplicitRules() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(json("{}"));

        NetworkOptions network = new NetworkOptions();
        network.setAllowOut(java.util.Arrays.asList("github.com"));
        connectSandbox().setNetwork(network);

        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("PUT", request.getMethod());
        assertEquals("/sandboxes/sb-1/network", request.getPath());
        JsonObject body = parse(request);
        assertEquals("github.com", body.getAsJsonArray("allowOut").get(0).getAsString());
        assertFalse(body.has("denyOut"));
        assertFalse(body.has("allowPublicTraffic"));
    }

    @Test
    void pauseReportsFalseOnConflict() throws Exception {
        startServer();
        // Modern: 409 means already paused, reported as false like JS/Python.
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(new MockResponse().setResponseCode(409));

        assertFalse(connectSandbox().pause());

        // JS modern pause POSTs with no request body.
        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("POST", request.getMethod());
        assertEquals("/sandboxes/sb-1/pause", request.getPath());
        assertEquals(0, request.getBodySize());
    }

    @Test
    void killReportsFalseWhenAlreadyGone() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(new MockResponse().setResponseCode(404));

        assertFalse(connectSandbox().kill());
    }

    @Test
    void resetReportsFalseOnConflict() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(new MockResponse().setResponseCode(409));

        assertFalse(connectSandbox().reset());
    }

    @Test
    void resetReportsTrueForAnySuccessIncludingEmptyBody() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        // JS/Python derive success from the status alone; legacy clusters can
        // answer 200 with an empty body.
        server.enqueue(new MockResponse().setResponseCode(200));
        assertTrue(connectSandbox().reset());

        // A literal JSON "null" body parses to null but is still a 2xx success.
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(json("null"));
        assertTrue(connectSandbox().reset());

        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(json("{\"trafficAccessToken\":\"rotated\"}"));
        assertTrue(connectSandbox().reset());
    }

    @Test
    void templateGetDerivesLatestBuildFromBuildsArray() throws Exception {
        startServer();
        // JS/Python Template.get: buildID/buildStatus come from builds[0],
        // unconditionally overriding any top-level values.
        server.enqueue(json("{\"templateID\":\"tpl-1\",\"buildID\":\"stale\","
                + "\"buildStatus\":\"old\",\"builds\":[{\"buildID\":\"b-9\",\"status\":\"ready\"}],"
                + "\"createdBy\":{\"id\":\"u-1\",\"email\":\"dev@example.com\"}}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        TemplateInfo info;
        try {
            info = new Template(client).get("tpl-1");
        } finally {
            client.close();
        }

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/templates/tpl-1", request.getPath());
        assertEquals("b-9", info.getBuildId());
        assertEquals("ready", info.getBuildStatus());
        // createdBy is an object; a String field would make Gson fail outright.
        assertEquals("u-1", info.getCreatedBy().getId());
        assertEquals("dev@example.com", info.getCreatedBy().getEmail());
    }

    @Test
    void buildInBackgroundTriggerBodyAlwaysCarriesStepsAndForce() throws Exception {
        startServer();
        server.enqueue(json("{\"templateID\":\"tpl-1\",\"buildID\":\"b-1\"}"));
        server.enqueue(json("{}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        try {
            new Template(client).buildInBackground("my-env", null);
        } finally {
            client.close();
        }

        RecordedRequest allocate = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/v3/templates", allocate.getPath());
        RecordedRequest trigger = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/v2/templates/tpl-1/builds/b-1", trigger.getPath());
        // JS/Python serialize() always emit steps and force, empty and false included.
        JsonObject body = parse(trigger);
        assertEquals(0, body.getAsJsonArray("steps").size());
        assertEquals(false, body.get("force").getAsBoolean());
    }

    @Test
    void noProxyWildcardBypassesEverything() {
        java.util.List<String> wildcard = java.util.Collections.singletonList("*");
        assertTrue(ApiClient.isBypassed("api.us-phx-1.sandbox.novita.ai", wildcard));
        assertTrue(ApiClient.isBypassed("anything.example.org", wildcard));

        java.util.List<String> entries = java.util.Arrays.asList("localhost", ".internal");
        assertTrue(ApiClient.isBypassed("localhost", entries));
        assertTrue(ApiClient.isBypassed("host.internal", entries));
        assertFalse(ApiClient.isBypassed("api.novita.ai", entries));
    }

    @Test
    void legacyApiDomainDetectionMatchesClusterList() throws Exception {
        checkLegacyMode("sandbox.novita.ai", true);
        checkLegacyMode("us-ga-1.sandbox.novita.ai", true);
        checkLegacyMode("us-01.sandbox.novita.ai", false);
        checkLegacyMode("us-ga-01.sandbox.novita.ai", false);
        checkLegacyMode("us-phx-1.sandbox.novita.ai", false);
        checkLegacyMode("preview.example.invalid", false);
        checkLegacyMode("dev.example.invalid", false);
    }

    private static void checkLegacyMode(String domain, boolean legacy) throws Exception {
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("test-key");
        opts.setDomain(domain);
        opts.setBaseUrl("https://api.modern.example.invalid");
        assertEquals(legacy ? 1800 : 60, opts.getRequestTimeoutSeconds());
        try (ApiClient client = new ApiClient(opts)) {
            assertEquals(legacy, client.isLegacyApiDomain(), domain);
            assertEquals(legacy ? 1800 : 60, client.requestTimeoutSeconds());
        }
        try (ApiClient client = new ApiClient("test-key", "https://api." + domain, 42)) {
            assertEquals(legacy, client.isLegacyApiDomain(), domain);
            assertEquals(42, client.requestTimeoutSeconds());
        }
    }

    @Test
    void legacyModeCanBeOverriddenWithoutPublishingDomainNames() throws Exception {
        for (String mode : new String[] {"true", " TRUE ", "false", " FALSE ", "auto", ""}) {
            ProcessBuilder process = new ProcessBuilder(
                    java.nio.file.Path.of(System.getProperty("java.home"), "bin", "java").toString(),
                    "-cp", System.getProperty("surefire.test.class.path", System.getProperty("java.class.path")),
                    LegacyModeProbe.class.getName(), mode);
            process.environment().put("NOVITA_IS_LEGACY", mode);
            process.redirectErrorStream(true);
            Process child = process.start();
            assertTrue(child.waitFor(20, TimeUnit.SECONDS), "Legacy mode probe timed out");
            String output = new String(child.getInputStream().readAllBytes(), java.nio.charset.StandardCharsets.UTF_8);
            assertEquals(0, child.exitValue(), output);
        }
    }

    public static class LegacyModeProbe {
        public static void main(String[] args) throws Exception {
            String mode = args[0].trim().toLowerCase(java.util.Locale.ROOT);
            checkLegacyMode("private.example.invalid", mode.equals("true"));
            checkLegacyMode("sandbox.novita.ai", !mode.equals("false"));
        }
    }

    @Test
    void volumeUpdateQuotaSendsPatchWithDerivedInodes() throws Exception {
        startServer();
        server.enqueue(json("{\"volumeID\":\"vol-1\",\"name\":\"data\",\"token\":\"t\"}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        try {
            new Volume(client).updateQuota("vol-1", 5L);
        } finally {
            client.close();
        }

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("PATCH", request.getMethod());
        assertEquals("/volumes/vol-1", request.getPath());
        JsonObject body = parse(request);
        assertEquals(5, body.get("quotaSizeGiB").getAsInt());
        assertEquals(5000, body.get("quotaInodes").getAsInt());
    }

    @Test
    void volumeUpdateQuotaWithoutQuotaSendsEmptyBody() throws Exception {
        startServer();
        server.enqueue(json("{\"volumeID\":\"vol-1\",\"name\":\"data\",\"token\":\"t\"}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        try {
            // JS/Python send {} when quotaSizeGiB is undefined / None.
            new Volume(client).updateQuota("vol-1", null);
        } finally {
            client.close();
        }

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("PATCH", request.getMethod());
        assertEquals("/volumes/vol-1", request.getPath());
        JsonObject body = parse(request);
        assertFalse(body.has("quotaSizeGiB"));
        assertFalse(body.has("quotaInodes"));
    }

    @Test
    void secretListParsesEnvelope() throws Exception {
        startServer();
        server.enqueue(json("{\"secrets\":[{\"name\":\"s1\",\"placeholder\":\"{{s1}}\","
                + "\"hasSecret\":true,\"status\":\"ready\"}]}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        SecretBinding[] secrets;
        try {
            secrets = new Secret(client).list();
        } finally {
            client.close();
        }

        assertEquals(1, secrets.length);
        assertEquals("s1", secrets[0].getName());
    }

    @Test
    void secretUpdateKeepsNameOutOfBody() throws Exception {
        startServer();
        server.enqueue(json("{\"name\":\"s1\",\"placeholder\":\"{{s1}}\",\"hasSecret\":true,\"status\":\"ready\"}"));

        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        ApiClient client = new ApiClient(opts.getApiKey(), opts.resolvedBaseUrl(), 30);
        try {
            new Secret(client).update(new SecretOptions()
                    .name("s1").value("v").hosts(java.util.Arrays.asList("Api.Example.COM")));
        } finally {
            client.close();
        }

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("PUT", request.getMethod());
        assertEquals("/secrets/s1", request.getPath());
        JsonObject body = parse(request);
        assertFalse(body.has("name"));
        assertEquals("api.example.com", body.getAsJsonArray("hosts").get(0).getAsString());
    }

    @Test
    void createSnapshotSendsEmptyBodyAndParsesSnapshotID() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(json("{\"snapshotID\":\"snap-1\"}"));

        SnapshotInfo snapshot = connectSandbox().createSnapshot();

        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/snapshots", request.getPath());
        assertEquals("{}", request.getBody().readUtf8());
        assertEquals("snap-1", snapshot.getSnapshotId());
    }

    @Test
    void listSnapshotsPaginatesThroughNextTokenHeader() throws Exception {
        startServer();
        server.enqueue(json("[{\"snapshotID\":\"snap-1\"}]").setHeader("x-next-token", "tok-2"));

        SnapshotListOptions options = new SnapshotListOptions();
        options.setApiKey("api-key");
        options.setBaseUrl(server.url("/").toString());
        options.setSandboxID("sb-1");
        options.setLimit(10);
        SnapshotListResult page = Sandbox.listSnapshots(options);

        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/snapshots?sandboxID=sb-1&limit=10", request.getPath());
        assertEquals("snap-1", page.getItems()[0].getSnapshotId());
        assertEquals("tok-2", page.getNextToken());
        assertTrue(page.isHasMore());
    }

    @Test
    void metricsSendsEpochSecondFiltersAndParsesModernFields() throws Exception {
        startServer();
        enqueueConnectInfo(server.url("/").toString(), "0.5.7");
        server.enqueue(json("[{\"timestamp\":\"2026-01-01T00:00:00Z\",\"cpuUsedPct\":12.5,"
                + "\"cpuCount\":2,\"memUsed\":100,\"memTotal\":200,\"diskUsed\":1,\"diskTotal\":2}]"));

        SandboxMetrics[] metrics = connectSandbox().metrics(
                java.time.Instant.parse("2026-01-01T00:00:00Z"),
                java.time.Instant.parse("2026-01-02T00:00:00Z"));

        discardConnectRequest();
        RecordedRequest request = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/metrics?start=1767225600&end=1767312000", request.getPath());
        assertEquals(1, metrics.length);
        assertEquals(12.5, metrics[0].getCpuUsedPct());
        assertEquals(Long.valueOf(100L), metrics[0].getMemUsed());
    }

    private Sandbox connectSandbox() throws Exception {
        return Sandbox.connect("sb-1", connectOpts());
    }

    /** Skips the POST /sandboxes/{id}/connect request every connect() makes. */
    private void discardConnectRequest() throws Exception {
        RecordedRequest connect = server.takeRequest(1, TimeUnit.SECONDS);
        assertEquals("/sandboxes/sb-1/connect", connect.getPath());
    }

    private SandboxOpts connectOpts() {
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey("api-key");
        // Pin the modern domain so a host NOVITA_DOMAIN cannot flip legacy
        // detection mid-test; the mock server URL is not a Novita domain.
        opts.setDomain("us-phx-1.sandbox.novita.ai");
        opts.setBaseUrl(server.url("/").toString());
        return opts;
    }

    private SandboxOpts cloneOpts() {
        return connectOpts();
    }

    private void startServer() throws Exception {
        server = new MockWebServer();
        server.start();
    }

    private void enqueueConnectInfo(String domain, String envdVersion) {
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"domain\":\"" + domain
                + "\",\"envdVersion\":\"" + envdVersion
                + "\",\"envdAccessToken\":\"token\"}"));
    }

    private void enqueueConnectInfoNoToken(String domain, String envdVersion) {
        server.enqueue(json("{\"sandboxID\":\"sb-1\",\"domain\":\"" + domain
                + "\",\"envdVersion\":\"" + envdVersion + "\"}"));
    }

    private static MockResponse json(String body) {
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/json")
                .setBody(body);
    }

    /** A connect-protocol stream: data frames for the events, then a trailer. */
    private static MockResponse connectStream(String... eventJson) {
        okio.Buffer body = new okio.Buffer();
        for (String event : eventJson) {
            byte[] payload = event.getBytes(java.nio.charset.StandardCharsets.UTF_8);
            body.writeByte(0);
            body.writeInt(payload.length);
            body.write(payload);
        }
        byte[] trailer = "{}".getBytes(java.nio.charset.StandardCharsets.UTF_8);
        body.writeByte(2);
        body.writeInt(trailer.length);
        body.write(trailer);
        return new MockResponse()
                .setResponseCode(200)
                .setHeader("Content-Type", "application/connect+json")
                .setBody(body);
    }

    private static JsonObject parse(RecordedRequest request) throws Exception {
        JsonElement element = JsonParser.parseString(request.getBody().readUtf8());
        return element.getAsJsonObject();
    }

    /** Parses the JSON message inside a connect-protocol unary envelope. */
    private static JsonObject parseEnvelope(RecordedRequest request) throws Exception {
        okio.Buffer body = new okio.Buffer();
        body.write(request.getBody().clone().readByteArray());
        body.readByte();
        int length = body.readInt();
        return JsonParser.parseString(body.readUtf8(length)).getAsJsonObject();
    }
}
