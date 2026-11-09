package ai.novita.sandbox.core;

import com.google.gson.*;
import okhttp3.mockwebserver.*;
import okio.Buffer;
import org.apache.commons.compress.archivers.tar.*;
import org.junit.jupiter.api.*;
import org.junit.jupiter.api.io.TempDir;
import java.io.*;
import java.lang.reflect.*;
import java.nio.charset.StandardCharsets;
import java.nio.file.*;
import java.util.*;
import java.util.zip.GZIPInputStream;
import static org.junit.jupiter.api.Assertions.*;

class TemplateGitRegressionTest {
    @TempDir Path directory;
    MockWebServer server;
    ApiClient api;
    Sandbox sandbox;
    @BeforeEach void start() throws Exception {
        server = new MockWebServer(); server.start();
        api = new ApiClient("placeholder", server.url("/").toString(), 3, "modern.example");
        SandboxInfo info = new SandboxInfo(); info.setId("fixture");
        info.setDomain(server.url("/").toString()); info.setEnvdVersion("0.5.7");
        sandbox = new Sandbox("fixture", api, info);
    }
    @AfterEach void stop() throws Exception { sandbox.rpc().close(); api.close(); server.shutdown(); }
    static Buffer frame(int flags, String json) {
        byte[] b = json.getBytes(StandardCharsets.UTF_8);
        return new Buffer().writeByte(flags).writeInt(b.length).write(b);
    }
    void process() throws Exception {
        Buffer b = frame(0, "{\"event\":{\"start\":{\"pid\":123}}}");
        b.writeAll(frame(0, "{\"event\":{\"end\":{\"exitCode\":0,\"exited\":true}}}"));
        b.writeAll(frame(2, "{}"));
        server.enqueue(new MockResponse().setHeader("Content-Type", "application/connect+json").setBody(b));
    }
    String command() throws Exception {
        Buffer b = server.takeRequest().getBody(); b.readByte();
        return JsonParser.parseString(b.readUtf8(b.readInt())).getAsJsonObject()
                .getAsJsonObject("process").getAsJsonArray("args").get(2).getAsString();
    }
    static byte[] archive(Path source, String context, boolean follow) throws Exception {
        Method m = Template.class.getDeclaredMethod("tar", Path.class, String.class, boolean.class, List.class);
        m.setAccessible(true);
        try { return (byte[]) m.invoke(null, source, context, follow, Collections.emptyList()); }
        catch (InvocationTargetException e) { throw (Exception)e.getCause(); }
    }
    @Test void relativeCopyContextArchivesExistingFile() throws Exception {
        byte[] bytes = assertDoesNotThrow(() -> archive(Paths.get("pom.xml"), ".", false));
        try (TarArchiveInputStream in = new TarArchiveInputStream(new GZIPInputStream(new ByteArrayInputStream(bytes)))) {
            assertEquals("pom.xml", in.getNextTarEntry().getName());
            assertTrue(new String(in.readAllBytes(), StandardCharsets.UTF_8).contains("<project"));
        }
    }
    @Test void symlinkIsPreservedWithoutReadingTarget() throws Exception {
        Files.writeString(directory.resolve("a.txt"), "content");
        Path link = Files.createSymbolicLink(directory.resolve("link.txt"), Paths.get("a.txt"));
        byte[] bytes = assertDoesNotThrow(() -> archive(link, directory.toString(), false));
        try (TarArchiveInputStream in = new TarArchiveInputStream(new GZIPInputStream(new ByteArrayInputStream(bytes)))) {
            TarArchiveEntry entry = in.getNextTarEntry();
            assertTrue(entry.isSymbolicLink()); assertEquals("a.txt", entry.getLinkName()); assertEquals(0, entry.getSize());
        }
    }
    @Test void followingDirectorySymlinksIncludesContents() throws Exception {
        Files.createDirectory(directory.resolve("actual")); Files.writeString(directory.resolve("actual/a.txt"), "content");
        Path link = Files.createSymbolicLink(directory.resolve("linked"), Paths.get("actual"));
        byte[] bytes = assertDoesNotThrow(() -> archive(link, directory.toString(), true));
        Map<String,String> files = new HashMap<>();
        try (TarArchiveInputStream in = new TarArchiveInputStream(new GZIPInputStream(new ByteArrayInputStream(bytes)))) {
            TarArchiveEntry entry;
            while ((entry = in.getNextTarEntry()) != null) if (entry.isFile()) files.put(entry.getName(), new String(in.readAllBytes(), StandardCharsets.UTF_8));
        }
        assertEquals("content", files.get("linked/a.txt"));
    }
    @Test void clonePutsBranchAfterSubcommand() throws Exception {
        process(); GitCloneOptions opts = new GitCloneOptions(); opts.setBranch("main");
        sandbox.git().clone("https://example.invalid/repo", "/repo", opts);
        assertTrue(command().startsWith("git clone --branch 'main' "));
    }
    @Test void commitDoesNotStageUnselectedFiles() throws Exception {
        process(); sandbox.git().commit("selected");
        assertEquals("git commit -m 'selected'", command());
    }
    @Test void branchCreationAlsoChecksOutSelectedBranch() throws Exception {
        process(); process(); sandbox.git().init("/repo"); sandbox.git().createBranch("topic"); command();
        assertEquals("cd '/repo' && git checkout -b 'topic'", command());
    }
    @Test void everyRepositoryOperationUsesSelectedDirectory() throws Exception {
        process(); sandbox.git().init("/repo"); command();
        List<ThrowingOperation> operations = Arrays.asList(
            () -> sandbox.git().checkoutBranch("main"), () -> sandbox.git().deleteBranch("topic", false),
            () -> sandbox.git().remoteAdd("origin", "https://example.invalid/repo"), () -> sandbox.git().remoteGet("origin"),
            () -> sandbox.git().reset("mixed", "HEAD"), () -> sandbox.git().restore("a.txt"),
            () -> sandbox.git().getConfig("user.name"), () -> sandbox.git().branches());
        for (ThrowingOperation operation : operations) { process(); operation.run(); assertTrue(command().startsWith("cd '/repo' && ")); }
    }
    interface ThrowingOperation { void run() throws Exception; }
    @Test void buildFailureRetainsStringStep() throws Exception {
        server.enqueue(new MockResponse().setBody("{\"status\":\"failed\",\"reason\":{\"step\":\"RUN npm install\",\"message\":\"failed\"}}"));
        BuildStatus status = assertDoesNotThrow(() -> new Template(api).getBuildStatus("t", "b"));
        assertEquals("RUN npm install", status.getReason().getStep());
    }
    @Test void dockerfileCopyRetainsOwnerAndMode() throws Exception {
        Map<String,Object> spec = TemplateBuilder.create().fromDockerfile("FROM debian:12\nCOPY --chown=user:group --chmod=0755 a.txt /app/\n").toMap();
        assertEquals(Arrays.asList("a.txt", "/app/", "user:group", "0755"), copies(spec).get(0).get("args"));
    }
    @Test void dockerfileJsonCopyPreservesSpacesAndMultipleSources() throws Exception {
        Map<String,Object> spec = assertDoesNotThrow(() -> TemplateBuilder.create().fromDockerfile("FROM debian:12\nCOPY [\"a file.txt\", \"b.txt\", \"/app dir/\"]\n").toMap());
        assertEquals(Arrays.asList("a file.txt", "/app dir/", "", ""), copies(spec).get(0).get("args"));
        assertEquals(Arrays.asList("b.txt", "/app dir/", "", ""), copies(spec).get(1).get("args"));
    }
    @Test void dockerfileExecCommandsCombineEntrypointAndCmdWithoutLosingQuotes() throws Exception {
        Map<String,Object> spec = TemplateBuilder.create().fromDockerfile("FROM debian:12\nENTRYPOINT [\"sh\",\"-c\"]\nCMD [\"echo a b\"]\n").toMap();
        assertEquals("sh -c 'echo a b'", spec.get("startCmd"));
    }
    @Test void renderedDockerfileRetainsCopyOwnershipAndSpaceContainingPaths() throws Exception {
        TemplateBuilder original = TemplateBuilder.create().fromImage("debian:12", false)
                .copyFile("a file.txt", "/app dir/", "user:group", 0755, false, null);
        Map<String,Object> reparsed = TemplateBuilder.create().fromDockerfile(original.toDockerfile()).toMap();
        assertEquals(Arrays.asList("a file.txt", "/app dir/", "user:group", "0755"), copies(reparsed).get(0).get("args"));
    }
    @SuppressWarnings("unchecked") static List<Map<String,Object>> copies(Map<String,Object> spec) {
        List<Map<String,Object>> result = new ArrayList<>();
        for (Map<String,Object> step : (List<Map<String,Object>>)spec.get("steps")) if ("COPY".equals(step.get("type"))) result.add(step);
        return result;
    }
}
