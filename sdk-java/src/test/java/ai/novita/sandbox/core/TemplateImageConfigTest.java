package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.mockwebserver.*;
import org.junit.jupiter.api.*;
import java.lang.reflect.Method;
import java.nio.charset.StandardCharsets;
import java.util.*;
import static org.junit.jupiter.api.Assertions.*;

class TemplateImageConfigTest {
    MockWebServer registry;
    @BeforeEach void start() throws Exception { registry = new MockWebServer(); registry.start(); }
    @AfterEach void stop() throws Exception { registry.shutdown(); }
    String image() { return registry.getHostName() + ":" + registry.getPort() + "/org/demo:1"; }
    void imageResponses() {
        registry.enqueue(new MockResponse().setBody("{\"manifests\":[{\"digest\":\"sha256:arm\",\"platform\":{\"os\":\"linux\",\"architecture\":\"arm64\"}},{\"digest\":\"sha256:amd\",\"platform\":{\"os\":\"linux\",\"architecture\":\"amd64\"}}]}"));
        registry.enqueue(new MockResponse().setBody("{\"config\":{\"digest\":\"sha256:config\"}}"));
        registry.enqueue(new MockResponse().setBody("{\"config\":{\"Env\":[\"PATH=/opt/tools/bin:/usr/bin\",\"HOME=/root\",\"FOO=base\"],\"WorkingDir\":\"/app\",\"Entrypoint\":[\"python\"],\"Cmd\":[\"server.py\",\"argument with spaces\"]}}"));
    }
    static JsonObject json(TemplateBuilder builder) { return JsonParser.parseString(builder.toJson()).getAsJsonObject(); }
    static String runtimeScript(JsonObject json) {
        for (JsonElement e : json.getAsJsonArray("steps")) {
            JsonObject step = e.getAsJsonObject();
            if (!step.get("type").getAsString().equals("RUN")) continue;
            String cmd = step.getAsJsonArray("args").get(0).getAsString();
            if(cmd.startsWith("echo ") && cmd.contains("image-env.sh")) {
                return new String(Base64.getDecoder().decode(cmd.split(" ",3)[1]), StandardCharsets.UTF_8);
            }
        }
        return "";
    }
    @Test void serializationReadsAmd64ConfigAndRestoresRuntime() throws Exception {
        imageResponses(); TemplateBuilder builder = TemplateBuilder.create().fromImage(image());
        JsonObject spec = json(builder);
        assertEquals("python server.py 'argument with spaces'", spec.has("startCmd") ? spec.get("startCmd").getAsString() : null);
        assertTrue(spec.getAsJsonArray("steps").toString().contains("/app"));
        assertTrue(runtimeScript(spec).contains("PATH '/opt/tools/bin:/usr/bin'"));
        assertFalse(runtimeScript(spec).contains("HOME="));
        assertEquals("/v2/org/demo/manifests/1", registry.takeRequest().getPath());
        assertEquals("/v2/org/demo/manifests/sha256:amd", registry.takeRequest().getPath());
        assertEquals("/v2/org/demo/blobs/sha256:config", registry.takeRequest().getPath());
        imageResponses(); json(builder); assertEquals(6, registry.getRequestCount(), "refresh mutable tags per build");
    }
    @Test void explicitBuilderValuesOverrideInheritedConfig() {
        imageResponses();
        JsonObject spec = json(TemplateBuilder.create().fromImage(image()).environment("FOO", "caller", true)
                .workdir("/chosen").setStartCmd("my-command").setReadyCmd("my-ready"));
        assertEquals("my-command", spec.get("startCmd").getAsString());
        assertEquals("my-ready", spec.get("readyCmd").getAsString());
        assertTrue(runtimeScript(spec).contains("FOO \"caller\""));
        assertTrue(runtimeScript(spec).contains("PATH '/opt/tools/bin:/usr/bin'"));
        assertTrue(spec.getAsJsonArray("steps").toString().contains("\"/app\""));
    }
    @Test void registryCredentialsAreUsedForManifestAndBlob() throws Exception {
        imageResponses();
        Map<String,Object> credentials = new LinkedHashMap<>(); credentials.put("type", "registry");
        credentials.put("username", "fixture-user"); credentials.put("password", "fixture-password");
        JsonObject spec = json(TemplateBuilder.create().fromImage(image()).with("fromImageRegistry", credentials));
        assertTrue(spec.has("startCmd"), "authenticated config must be applied");
        String auth = "Basic " + Base64.getEncoder().encodeToString("fixture-user:fixture-password".getBytes(StandardCharsets.UTF_8));
        for (int i=0; i<3; i++) assertEquals(auth, registry.takeRequest().getHeader("Authorization"));
    }
    @Test void registry404ProducesDiagnosticWithoutThrowing() throws Exception {
        registry.enqueue(new MockResponse().setResponseCode(404));
        TemplateBuilder builder = TemplateBuilder.create().fromImage(image());
        assertDoesNotThrow(builder::toJson);
        Method diagnostic = assertDoesNotThrow(() -> TemplateBuilder.class.getMethod("getImageConfigFailure"));
        Object result = diagnostic.invoke(builder);
        assertNotNull(result); assertTrue(result.toString().contains("404"));
        assertTrue(result.toString().contains("unusable"));
    }
    @Test void publicStatusSupportsLogOffset() throws Exception {
        registry.enqueue(new MockResponse().setBody("{\"status\":\"building\",\"logEntries\":[]}"));
        try (ApiClient api = new ApiClient("placeholder", registry.url("/").toString(), 3, "modern.example")) {
            Method status = assertDoesNotThrow(() -> Template.class.getMethod("getBuildStatus", String.class, String.class, int.class));
            status.invoke(new Template(api), "t", "b", 17);
            assertEquals("/templates/t/builds/b/status?logsOffset=17", registry.takeRequest().getPath());
        }
    }
    @Test void bearerChallengeExchangesCredentialsForScopedToken() throws Exception {
        registry.enqueue(new MockResponse().setResponseCode(401).setHeader("WWW-Authenticate",
                "Bearer realm=\"" + registry.url("/token") + "\",service=\"local-registry\",scope=\"repository:org/demo:pull\""));
        registry.enqueue(new MockResponse().setBody("{\"access_token\":\"fixture-bearer\"}"));
        imageResponses();
        JsonObject spec = json(TemplateBuilder.create().fromImage(image(), "fixture-user", "fixture-password"));
        assertTrue(spec.has("startCmd"));
        String basic = registry.takeRequest().getHeader("Authorization");
        RecordedRequest token = registry.takeRequest();
        assertEquals(basic, token.getHeader("Authorization"));
        assertEquals("repository:org/demo:pull", token.getRequestUrl().queryParameter("scope"));
        for (int i=0; i<3; i++) assertEquals("Bearer fixture-bearer", registry.takeRequest().getHeader("Authorization"));
    }
    @Test void transientRegistryFailureRetriesAndRecovers() {
        registry.enqueue(new MockResponse().setResponseCode(429).setHeader("Retry-After", "0"));
        imageResponses();
        assertTrue(json(TemplateBuilder.create().fromImage(image())).has("startCmd"));
        assertEquals(4, registry.getRequestCount());
    }
    @Test void absentAmd64VariantIsReportedAsUnusable() {
        registry.enqueue(new MockResponse().setBody("{\"manifests\":[{\"digest\":\"sha256:arm\",\"platform\":{\"os\":\"linux\",\"architecture\":\"arm64\"}}]}"));
        TemplateBuilder builder = TemplateBuilder.create().fromImage(image()); json(builder);
        assertTrue(builder.getImageConfigFailure().get("unusable").contains("linux/amd64"));
        assertEquals(1, registry.getRequestCount());
    }
    @Test void optOutAndTemplateBasePerformNoRegistryRequests() {
        assertFalse(json(TemplateBuilder.create().fromImage(image(), false)).has("startCmd"));
        assertFalse(json(TemplateBuilder.create().fromImage(image()).inheritImageConfig(false)).has("startCmd"));
        JsonObject fromTemplate = json(TemplateBuilder.create().fromImage(image()).fromTemplate("existing"));
        assertFalse(fromTemplate.has("fromImage")); assertEquals("existing", fromTemplate.get("fromTemplate").getAsString());
        assertEquals(0, registry.getRequestCount());
    }
    @Test void credentialsAreNotIncludedInFailureDiagnostics() {
        registry.enqueue(new MockResponse().setResponseCode(403).setBody("fixture-password"));
        TemplateBuilder builder = TemplateBuilder.create().fromImage(image(), "fixture-user", "fixture-password");
        json(builder);
        assertTrue(builder.getImageConfigFailure().get("error").contains("403"));
        assertFalse(builder.getImageConfigFailure().toString().contains("fixture-password"));
    }
    @Test void malformedConfigIsDiagnosticAndDoesNotPreventSerialization() {
        registry.enqueue(new MockResponse().setBody("not-json"));
        TemplateBuilder builder = TemplateBuilder.create().fromImage(image());
        assertDoesNotThrow(builder::toJson); assertNotNull(builder.getImageConfigFailure());
    }
    @Test void imageReferenceNormalizationIncludesTagsDigestsAndRegistries() {
        TemplateImageConfig.ImageReference ref = TemplateImageConfig.ImageReference.parse("python");
        assertEquals("https://registry-1.docker.io/v2/library/python", ref.base.toString()); assertEquals("latest", ref.reference);
        ref = TemplateImageConfig.ImageReference.parse("docker.io/org/app:tag");
        assertEquals("org/app", ref.repository); assertEquals("tag", ref.reference);
        ref = TemplateImageConfig.ImageReference.parse("registry.example:5000/org/app@sha256:123");
        assertEquals("registry.example", ref.base.host()); assertEquals(5000, ref.base.port()); assertEquals("sha256:123", ref.reference);
    }
    @Test void realBuildAndBackgroundBuildResolveBeforeTriggering() throws Exception {
        for (boolean background : new boolean[]{false, true}) {
            imageResponses();
            registry.enqueue(new MockResponse().setBody("{\"templateID\":\"t\",\"buildID\":\"b\"}"));
            registry.enqueue(new MockResponse().setBody("{}"));
            if (!background) registry.enqueue(new MockResponse().setBody("{\"status\":\"ready\",\"logEntries\":[]}"));
            try (ApiClient api = new ApiClient("api-fixture", registry.url("/").toString(), 3, "modern.example")) {
                Template template = new Template(api); TemplateBuilder builder = TemplateBuilder.create().fromImage(image());
                if (background) template.buildInBackground("fixture", builder, null); else template.build("fixture", builder, null);
            }
            for (int i=0; i<3; i++) assertNull(registry.takeRequest().getHeader("X-API-Key"));
            assertEquals("/v3/templates", registry.takeRequest().getPath());
            RecordedRequest trigger = registry.takeRequest();
            assertEquals("/v2/templates/t/builds/b", trigger.getPath());
            JsonObject body = JsonParser.parseString(trigger.getBody().readUtf8()).getAsJsonObject();
            assertEquals("python server.py 'argument with spaces'", body.get("startCmd").getAsString());
            assertTrue(runtimeScript(body).contains("PATH "));
            if (!background) assertEquals("/templates/t/builds/b/status?logsOffset=0", registry.takeRequest().getPath());
        }
    }
    @Test void buildReportsConfigFailureThroughLogConsumer() throws Exception {
        registry.enqueue(new MockResponse().setResponseCode(404));
        registry.enqueue(new MockResponse().setBody("{\"templateID\":\"t\",\"buildID\":\"b\"}"));
        registry.enqueue(new MockResponse().setBody("{}"));
        List<BuildLogEntry> logs = new ArrayList<>();
        try (ApiClient api = new ApiClient("placeholder", registry.url("/").toString(), 3, "modern.example")) {
            new Template(api).buildInBackground("fixture", TemplateBuilder.create().fromImage(image()), new TemplateBuildOptions().setLogConsumer(logs::add));
        }
        assertEquals(1, logs.size()); assertEquals("warn", logs.get(0).getLevel()); assertTrue(logs.get(0).getMessage().contains("404"));
    }
    @Test void negativeLogOffsetDoesNotSendRequest() throws Exception {
        try (ApiClient api = new ApiClient("placeholder", registry.url("/").toString(), 3, "modern.example")) {
            assertThrows(IllegalArgumentException.class, () -> new Template(api).getBuildStatus("t", "b", -1));
        }
        assertEquals(0, registry.getRequestCount());
    }
    @Test void credentialsAreNotDelegatedToArbitraryTokenOrigins() throws Exception {
        Method method = assertDoesNotThrow(() -> TemplateImageConfig.class.getDeclaredMethod("tokenCredentials", okhttp3.HttpUrl.class, okhttp3.HttpUrl.class, String.class));
        method.setAccessible(true);
        okhttp3.HttpUrl origin = okhttp3.HttpUrl.get("https://registry.example/v2/image");
        assertNull(method.invoke(null, origin, okhttp3.HttpUrl.get("https://other.example/token"), "Basic fixture"));
        assertEquals("Basic fixture", method.invoke(null, origin, okhttp3.HttpUrl.get("https://registry.example/token"), "Basic fixture"));
        assertEquals("Basic fixture", method.invoke(null, okhttp3.HttpUrl.get("https://registry-1.docker.io/v2/image"), okhttp3.HttpUrl.get("https://auth.docker.io/token"), "Basic fixture"));
        assertNull(method.invoke(null, okhttp3.HttpUrl.get("https://registry-1.docker.io/v2/image"), okhttp3.HttpUrl.get("https://auth.docker.io:8443/token"), "Basic fixture"));
    }
    @Test void crossOriginPlaintextRedirectDoesNotReceiveCredentials() throws Exception {
        try (MockWebServer other = new MockWebServer()) {
            other.start();
            for (int i=0; i<3; i++) registry.enqueue(new MockResponse().setResponseCode(302).setHeader("Location", other.url("/stolen")));
            TemplateBuilder builder = TemplateBuilder.create().fromImage(image(), "fixture-user", "fixture-password");
            json(builder); assertNotNull(builder.getImageConfigFailure());
            assertEquals(0, other.getRequestCount());
        }
    }
    @Test void failedResolutionIsStableAndExplicitValuesSurvive() {
        registry.enqueue(new MockResponse().setResponseCode(404));
        TemplateBuilder builder = TemplateBuilder.create().fromImage(image()).environment("KEEP", "yes", true).workdir("/chosen").command("my-start");
        JsonObject first = json(builder); registry.enqueue(new MockResponse().setResponseCode(404)); JsonObject second = json(builder);
        assertEquals(first, second); assertEquals(2, registry.getRequestCount());
        assertEquals("my-start", second.get("startCmd").getAsString());
        assertTrue(runtimeScript(second).contains("KEEP \"yes\""));
        assertTrue(second.getAsJsonArray("steps").toString().contains("/chosen"));
    }
    @Test void explicitOverridesAfterResolutionStillWin() {
        imageResponses(); TemplateBuilder builder = TemplateBuilder.create().fromImage(image()); json(builder);
        imageResponses(); JsonObject later = json(builder.environment("PATH", "/custom", true).workdir("/other").command("override"));
        assertTrue(runtimeScript(later).contains("PATH \"/custom\""));
        assertTrue(later.getAsJsonArray("steps").toString().contains("\"/app\""));
        assertEquals("override", later.get("startCmd").getAsString()); assertEquals(6, registry.getRequestCount());
    }
}
