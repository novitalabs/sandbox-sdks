package ai.novita.sandbox.core;

import java.util.Map;
import java.util.concurrent.CompletableFuture;
import com.google.gson.JsonObject;

/**
 * Novita Agent Sandbox - main interface for sandbox operations.
 *
 * A sandbox is an isolated cloud environment for running untrusted code,
 * executing notebooks, and driving desktop environments.
 */
public class Sandbox implements AutoCloseable {
    private final String id;
    private final ApiClient client;
    private final Filesystem files;
    private final Commands commands;
    private final Pty pty;
    private final Git git;
    private final SandboxInfo connectionInfo;
    private volatile SandboxInfo runtimeInfo;
    private ConnectRpcClient rpc;
    private volatile boolean closed;
    /** Cached envd domain; {@link #portUrl} would otherwise cost an HTTP round trip per call. */
    private volatile String cachedDomain;

    /**
     * Creates a new sandbox instance.
     *
     * @param template The template to use (e.g., "base", "python", "node")
     * @param opts Optional configuration for the sandbox
     * @return A new Sandbox instance
     * @throws SandboxException if creation fails
     */
    public static Sandbox create(String template, SandboxOpts opts) throws SandboxException {
        if (opts == null) {
            opts = new SandboxOpts();
        }

        ApiClient client = new ApiClient(opts);
        SandboxInfo created;
        if (opts.isDebug()) {
            created = new SandboxInfo(); created.setId(template == null ? "base" : template); created.setDomain(opts.resolvedDomain()); created.setEnvdVersion("0.0.0");
        } else {
            try { created = client.createSandbox(template, opts); } catch (Exception e) { client.close(); throw e; }
        }
        // JS/Python construct the sandbox directly from the POST response; no
        // follow-up GET (an insecure sandbox legitimately has no envd token).
        String createdId = created.getId();
        // JS/Python append clientID based on the configured control-plane
        // domain, not the runtime domain: a legacy cluster that omits the
        // domain from its response still gets sandboxID-clientID.
        // JS: sandboxDomain = response.domain ?? connectionConfig.domain
        // (sandbox/index.ts:168), so the response wins and the fallback honors
        // the full configured chain (opts.domain -> NOVITA_DOMAIN -> default).
        String domain = created.getDomain() != null && !created.getDomain().trim().isEmpty()
                ? created.getDomain() : opts.resolvedDomain();
        created.setDomain(domain);
        if (client.isLegacyApiDomain() && created.getClientID() != null && !created.getClientID().isEmpty()) {
            createdId += "-" + created.getClientID();
        }
        return new Sandbox(createdId, client, created);
    }

    /** Matches JS {@code DEFAULT_NOVITA_DOMAIN} in connectionConfig.ts. */
    static final String DEFAULT_DOMAIN = "us-phx-1.sandbox.novita.ai";


    /**
     * Connects to an existing sandbox.
     *
     * @param sandboxId The ID of the sandbox to connect to
     * @param opts Optional connection configuration
     * @return A Sandbox instance connected to the existing sandbox
     * @throws SandboxException if connection fails
     */
    public static Sandbox connect(String sandboxId, SandboxOpts opts) throws SandboxException {
        if (opts == null) {
            opts = new SandboxOpts();
        }
        if (sandboxId == null || sandboxId.trim().isEmpty()) {
            throw new SandboxException("sandboxId is required");
        }

        // JS/Python connect() POSTs /sandboxes/{id}/connect, which resumes a
        // paused sandbox and returns the current envd credentials.
        ApiClient client = new ApiClient(opts);
        SandboxInfo info;
        try { info = client.connectSandbox(sandboxId, opts); } catch (Exception e) { client.close(); throw e; }
        if (info.getId() == null || info.getId().trim().isEmpty()) {
            info.setId(sandboxId);
        }
        // Legacy clusters address the sandbox as sandboxID-clientID.
        if (client.isLegacyApiDomain() && info.getClientID() != null && !info.getClientID().isEmpty()) {
            info.setId(info.getId() + "-" + info.getClientID());
        }
        // JS: sandboxDomain = response.domain ?? connectionConfig.domain
        // (sandbox/index.ts:168); fill from the full configured chain.
        if (info.getDomain() == null || info.getDomain().trim().isEmpty()) {
            info.setDomain(opts.resolvedDomain());
        }
        return new Sandbox(info.getId(), client, info);
    }

    /**
     * Lists sandbox metadata without opening a client or runtime handle per item.
     * Call {@link #connect(String, SandboxOpts)} only for an item that needs to be operated on.
     */
    public static SandboxInfo[] list(SandboxOpts opts) throws SandboxException {
        if (opts == null) opts = new SandboxOpts();
        ApiClient listClient = new ApiClient(opts);
        try {
            return listClient.listSandboxes();
        } finally {
            listClient.close();
        }
    }

    /**
     * Lists one page of sandboxes with filters, matching JS/Python
     * {@code Sandbox.list(opts)}; page through with the returned next token.
     */
    public static SandboxListResult list(SandboxListOptions options) throws SandboxException {
        SandboxListOptions opts = options == null ? new SandboxListOptions() : options;
        ApiClient listClient = new ApiClient(opts);
        try {
            return listClient.listSandboxesPage(opts);
        } finally {
            listClient.close();
        }
    }

    /** Lists every remaining page using the supplied connection options and filters. */
    public static SandboxInfo[] listAll(SandboxListOptions options) throws SandboxException {
        SandboxListOptions opts=options==null?new SandboxListOptions():options;
        try(ApiClient client=new ApiClient(opts)){return client.listAllSandboxes(opts);}
    }

    public static java.util.concurrent.CompletableFuture<SandboxListResult> listAsync(SandboxListOptions options) {
        return AsyncSupport.supplyAsync(() -> { try { return list(options); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }

    /** Creates a template control-plane client using the same credentials. */
    public static Template templates(SandboxOpts opts) throws SandboxException {
        if (opts == null) opts = new SandboxOpts();
        return new Template(new ApiClient(opts));
    }

    private Sandbox(String id, ApiClient client) { this(id, client, null); }
    Sandbox(String id, ApiClient client, SandboxInfo connectionInfo) {
        this.id = id;
        this.client = client;
        this.connectionInfo = connectionInfo;
        this.runtimeInfo = connectionInfo;
        this.files = new Filesystem(this);
        this.commands = new Commands(this);
        this.pty = new Pty(this);
        this.git = new Git(this);
    }

    /**
     * Gets the sandbox ID.
     */
    public String getId() {
        return id;
    }
    boolean envdAtLeast(String requiredVersion) throws SandboxException {
        SandboxInfo info = runtimeInfo;
        if (info == null) {
            info = client.getSandboxInfo(id);
            runtimeInfo = info;
        }
        return compareVersions(info.getEnvdVersion(), requiredVersion) >= 0;
    }

    boolean isLegacyEnvd() throws SandboxException { return !envdAtLeast("0.4.0"); }

    Map<String,String> envdAuthenticationHeaders(String requestedUser) throws SandboxException {
        String user = requestedUser;
        if (user == null && isLegacyEnvd()) user = "user";
        if (user == null || user.isEmpty()) return java.util.Collections.emptyMap();
        String value = java.util.Base64.getEncoder().encodeToString(
                (user + ":").getBytes(java.nio.charset.StandardCharsets.UTF_8));
        return java.util.Collections.singletonMap("Authorization", "Basic " + value);
    }

    private static int compareVersions(String left, String right) {
        String[] a = (left == null ? "0" : left).split("[-+]", 2)[0].split("\\.");
        String[] b = (right == null ? "0" : right).split("[-+]", 2)[0].split("\\.");
        int length = Math.max(a.length, b.length);
        for (int i = 0; i < length; i++) {
            int x = i < a.length ? numericVersionPart(a[i]) : 0;
            int y = i < b.length ? numericVersionPart(b[i]) : 0;
            if (x != y) return Integer.compare(x, y);
        }
        return 0;
    }

    private static int numericVersionPart(String value) {
        try { return Integer.parseInt(value); }
        catch (NumberFormatException ignored) { return 0; }
    }

    /**
     * Gets the filesystem interface for this sandbox.
     */
    public Filesystem files() {
        return files;
    }

    /**
     * Gets the commands interface for this sandbox.
     */
    public Commands commands() {
        return commands;
    }

    public Pty pty() { return pty; }

    /**
     * Gets the Git interface for this sandbox.
     */
    public Git git() {
        return git;
    }

    /**
     * Terminates the sandbox and releases all client resources.
     *
     * @return {@code true} if the sandbox was killed; {@code false} if it was already gone (404)
     * @throws SandboxException if termination fails
     */
    public boolean kill() throws SandboxException {
        synchronized (this) {
            if (closed) return true;
            // Match the JS/Python behavior: a failed control-plane request must
            // remain retryable and must not make a still-running sandbox appear closed.
            boolean killed = client.killSandbox(id);
            closed = true;
            release();
            return killed;
        }
    }

    /** Closes the envd RPC client and the control-plane HTTP client. */
    private void release() {
        ConnectRpcClient r = rpc;
        if (r != null) { r.close(); rpc = null; }
        client.close();
    }

    @Override public void close() throws SandboxException { kill(); }
    public boolean isClosed() { return closed; }

    /**
     * Pauses the sandbox, matching JS/Python: 409 (already paused) reports
     * {@code false} instead of failing.
     */
    public boolean pause() throws SandboxException { return client.pauseSandbox(id); }

    /** Extends memory by the requested amount in MiB (modern clusters only). */
    public void resize(int memoryMiB) throws SandboxException {
        if (memoryMiB <= 0) throw new SandboxException("memoryMiB must be positive");
        requireModernDomain("Sandbox.resize");
        JsonObject payload = new JsonObject(); payload.addProperty("requested_size_mib", memoryMiB);
        client.postSandbox(id, "hotplug-memory", payload);
    }

    public void setTimeout(int seconds) throws SandboxException {
        if (seconds < 0) throw new SandboxException("seconds must be non-negative");
        client.setTimeout(id, seconds);
    }

    /**
     * Resets the sandbox to its latest snapshot and resumes it, matching
     * {@code reset()} in the JS/Python SDKs (resume defaults to true).
     *
     * @return {@code true} on success; {@code false} when the control plane
     *         answered 409 (the sandbox could not be reset right now)
     */
    public boolean reset() throws SandboxException { return reset(new ResetOptions()); }

    /** Resets the sandbox with explicit options (resume, secure, network, timeout). */
    public boolean reset(ResetOptions options) throws SandboxException {
        com.google.gson.JsonObject result = client.resetSandbox(id, options);
        // Persist the rotated traffic token for secure sandboxes, matching the
        // JS/Python fix "persist traffic token after sandbox reset".
        if (result != null && result.has("trafficAccessToken") && !result.get("trafficAccessToken").isJsonNull()) {
            String token = result.get("trafficAccessToken").getAsString();
            SandboxInfo i = runtimeInfo != null ? runtimeInfo : connectionInfo;
            if (i == null) i = new SandboxInfo();
            i.setTrafficAccessToken(token);
            runtimeInfo = i;
        }
        // The envd domain may change across a reset, so drop the cached value.
        cachedDomain = null;
        mcpToken = null;
        ConnectRpcClient r = rpc;
        if (r != null) { r.close(); rpc = null; }
        // JS/Python semantics: 409 -> false; any 2xx (even an empty body) -> true.
        return result != null;
    }
    public void setNetwork(NetworkOptions options) throws SandboxException {
        requireModernDomain("Sandbox.setNetwork");
        client.setNetwork(id, options);
    }
    public java.util.concurrent.CompletableFuture<Boolean> pauseAsync(){return AsyncSupport.supplyAsync(()->{try{return pause();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Boolean> killAsync(){return AsyncSupport.supplyAsync(()->{try{return kill();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> resizeAsync(int memoryMiB){return AsyncSupport.runAsync(()->{try{resize(memoryMiB);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> setTimeoutAsync(int seconds){return AsyncSupport.runAsync(()->{try{setTimeout(seconds);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Boolean> resetAsync(){return AsyncSupport.supplyAsync(()->{try{return reset();}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Boolean> resetAsync(ResetOptions options){return AsyncSupport.supplyAsync(()->{try{return reset(options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<Void> setNetworkAsync(NetworkOptions options){return AsyncSupport.runAsync(()->{try{setNetwork(options);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<SandboxInfo> mountVolumeAsync(String name,String path){return AsyncSupport.supplyAsync(()->{try{return mountVolume(name,path);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public java.util.concurrent.CompletableFuture<SandboxInfo> unmountVolumeAsync(String path,boolean force){return AsyncSupport.supplyAsync(()->{try{return unmountVolume(path,force);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    /**
     * Sandbox metrics, matching JS/Python {@code getMetrics}: modern clusters
     * accept {@code start}/{@code end} epoch-second filters; legacy responses
     * carry MiB values that are normalized to bytes.
     */
    public SandboxMetrics[] metrics() throws SandboxException { return metrics(null, null); }
    public SandboxMetrics[] metrics(java.time.Instant start, java.time.Instant end) throws SandboxException {
        if (client.isLegacyApiDomain()) {
            SandboxMetrics[] metrics = client.request("GET", "/sandboxes/"+segment(id)+"/metrics", null, SandboxMetrics[].class);
            if (metrics != null) for (SandboxMetrics m : metrics) m.applyLegacyNormalization();
            return metrics;
        }
        java.util.Map<String,String> query = new java.util.LinkedHashMap<>();
        if (start != null) query.put("start", String.valueOf(start.getEpochSecond()));
        if (end != null) query.put("end", String.valueOf(end.getEpochSecond()));
        return client.request("GET", "/sandboxes/"+segment(id)+"/metrics", query, null, SandboxMetrics[].class);
    }

    public SandboxQuota quota() throws SandboxException {
        requireModernDomain("Sandbox.quota");
        return client.request("GET", "/sandboxes/quota", null, SandboxQuota.class);
    }

    /**
     * Mounts a volume by name at the given path, matching JS/Python
     * {@code mountVolume(name, path)}: POST /sandboxes/{id}/volume-mounts.
     *
     * @throws SandboxException on legacy domains, matching the other SDKs
     */
    public SandboxInfo mountVolume(String name, String path) throws SandboxException {
        requireModernDomain("Sandbox.mountVolume");
        return client.mountVolume(id, name, path);
    }

    /**
     * Unmounts the volume mounted at the given path, matching JS/Python
     * {@code unmountVolume(path, force)}: DELETE /sandboxes/{id}/volume-mounts.
     */
    public SandboxInfo unmountVolume(String path) throws SandboxException { return unmountVolume(path, false); }
    public SandboxInfo unmountVolume(String path, boolean force) throws SandboxException {
        requireModernDomain("Sandbox.unmountVolume");
        return client.unmountVolume(id, path, force);
    }

    private void requireModernDomain(String api) throws SandboxException {
        // JS/Python raiseIfLegacy checks the configured control-plane domain
        // (config.isLegacyDomain), not the sandbox's runtime domain: a legacy
        // cluster that omits the domain from its response must still reject
        // modern-only operations.
        if (client.isLegacyApiDomain()) {
            throw new SandboxException(api + " is only supported on modern clusters");
        }
    }

    /**
     * Creates a persistent snapshot of this sandbox on modern (v2) clusters,
     * matching JS/Python: POST /sandboxes/{id}/snapshots with an empty body.
     */
    public SnapshotInfo createSnapshot() throws SandboxException {
        requireModernDomain("Sandbox.createSnapshot");
        return client.request("POST", "/sandboxes/" + segment(id) + "/snapshots", new java.util.LinkedHashMap<String, Object>(), SnapshotInfo.class);
    }

    /**
     * Lists snapshots, matching JS/Python: static, optionally filtered by
     * source sandbox, paginated through the {@code x-next-token} header.
     */
    public static SnapshotListResult listSnapshots(SnapshotListOptions options) throws SandboxException {
        SnapshotListOptions opts = options == null ? new SnapshotListOptions() : options;
        ApiClient listClient = new ApiClient(opts);
        if (listClient.isLegacyApiDomain()) { listClient.close(); throw new SandboxException("Sandbox.listSnapshots is only supported on modern clusters"); }
        try {
            return listClient.listSnapshotsPage(opts);
        } finally {
            listClient.close();
        }
    }

    /** Convenience overload listing this sandbox's snapshots. */
    public SnapshotListResult listSnapshots(Integer limit, String nextToken) throws SandboxException {
        SnapshotListOptions options = new SnapshotListOptions();
        options.setApiKey(client.apiKey());
        options.setAccessToken(client.options().getAccessToken());
        options.setLogger(client.options().getLogger());
        options.setHeaders(client.options().getHeaders());
        options.setRequestTimeoutSeconds(client.requestTimeoutSeconds());
        options.setBaseUrl(client.baseUrl());
        // Preserve the configured domain (JS resolveApiOpts spreads the whole
        // connection config) so legacy detection is not re-resolved from the
        // environment and bypassed.
        if (client.configuredDomain() != null) options.setDomain(client.configuredDomain());
        options.setSandboxID(id);
        if (limit != null) options.setLimit(limit);
        if (nextToken != null && !nextToken.isEmpty()) options.setNextToken(nextToken);
        return listSnapshots(options);
    }

    /** Deletes a snapshot. A missing snapshot is reported as false. */
    public static boolean deleteSnapshot(String snapshotId, SandboxOpts opts) throws SandboxException {
        if (snapshotId == null || snapshotId.trim().isEmpty()) throw new SandboxException("snapshotId is required");
        if (opts == null) opts = new SandboxOpts();
        ApiClient c = new ApiClient(opts);
        try {
            c.request("DELETE", "/templates/" + segment(snapshotId), null, null);
            return true;
        } catch (NotFoundException e) {
            return false;
        } finally { c.close(); }
    }

    public CompletableFuture<SnapshotInfo> createSnapshotAsync() { return AsyncSupport.supplyAsync(() -> { try { return createSnapshot(); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }
    public CompletableFuture<SnapshotListResult> listSnapshotsAsync(Integer limit, String nextToken) { return AsyncSupport.supplyAsync(() -> { try { return listSnapshots(limit, nextToken); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }
    public static CompletableFuture<SnapshotListResult> listSnapshotsAsync(SnapshotListOptions options) { return AsyncSupport.supplyAsync(() -> { try { return listSnapshots(options); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } }); }

    /**
     * Retrieves information about the sandbox.
     *
     * @return SandboxInfo containing sandbox details
     * @throws SandboxException if retrieval fails
     */
    public SandboxInfo info() throws SandboxException {
        return client.getSandboxInfo(id);
    }

    /**
     * Returns the envd RPC client, creating it on first use.
     *
     * @param timeout how long a unary RPC may take; streaming calls are not bounded
     * @throws SandboxException if the sandbox has no envd domain
     */
    public synchronized ConnectRpcClient rpc(java.time.Duration timeout) throws SandboxException {
        if (rpc == null) {
            SandboxInfo i = runtimeInfo != null ? runtimeInfo : info();
            runtimeInfo = i;
            String domain = envdBaseUrl();
            rpc = new ConnectRpcClient(domain, i.getEnvdAccessToken(), id, timeout, client.options().getHeaders(),client.options().getLogger());
        }
        return rpc;
    }

    public synchronized ConnectRpcClient rpc() throws SandboxException { return rpc(java.time.Duration.ofSeconds(client.requestTimeoutSeconds())); }

    public java.util.concurrent.CompletableFuture<SandboxInfo> infoAsync() {
        return AsyncSupport.supplyAsync(() -> { try { return info(); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }

    /**
     * Builds the public URL for a sandbox port.
     *
     * <p>The envd domain is cached after the first lookup; {@link #reset} clears it.
     */
    public String portUrl(int port) throws SandboxException {
        String override = client.options().getSandboxUrl();
        if (override != null && !override.trim().isEmpty()) return override.replaceAll("/+$", "");
        return (client.options().isDebug() ? "http://" : "https://") + getHost(port);
    }

    /**
     * Public host for a sandbox port, matching JS {@code getHost(port)}:
     * {@code <port>-<sandboxID>.<sandboxDomain>}.
     */
    public String getHost(int port) throws SandboxException {
        if (client.options().isDebug()) return "localhost:" + port;
        String d = sandboxDomain();
        return port + "-" + id + "." + d;
    }

    /** Resolves (and caches) the sandbox domain without the URL scheme. */
    private String sandboxDomain() throws SandboxException {
        String d = cachedDomain;
        if (d == null) {
            // Modern connect responses carry the envd domain, while a later
            // GET /sandboxes/{id} may intentionally omit it. Prefer the
            // connection metadata captured during create/connect.
            d = runtimeInfo != null ? runtimeInfo.getDomain() : null;
            if (d == null) d = info().getDomain();
            if (d == null) throw new SandboxException("sandbox domain unavailable");
            cachedDomain = d;
        }
        return d.replaceFirst("^https?://", "").replaceAll("/+$", "");
    }

    /**
     * URL of the MCP server for this sandbox, matching JS {@code getMcpUrl()}.
     *
     * @throws SandboxException on legacy domains, matching the other SDKs
     */
    public String getMcpUrl() throws SandboxException {
        requireModernDomain("Sandbox.getMcpUrl");
        return "https://" + getHost(MCP_PORT) + "/mcp";
    }

    /**
     * Reads the MCP gateway token for this sandbox, matching JS
     * {@code getMcpToken()}. The token is read once as root and cached.
     *
     * @return the token, or null when MCP is not enabled
     * @throws SandboxException on legacy domains or read failure
     */
    public String getMcpToken() throws SandboxException {
        requireModernDomain("Sandbox.getMcpToken");
        String token = mcpToken;
        if (token == null) {
            FilesystemReadOptions opts = new FilesystemReadOptions();
            opts.setUsername("root");
            token = new String(files.read("/etc/mcp-gateway/.token", opts), java.nio.charset.StandardCharsets.UTF_8);
            mcpToken = token;
        }
        return token;
    }

    /**
     * Checks whether the sandbox envd is healthy, matching JS/Python
     * {@code isRunning()}: GET /health, where a 502 response means not running.
     */
    public boolean isRunning() throws SandboxException {
        String envdBase = envdBaseUrl();
        okhttp3.Request.Builder builder = new okhttp3.Request.Builder().url(envdBase + "/health");
        String token = currentInfo().getEnvdAccessToken();
        if (token != null && !token.isEmpty()) builder.header("X-Access-Token", token);
        for (java.util.Map.Entry<String, String> header : envdAuthenticationHeaders(null).entrySet()) {
            builder.header(header.getKey(), header.getValue());
        }
        try (okhttp3.Response response = AsyncSupport.execute(httpClient().newCall(builder.build()))) {
            if (response.code() == 502) return false;
            if (!response.isSuccessful()) {
                throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            }
            return true;
        } catch (java.io.IOException e) {
            throw new SandboxException("Health check failed", e);
        }
    }

    /**
     * URL for uploading a file to this sandbox, matching JS/Python
     * {@code uploadUrl(path, user, useSignatureExpiration)}.
     *
     * <p>Secure sandboxes sign the URL with the envd access token. Expiration
     * can only be used when the sandbox is secure.
     */
    public String uploadUrl(String path, String user, Integer useSignatureExpiration) throws SandboxException {
        return fileUrl(path, "write", user, useSignatureExpiration);
    }

    /** URL for downloading a file from this sandbox. */
    public String downloadUrl(String path, String user, Integer useSignatureExpiration) throws SandboxException {
        return fileUrl(path, "read", user, useSignatureExpiration);
    }

    public String uploadUrl() throws SandboxException { return uploadUrl("", null, null); }
    public String downloadUrl(String path) throws SandboxException { return downloadUrl(path, null, null); }

    private String fileUrl(String path, String operation, String user, Integer signatureExpiration) throws SandboxException {
        SandboxInfo info = currentInfo();
        String envdAccessToken = info.getEnvdAccessToken();
        boolean useSignature = envdAccessToken != null && !envdAccessToken.isEmpty();
        if (!useSignature && signatureExpiration != null) {
            throw new SandboxException("Signature expiration can be used only when sandbox is created as secured.");
        }
        // Mirror the filesystem default-user rule: envd < 0.4.0 requires an
        // explicit "user"; newer envd versions default the user server-side.
        String username = user;
        if (username == null && isLegacyEnvd()) username = "user";
        java.util.Map<String, String> query = new java.util.LinkedHashMap<>();
        if (path != null && !path.isEmpty()) query.put("path", path);
        if (username != null && !username.isEmpty()) query.put("username", username);
        if (useSignature) {
            Signature.Result signature = Signature.get(path == null ? "" : path, operation, username, envdAccessToken, signatureExpiration);
            query.put("signature", signature.signature);
            if (signature.expiration != null) query.put("signature_expiration", String.valueOf(signature.expiration));
        }
        StringBuilder url = new StringBuilder(envdBaseUrl()).append("/files");
        String separator = "?";
        for (java.util.Map.Entry<String, String> entry : query.entrySet()) {
            url.append(separator).append(java.net.URLEncoder.encode(entry.getKey(), java.nio.charset.StandardCharsets.UTF_8))
               .append('=').append(java.net.URLEncoder.encode(entry.getValue(), java.nio.charset.StandardCharsets.UTF_8));
            separator = "&";
        }
        return url.toString();
    }

    /** Lazily fetches and caches sandbox runtime info, mirroring {@link #rpc()}'s lazy connect. */
    private SandboxInfo currentInfo() throws SandboxException {
        SandboxInfo info = runtimeInfo;
        if (info == null) {
            info = info();
            runtimeInfo = info;
        }
        return info;
    }

    /**
     * Base URL of the envd HTTP API, using the same domain rule as
     * {@link #rpc()}: a scheme-prefixed domain is used as-is (test setups),
     * otherwise the public host {@code 49983-<id>.<domain>} is used.
     */
    private String envdBaseUrl() throws SandboxException {
        String override = client.options().getSandboxUrl();
        if (override != null && !override.trim().isEmpty()) return override.replaceAll("/+$", "");
        if (client.options().isDebug()) return "http://localhost:" + ENVD_PORT;
        String domain = currentInfo().getDomain();
        if (domain == null || domain.trim().isEmpty()) domain = DEFAULT_DOMAIN;
        if (domain.startsWith("http")) return domain.replaceAll("/+$", "");
        return "https://" + getHost(ENVD_PORT);
    }

    private static final int ENVD_PORT = 49983;
    private static final int MCP_PORT = 50005;
    private volatile String mcpToken;

    /**
     * Commits the sandbox to a snapshot template on legacy clusters, matching
     * the deprecated JS/Python {@code commit(alias)}.
     *
     * @throws SandboxException on modern clusters, matching the other SDKs
     */
    public TemplateInfo commit(String alias) throws SandboxException {
        if (!client.isLegacyApiDomain()) {
            throw new SandboxException("Sandbox.commit is only supported on legacy domains; use createSnapshot on modern clusters");
        }
        return client.commitSandbox(id, alias);
    }

    /**
     * Clones this sandbox, matching JS/Python {@code clone(count, ...)}.
     * Returns connected Sandbox instances and the snapshot template ID.
     */
    public CloneResult clone(CloneOptions options) throws SandboxException {
        if (options == null) options = new CloneOptions();
        com.google.gson.JsonObject response = this.client.cloneSandbox(id, options);
        String domain = sandboxDomain();
        final ApiClient shared = this.client;
        return buildCloneResult(response, () -> new ApiClient(shared.options()), domain, shared.isLegacyApiDomain());
    }

    /** Clones a sandbox by ID; the source sandbox does not need a live connection. */
    public static CloneResult clone(String sandboxId, CloneOptions options, SandboxOpts opts) throws SandboxException {
        if (sandboxId == null || sandboxId.trim().isEmpty()) throw new SandboxException("sandboxId is required");
        if (opts == null) opts = new SandboxOpts();
        ApiClient requestClient = new ApiClient(opts);
        try {
            com.google.gson.JsonObject response = requestClient.cloneSandbox(sandboxId, options);
            final SandboxOpts connectionOpts = opts.copy();
            return buildCloneResult(response, () -> new ApiClient(connectionOpts), opts.resolvedDomain(), requestClient.isLegacyApiDomain());
        } finally {
            requestClient.close();
        }
    }

    interface CloneClientSupplier {
        ApiClient newClient() throws SandboxException;
    }

    /**
     * Each cloned sandbox owns its control-plane client, mirroring the JS SDK
     * where every clone is a fully independent Sandbox instance.
     */
    static CloneResult buildCloneResult(com.google.gson.JsonObject response, CloneClientSupplier clientSupplier, String domain, boolean legacyDomain) throws SandboxException {
        // JS/Python clones inherit the full connection config, so the envd
        // domain must never fall back to a legacy default.
        if (domain == null || domain.trim().isEmpty()) domain = DEFAULT_DOMAIN;
        CloneResult result = new CloneResult();
        com.google.gson.JsonArray items = response != null && response.has("sandboxes") && response.get("sandboxes").isJsonArray()
                ? response.getAsJsonArray("sandboxes") : new com.google.gson.JsonArray();
        java.util.List<Sandbox> sandboxes = new java.util.ArrayList<>();
        for (com.google.gson.JsonElement element : items) {
            com.google.gson.JsonObject item = element.getAsJsonObject();
            String sandboxIdValue = stringField(item, "sandboxID");
            String clientId = stringField(item, "clientID");
            // JS/Python append clientID only when the configured control-plane
            // domain is legacy (config.is_legacy_domain).
            String cloneId = sandboxIdValue == null ? "" : (legacyDomain && clientId != null && !clientId.isEmpty() ? sandboxIdValue + "-" + clientId : sandboxIdValue);
            SandboxInfo info = new SandboxInfo();
            info.setId(cloneId);
            info.setDomain(domain);
            info.setEnvdVersion(stringField(item, "envdVersion"));
            info.setEnvdAccessToken(stringField(item, "envdAccessToken"));
            info.setTrafficAccessToken(stringField(item, "trafficAccessToken"));
            sandboxes.add(new Sandbox(cloneId, clientSupplier.newClient(), info));
        }
        result.setSandboxes(sandboxes.toArray(new Sandbox[0]));
        result.setCount(sandboxes.size());
        if (response != null) result.setSnapshotTemplateId(stringField(response, "snapshotTemplateId"));
        return result;
    }

    private static String stringField(com.google.gson.JsonObject object, String field) {
        if (object == null || !object.has(field) || object.get(field).isJsonNull()) return null;
        return object.get(field).getAsString();
    }

    /**
     * Queries sandbox lifecycle events, matching JS/Python
     * {@code getEvents(opts)}; the options also carry connection settings.
     */
    public static SandboxEventsResult getEvents(SandboxEventsOptions options) throws SandboxException {
        SandboxEventsOptions opts = options == null ? new SandboxEventsOptions() : options;
        ApiClient apiClient = new ApiClient(opts);
        try {
            return apiClient.getEvents(opts.getStartTime(), opts.getEndTime(), opts.getSandboxID(), opts.getTemplateID(),
                    opts.getEvents(), opts.getState(), opts.getOrderAsc(), opts.getOffset(), opts.getLimit());
        } finally { apiClient.close(); }
    }

    public java.util.concurrent.CompletableFuture<Boolean> isRunningAsync() {
        return AsyncSupport.supplyAsync(() -> { try { return isRunning(); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public java.util.concurrent.CompletableFuture<String> getMcpTokenAsync() {
        return AsyncSupport.supplyAsync(() -> { try { return getMcpToken(); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public java.util.concurrent.CompletableFuture<String> uploadUrlAsync(String path, String user, Integer useSignatureExpiration) {
        return AsyncSupport.supplyAsync(() -> { try { return uploadUrl(path, user, useSignatureExpiration); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public java.util.concurrent.CompletableFuture<String> downloadUrlAsync(String path, String user, Integer useSignatureExpiration) {
        return AsyncSupport.supplyAsync(() -> { try { return downloadUrl(path, user, useSignatureExpiration); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public static java.util.concurrent.CompletableFuture<SandboxEventsResult> getEventsAsync(SandboxEventsOptions options) {
        return AsyncSupport.supplyAsync(() -> { try { return getEvents(options); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public java.util.concurrent.CompletableFuture<CloneResult> cloneAsync(CloneOptions options) {
        return AsyncSupport.supplyAsync(() -> { try { return clone(options); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }
    public java.util.concurrent.CompletableFuture<TemplateInfo> commitAsync(String alias) {
        return AsyncSupport.supplyAsync(() -> { try { return commit(alias); } catch (Exception e) { throw new java.util.concurrent.CompletionException(e); } });
    }

    ApiClient getClient() {
        return client;
    }

    /** Headers for applications running inside a protected sandbox. */
    public Map<String,String> portHeaders() throws SandboxException {
        Map<String,String> headers = new java.util.LinkedHashMap<>(client.options().getHeaders());
        SandboxInfo info = currentInfo();
        if (info.getEnvdAccessToken() != null) headers.put("X-Access-Token", info.getEnvdAccessToken());
        if (info.getTrafficAccessToken() != null) headers.put("E2B-Traffic-Access-Token", info.getTrafficAccessToken());
        return java.util.Collections.unmodifiableMap(headers);
    }
    public String getTrafficAccessToken() throws SandboxException { return currentInfo().getTrafficAccessToken(); }
    public int getRequestTimeoutSeconds() { return client.requestTimeoutSeconds(); }
    /** Refreshes credentials and resumes this sandbox while preserving this Java handle. */
    public Sandbox reconnect() throws SandboxException { return reconnect(null); }
    public synchronized Sandbox reconnect(SandboxOpts options) throws SandboxException {
        SandboxInfo info = client.connectSandbox(id, options == null ? client.options() : options);
        if (info.getDomain() == null) info.setDomain(currentInfo().getDomain());
        runtimeInfo = info; cachedDomain = null; mcpToken = null;
        if (rpc != null) { rpc.close(); rpc = null; }
        return this;
    }

    /**
     * Shared HTTP client for requests against sandbox ports.
     *
     * <p>It carries no API-key header and shares the connection pool and
     * dispatcher with the control-plane client. Do <em>not</em> close it; derive
     * a per-call variant with {@code newBuilder()} when a different timeout is
     * needed, so the pool is still shared.
     */
    public okhttp3.OkHttpClient httpClient() {
        return client.httpClient();
    }
    <T> T request(String method, String path, Object payload, Class<T> type) throws SandboxException { return client.request(method, "/sandboxes/" + segment(id) + path, payload, type); }

    private static String segment(String value) { return java.net.URLEncoder.encode(value, java.nio.charset.StandardCharsets.UTF_8).replace("+", "%20"); }

    @Override public boolean equals(Object o) {
        if (this == o) return true;
        if (!(o instanceof Sandbox)) return false;
        Sandbox other = (Sandbox) o;
        return id == null ? other.id == null : id.equals(other.id);
    }

    @Override public int hashCode() { return id == null ? 0 : id.hashCode(); }

    @Override public String toString() { return "Sandbox{" + id + "}"; }
}
