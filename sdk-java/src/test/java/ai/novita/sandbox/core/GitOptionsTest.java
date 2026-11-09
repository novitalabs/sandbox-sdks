package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.mockwebserver.*;
import okio.Buffer;
import org.junit.jupiter.api.*;
import java.nio.charset.StandardCharsets;
import java.util.*;
import java.util.concurrent.TimeUnit;
import static org.junit.jupiter.api.Assertions.*;

@Timeout(15)
class GitOptionsTest {
    private MockWebServer server;
    private ApiClient api;
    private Sandbox sandbox;
    private final List<String> logs = new ArrayList<>();

    @BeforeEach void start() throws Exception {
        server = new MockWebServer(); server.start();
        SandboxOpts opts = new SandboxOpts(); opts.setApiKey("fixture-key");
        opts.setBaseUrl(server.url("/").toString()); opts.setSandboxUrl(server.url("/").toString());
        opts.setDomain("modern.example"); opts.setRequestTimeoutSeconds(1); opts.setLogger(logs::add);
        api = new ApiClient(opts);
        SandboxInfo info = new SandboxInfo(); info.setId("fixture"); info.setEnvdVersion("0.5.7");
        sandbox = new Sandbox("fixture", api, info);
    }
    @AfterEach void stop() throws Exception { sandbox.rpc().close(); api.close(); server.shutdown(); }

    private static Buffer frame(String json) {
        byte[] bytes = json.getBytes(StandardCharsets.UTF_8);
        return new Buffer().writeByte(0).writeInt(bytes.length).write(bytes);
    }
    private static MockResponse success() throws Exception {
        Buffer body = frame("{\"event\":{\"start\":{\"pid\":8}}}");
        body.writeAll(frame("{\"event\":{\"end\":{\"exitCode\":0}}}"));
        return new MockResponse().setHeader("Content-Type", "application/connect+json").setBody(body);
    }
    private JsonObject request() throws Exception {
        RecordedRequest request = server.takeRequest(2, TimeUnit.SECONDS);
        assertNotNull(request); assertEquals("/process.Process/Start", request.getPath());
        Buffer body = request.getBody(); assertEquals(0, body.readByte());
        return JsonParser.parseString(body.readUtf8(body.readInt())).getAsJsonObject().getAsJsonObject("process");
    }
    private static String command(JsonObject process) { return process.getAsJsonArray("args").get(2).getAsString(); }
    private interface Operation { void run(Git git) throws Exception; }

    @Test void commandOptionsApplyToEveryGitOperationWithoutMutatingCaller() throws Exception {
        CommandOpts opts = new CommandOpts(); opts.setWorkDir("/per-call"); opts.setUser("root");
        Map<String,String> env = new HashMap<>(); env.put("CUSTOM", "original"); opts.setEnv(env);
        opts.setTimeout(0); opts.setRequestTimeoutSeconds(3);
        Git git = sandbox.git().withOptions(opts);
        opts.setWorkDir("/changed"); env.put("CUSTOM", "changed");
        List<Operation> operations = Arrays.asList(
                g -> g.clone("https://example.invalid/repo", "target", null),
                g -> g.add("file"), g -> g.commit("message"), Git::push, Git::pull,
                g -> g.checkoutBranch("main"), g -> g.createBranch("topic"), g -> g.deleteBranch("topic", false),
                g -> g.remoteAdd("origin", "https://example.invalid/repo"), g -> g.remoteGet("origin"),
                g -> g.reset("mixed", "HEAD"), g -> g.restore("file"),
                g -> g.setConfig("user.name", "fixture"), g -> g.getConfig("user.name"), Git::status, Git::branches,
                g -> g.init("explicit-target"));
        for (Operation operation : operations) {
            server.enqueue(success()); operation.run(git);
            RecordedRequest wire = server.takeRequest(2, TimeUnit.SECONDS); assertNotNull(wire);
            assertEquals("Basic cm9vdDo=", wire.getHeader("Authorization"));
            Buffer body = wire.getBody(); body.readByte();
            JsonObject process = JsonParser.parseString(body.readUtf8(body.readInt())).getAsJsonObject().getAsJsonObject("process");
            assertEquals("/per-call", process.get("cwd").getAsString());
            assertEquals("original", process.getAsJsonObject("envs").get("CUSTOM").getAsString());
        }
        assertEquals("changed", env.get("CUSTOM"));
    }

    @Test void viewsInheritRepositoryButKeepTheirOwnSelection() throws Exception {
        Git original = sandbox.git(); server.enqueue(success()); original.init("/original"); request();
        Git inherited = original.withOptions(new CommandOpts());
        server.enqueue(success()); inherited.status(); assertEquals("cd '/original' && git status --porcelain -b", command(request()));
        CommandOpts opts = new CommandOpts(); opts.setWorkDir("/other");
        Git other = original.withOptions(opts);
        server.enqueue(success()); other.status(); JsonObject process = request();
        assertEquals("/other", process.get("cwd").getAsString()); assertEquals("git status --porcelain -b", command(process));
        server.enqueue(success()); other.init("/explicit-target"); assertEquals("git init '/explicit-target'", command(request()));
        server.enqueue(success()); other.status(); assertEquals("cd '/explicit-target' && git status --porcelain -b", command(request()));
        server.enqueue(success()); original.status(); assertEquals("cd '/original' && git status --porcelain -b", command(request()));
    }

    @Test void authenticatedPushPullAndCloneKeepCredentialsOutOfArgvAndLogs() throws Exception {
        GitAuth auth = new GitAuth().setUsername("fixture-user").setPassword("fixture-password");
        CommandOpts opts = new CommandOpts(); opts.setEnv(Collections.singletonMap("CUSTOM", "kept"));
        Git git = sandbox.git().withOptions(opts);
        server.enqueue(success()); git.push(new GitPushOptions().setRemote("origin").setBranch("topic branch").setAuth(auth).setSetUpstream(true));
        JsonObject push = request();
        assertTrue(command(push).endsWith(" push --set-upstream -- 'origin' 'topic branch'"));
        server.enqueue(success()); git.pull(new GitPullOptions().setRemote("origin").setBranch("main").setAuth(auth).setRebase(false));
        JsonObject pull = request(); assertTrue(command(pull).endsWith(" pull --no-rebase -- 'origin' 'main'"));
        server.enqueue(success()); git.clone("https://example.invalid/repo", "target", new GitCloneOptions().setBranch("main").setDepth(2).setAuth(auth));
        JsonObject clone = request(); assertTrue(command(clone).endsWith(" clone --branch 'main' --depth 2 -- 'https://example.invalid/repo' 'target'"));
        for (JsonObject process : Arrays.asList(push, pull, clone)) {
            assertFalse(process.getAsJsonArray("args").toString().contains("fixture-password"));
            assertFalse(process.getAsJsonArray("args").toString().contains("fixture-user"));
            assertEquals("fixture-password", process.getAsJsonObject("envs").get("NOVITA_GIT_PASS").getAsString());
            assertEquals("kept", process.getAsJsonObject("envs").get("CUSTOM").getAsString());
        }
        server.enqueue(success()); git.status();
        assertFalse(request().getAsJsonObject("envs").has("NOVITA_GIT_PASS"));
        assertEquals(Collections.singletonMap("CUSTOM", "kept"), opts.getEnv());
        assertFalse(logs.toString().contains("fixture-password"));
    }

    @Test void pullRebaseAndBranchOnlyUseExplicitRemoteArguments() throws Exception {
        server.enqueue(success()); sandbox.git().pull(new GitPullOptions().setBranch("main").setRebase(true));
        assertEquals("git pull --rebase -- 'origin' 'main'", command(request()));
        server.enqueue(success()); sandbox.git().push(new GitPushOptions());
        assertEquals("git push", command(request()));
        assertThrows(IllegalArgumentException.class, () -> new GitCloneOptions().setDepth(0));
        assertThrows(IllegalArgumentException.class, () -> new GitCloneOptions().setDepth(-1));
    }

    @Test void authenticatedFailuresRedactCredentialsFromExceptionDetails() throws Exception {
        String secret = "fixture-password";
        Buffer body = frame("{\"event\":{\"start\":{\"pid\":8}}}");
        body.writeAll(frame("{\"event\":{\"data\":{\"stderr\":\"" + Base64.getEncoder().encodeToString(secret.getBytes(StandardCharsets.UTF_8)) + "\"}}}"));
        body.writeAll(frame("{\"event\":{\"end\":{\"exitCode\":1,\"error\":\"authentication failed: " + secret + "\"}}}"));
        server.enqueue(new MockResponse().setHeader("Content-Type", "application/connect+json").setBody(body));
        GitAuth auth = new GitAuth().setUsername("fixture-user").setPassword(secret);
        CommandExitException error = assertThrows(CommandExitException.class,
                () -> sandbox.git().push(new GitPushOptions().setAuth(auth)));
        assertEquals(1, error.getExitCode());
        assertFalse(error.getMessage().contains(secret));
        assertFalse(error.getStderr().contains(secret));
        assertFalse(error.getCommandError().contains(secret));
        java.io.StringWriter trace = new java.io.StringWriter(); error.printStackTrace(new java.io.PrintWriter(trace));
        assertFalse(trace.toString().contains(secret)); assertFalse(logs.toString().contains(secret)); request();
    }

    @Test void executionAndRequestTimeoutsApplyToBoundViews() throws Exception {
        CommandOpts execution = new CommandOpts(); execution.setTimeout(1); execution.setRequestTimeoutSeconds(3);
        server.enqueue(success().setBodyDelay(1400, TimeUnit.MILLISECONDS));
        assertThrows(TimeoutException.class, () -> sandbox.git().withOptions(execution).status()); request();
        CommandOpts request = new CommandOpts(); request.setTimeout(0); request.setRequestTimeoutSeconds(1);
        server.enqueue(success().setHeadersDelay(1400, TimeUnit.MILLISECONDS));
        assertThrows(TimeoutException.class, () -> sandbox.git().withOptions(request).status()); request();
    }

    @Test void zeroExecutionTimeoutAndLongerRequestOverrideRemainEffective() throws Exception {
        CommandOpts opts = new CommandOpts(); opts.setTimeout(0); opts.setRequestTimeoutSeconds(3);
        server.enqueue(success().setBodyDelay(1200, TimeUnit.MILLISECONDS));
        assertDoesNotThrow(() -> sandbox.git().withOptions(opts).status()); request();
    }
}
