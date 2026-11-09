package ai.novita.sandbox.core;

import com.google.gson.Gson;
import com.google.gson.JsonObject;
import okhttp3.*;

import java.io.IOException;
import java.util.concurrent.TimeUnit;
import java.util.Map;

/**
 * Internal API client for communicating with Novita Sandbox API.
 */
class ApiClient implements AutoCloseable {
    private static String segment(String value) { return java.net.URLEncoder.encode(value, java.nio.charset.StandardCharsets.UTF_8).replace("+", "%20"); }
    private static final String DEFAULT_BASE_URL = "https://api.us-phx-1.sandbox.novita.ai";
    private static final MediaType JSON = MediaType.get("application/json; charset=utf-8");
    private static final MediaType OCTET_STREAM = MediaType.get("application/octet-stream");

    private final String baseUrl;
    private final String apiKey;
    /** Configured envd domain; JS/Python legacy behavior keys off this, not the API URL. */
    private final String configuredDomain;
    private final OkHttpClient httpClient;
    private final Gson gson;
    private final Headers headers;
    private final SandboxOpts options;

    ApiClient(String apiKey, String baseUrl) throws SandboxException {
        this(apiKey, baseUrl, 30);
    }

    ApiClient(String apiKey, String baseUrl, int timeoutSeconds) throws SandboxException {
        this(apiKey, baseUrl, timeoutSeconds, null);
    }

    ApiClient(String apiKey, String baseUrl, int timeoutSeconds, String configuredDomain) throws SandboxException {
        this(connectionOptions(apiKey, baseUrl, timeoutSeconds, configuredDomain));
    }
    private static SandboxOpts connectionOptions(String key, String url, int timeout, String domain) {
        SandboxOpts opts = new SandboxOpts(); opts.setApiKey(key); opts.setBaseUrl(url); opts.setRequestTimeoutSeconds(timeout);
        if (domain == null && url != null) {
            String host = java.net.URI.create(url).getHost();
            domain = host != null && host.startsWith("api.") ? host.substring(4) : host;
        }
        if (domain != null) opts.setDomain(domain);
        return opts;
    }
    ApiClient(SandboxOpts opts) throws SandboxException {
        this.options = opts.copy();
        String key = opts.getApiKey();
        if (key == null || key.isEmpty()) key = System.getenv("NOVITA_API_KEY");
        String token = opts.getAccessToken();
        if ((key == null || key.isEmpty()) && (token == null || token.isEmpty()) && !opts.isDebug())
            throw new AuthenticationException("API key or access token required");
        this.apiKey = key;
        this.baseUrl = opts.resolvedBaseUrl().replaceAll("/+$", "");
        this.configuredDomain = opts.resolvedDomain();
        Headers.Builder headerBuilder = new Headers.Builder();
        opts.getHeaders().forEach(headerBuilder::set);
        if (key != null && !key.isEmpty()) headerBuilder.set("X-API-Key", key);
        if (token != null && !token.isEmpty()) headerBuilder.set("Authorization", "Bearer " + token);
        this.headers = headerBuilder.build();
        int timeoutSeconds = opts.getRequestTimeoutSeconds();
        OkHttpClient.Builder httpBuilder = new OkHttpClient.Builder()
                .connectTimeout(timeoutSeconds, TimeUnit.SECONDS).readTimeout(timeoutSeconds, TimeUnit.SECONDS)
                .writeTimeout(timeoutSeconds, TimeUnit.SECONDS).callTimeout(timeoutSeconds, TimeUnit.SECONDS);
        configureProxy(httpBuilder);
        configureLogging(httpBuilder,opts.getLogger());
        this.httpClient = httpBuilder.build();
        this.gson = new Gson();
    }
    SandboxOpts options() { return options.copy(); }
    int requestTimeoutSeconds() { return options.getRequestTimeoutSeconds(); }

    /**
     * Uses an HTTP(S)_PROXY environment variable when the caller has not
     * configured a JVM proxy, honoring NO_PROXY the same way the JS/Python
     * HTTP stacks do (loopback and listed hosts are always direct).
     */
    static void configureLogging(OkHttpClient.Builder builder,java.util.function.Consumer<String> logger){
        if(logger==null)return;
        builder.addInterceptor(chain->{
            Request request=chain.request();
            try{logger.accept(request.method()+" "+request.url().encodedPath());}catch(RuntimeException ignored){/* Logging must not prevent the request. */}
            return chain.proceed(request);
        });
    }

    static void configureProxy(OkHttpClient.Builder builder) {
        String raw = System.getenv("HTTPS_PROXY");
        if (raw == null || raw.trim().isEmpty()) raw = System.getenv("https_proxy");
        if (raw == null || raw.trim().isEmpty()) raw = System.getenv("HTTP_PROXY");
        if (raw == null || raw.trim().isEmpty()) raw = System.getenv("http_proxy");
        if (raw == null || raw.trim().isEmpty()) return;
        try {
            java.net.URI uri = java.net.URI.create(raw.trim());
            if (uri.getHost() == null || uri.getPort() <= 0) return;
            final java.net.Proxy proxy = new java.net.Proxy(java.net.Proxy.Type.HTTP,
                    new java.net.InetSocketAddress(uri.getHost(), uri.getPort()));
            final java.util.List<String> bypass = noProxyHosts();
            builder.proxySelector(new java.net.ProxySelector() {
                @Override public java.util.List<java.net.Proxy> select(java.net.URI target) {
                    if (target.getHost() != null && isBypassed(target.getHost(), bypass)) {
                        return java.util.Collections.singletonList(java.net.Proxy.NO_PROXY);
                    }
                    return java.util.Collections.singletonList(proxy);
                }
                @Override public void connectFailed(java.net.URI u, java.net.SocketAddress a, java.io.IOException e) { }
            });
        } catch (RuntimeException ignored) {
            // Invalid proxy configuration must not prevent the SDK from starting.
        }
    }

    /** Parses NO_PROXY into host entries; loopback is always included. */
    private static java.util.List<String> noProxyHosts() {
        java.util.List<String> hosts = new java.util.ArrayList<>();
        hosts.add("localhost");
        hosts.add("127.0.0.1");
        hosts.add("::1");
        hosts.add("[::1]");
        String raw = System.getenv("NO_PROXY");
        if (raw == null || raw.trim().isEmpty()) raw = System.getenv("no_proxy");
        if (raw != null) {
            for (String entry : raw.split(",")) {
                String host = entry.trim().toLowerCase(java.util.Locale.ROOT);
                if (!host.isEmpty()) hosts.add(host);
            }
        }
        return hosts;
    }

    /** Matches a NO_PROXY entry: "*" bypasses everything, else exact host or dot/suffix domain match. */
    static boolean isBypassed(String host, java.util.List<String> bypass) {
        String h = host.toLowerCase(java.util.Locale.ROOT);
        for (String entry : bypass) {
            if (entry.equals("*")) return true;
            if (h.equals(entry)) return true;
            if (entry.startsWith(".") && (h.endsWith(entry) || h.equals(entry.substring(1)))) return true;
            if (!entry.startsWith(".") && !entry.equals("localhost") && h.endsWith("." + entry)) return true;
        }
        return false;
    }

    Gson gson() { return gson; }
    String baseUrl() { return baseUrl; }
    String apiKey() { return apiKey; }
    /** Shared HTTP client, so callers do not each build their own connection pool. */
    OkHttpClient httpClient() { return httpClient; }

    /** Appends URL-encoded query parameters to {@code path}. */
    private static String withQuery(String base, String path, Map<String,String> query) {
        StringBuilder target = new StringBuilder(base).append(path);
        if (query != null && !query.isEmpty()) {
            target.append('?');
            boolean first = true;
            for (Map.Entry<String,String> e : query.entrySet()) {
                if (!first) target.append('&');
                first = false;
                target.append(java.net.URLEncoder.encode(e.getKey(), java.nio.charset.StandardCharsets.UTF_8))
                      .append('=')
                      .append(java.net.URLEncoder.encode(e.getValue(), java.nio.charset.StandardCharsets.UTF_8));
            }
        }
        return target.toString();
    }

    /** Reads a response body, transparently decompressing gzip when present. */
    private static byte[] readBody(Response r) throws IOException {
        byte[] body = r.body() == null ? new byte[0] : r.body().bytes();
        if ("gzip".equalsIgnoreCase(r.header("Content-Encoding"))) return gunzip(body);
        return body;
    }

    private static byte[] gunzip(byte[] body) throws IOException {
        try (java.util.zip.GZIPInputStream g = new java.util.zip.GZIPInputStream(new java.io.ByteArrayInputStream(body));
             java.io.ByteArrayOutputStream out = new java.io.ByteArrayOutputStream()) {
            byte[] b = new byte[8192];
            int n;
            while ((n = g.read(b)) >= 0) out.write(b, 0, n);
            return out.toByteArray();
        }
    }

    byte[] raw(String method, String path, Map<String,String> query, byte[] data, String contentType) throws SandboxException {
        return raw(method,path,query,data,contentType,false);
    }
    <T> T multipart(String path, java.util.List<WriteEntry> files, Class<T> type) throws SandboxException { return multipart(path, files, type, null); }
    <T> T multipart(String path, java.util.List<WriteEntry> files, Class<T> type, Map<String,String> query) throws SandboxException { MultipartBody.Builder mb=new MultipartBody.Builder().setType(MultipartBody.FORM); for(WriteEntry f:files) mb.addFormDataPart("file",f.getPath(),RequestBody.create(f.getData(),OCTET_STREAM)); Request req=new Request.Builder().url(withQuery(baseUrl,path,query)).headers(headers).post(mb.build()).build(); try(Response r=AsyncSupport.execute(httpClient.newCall(req))){if(!r.isSuccessful())throw ApiExceptions.from(r.code(),r.body()==null?r.message():r.body().string()); if(type==null||r.body()==null)return null; String body=r.body().string(); com.google.gson.JsonElement parsed=com.google.gson.JsonParser.parseString(body); if(!parsed.isJsonArray())throw new SandboxException("Expected filesystem write response array"); return gson.fromJson(parsed,type);}catch(IOException e){throw AsyncSupport.transportFailure("Multipart request failed: "+path, e);} }
    byte[] rawAcceptGzip(String method,String path,Map<String,String> query) throws SandboxException {
        Request request=new Request.Builder().url(withQuery(baseUrl,path,query)).headers(headers).header("Accept-Encoding","gzip").get().build();
        try(Response r=AsyncSupport.execute(httpClient.newCall(request))){if(!r.isSuccessful()){String detail=r.body()==null?r.message():r.body().string();throw ApiExceptions.from(r.code(),detail);}return readBody(r);}catch(IOException e){throw AsyncSupport.transportFailure("Request failed: "+path, e);}
    }
    byte[] raw(String method, String path, Map<String,String> query, byte[] data, String contentType, boolean gzip) throws SandboxException {
        Request.Builder b=new Request.Builder().url(withQuery(baseUrl,path,query)).headers(headers); if(gzip){b.header("Content-Encoding","gzip");b.header("Accept-Encoding","gzip");} RequestBody body=data==null?null:RequestBody.create(data,MediaType.get(contentType)); b.method(method,body);
        try(Response r=AsyncSupport.execute(httpClient.newCall(b.build()))){if(!r.isSuccessful()){String detail=r.body()==null?r.message():r.body().string();throw ApiExceptions.from(r.code(),detail);}return readBody(r);}catch(IOException e){throw AsyncSupport.transportFailure("Request failed: "+path, e);}
    }

    <T> T request(String method, String path, Object payload, Class<T> type) throws SandboxException {
        Request.Builder b = new Request.Builder().url(baseUrl + path).headers(headers);
        if ("GET".equalsIgnoreCase(method)) b.get();
        else if ("HEAD".equalsIgnoreCase(method)) b.head();
        else if (payload != null) b.method(method, RequestBody.create(gson.toJson(payload), JSON));
        else b.method(method, null);
        try (Response r = AsyncSupport.execute(httpClient.newCall(b.build()))) {
            if (!r.isSuccessful()) throw ApiExceptions.from(r.code(), r.body()==null?r.message():r.body().string());
            if (type == null || r.body() == null) return null;
            return gson.fromJson(r.body().string(), type);
        } catch (IOException e) { throw AsyncSupport.transportFailure("Request failed: " + path, e); }
    }

    <T> T request(String method, String path, Map<String,String> query, Object payload, Class<T> type) throws SandboxException {
        Request.Builder b = new Request.Builder().url(withQuery(baseUrl, path, query)).headers(headers);
        if ("GET".equalsIgnoreCase(method)) b.get();
        else if ("HEAD".equalsIgnoreCase(method)) b.head();
        else if (payload != null) b.method(method, RequestBody.create(gson.toJson(payload), JSON));
        else b.method(method, null);
        try (Response r=AsyncSupport.execute(httpClient.newCall(b.build()))) { if(!r.isSuccessful()) throw ApiExceptions.from(r.code(), r.body()==null?r.message():r.body().string()); if(type==null||r.body()==null)return null; return gson.fromJson(r.body().string(),type); } catch(IOException e){throw AsyncSupport.transportFailure("Request failed: "+path, e);}
    }

    /** Migration has reason-based errors and a per-poll remaining budget. */
    <T> T requestMigration(String method, String path, Map<String,String> query, Object payload, Class<T> type, long remainingNanos) throws SandboxException {
        Request.Builder builder = new Request.Builder().url(withQuery(baseUrl, path, query)).headers(headers);
        if ("GET".equals(method)) builder.get();
        else builder.method(method, RequestBody.create(gson.toJson(payload), JSON));
        // Migration accepts are not idempotent. Surface even retryable HTTP
        // responses before OkHttp's recovery interceptor can replay the POST.
        OkHttpClient migrationHttp = httpClient.newBuilder().retryOnConnectionFailure(false)
                .followRedirects(false).followSslRedirects(false).addNetworkInterceptor(chain -> {
                    Response response = chain.proceed(chain.request());
                    if (response.isSuccessful()) return response;
                    try (Response failed = response) {
                        String body = "";
                        if (failed.body() != null) {
                            // Network interceptors run before transparent gzip decoding.
                            if ("gzip".equalsIgnoreCase(failed.header("Content-Encoding"))) {
                                try (java.io.InputStream decoded = new java.util.zip.GZIPInputStream(failed.body().byteStream())) {
                                    body = new String(decoded.readAllBytes(), java.nio.charset.StandardCharsets.UTF_8);
                                }
                            } else body = failed.body().string();
                        }
                        throw new MigrationHttpFailure(failed.code(), body);
                    }
                }).build();
        Call call = migrationHttp.newCall(builder.build());
        if (remainingNanos > 0) {
            long configured = call.timeout().timeoutNanos();
            call.timeout().timeout(configured > 0 ? Math.min(configured, remainingNanos) : remainingNanos, TimeUnit.NANOSECONDS);
        }
        try (Response response = AsyncSupport.execute(call)) {
            String body = response.body() == null ? "" : response.body().string();
            if (!response.isSuccessful()) throw JobErrors.from(response.code(), body);
            try { return gson.fromJson(body.trim().isEmpty() ? "{}" : body, type); }
            catch (com.google.gson.JsonParseException e) { throw new SandboxException("Invalid migration response", e); }
        } catch (MigrationHttpFailure e) { throw JobErrors.from(e.code, e.body); }
        catch (IOException e) { throw AsyncSupport.transportFailure("Migration request failed", e); }
    }

    private static final class MigrationHttpFailure extends java.net.ProtocolException {
        final int code;
        final String body;
        MigrationHttpFailure(int code, String body) { super("Migration HTTP " + code); this.code=code; this.body=body; }
    }

    BuildInfo requestBuild(String name, TemplateBuildOptions options) throws SandboxException {
        java.util.Map<String,Object> body = new java.util.HashMap<>(); body.put("name", name);
        body.put("cpuCount", options == null ? 2 : options.getCpuCount());
        body.put("memoryMB", options == null ? 1024 : options.getMemoryMb());
        // v3 request-build only accepts name, tags, cpuCount and memoryMB.
        // Alias and build instructions belong to the v2 trigger payload.
        body.put("tags", options == null ? java.util.Collections.emptyList() : options.getTags());
        return request("POST", "/v3/templates", body, BuildInfo.class);
    }

    void triggerBuild(String templateId, String buildId, Map<String,Object> spec) throws SandboxException {
        request("POST", "/v2/templates/" + segment(templateId) + "/builds/" + segment(buildId), spec, null);
    }

    SandboxInfo createSandbox(String template, SandboxOpts opts) throws SandboxException {
        SandboxOpts options = opts == null ? new SandboxOpts() : opts;
        JsonObject payload = new JsonObject();
        // The control-plane NewSandbox schema uses templateID. The JS/Python
        // SDKs expose this as the friendly template argument, but the wire
        // field must remain templateID for both legacy and v2 clusters.
        payload.addProperty("templateID", template);

        if (isLegacyApiDomain()) {
            // Legacy clusters reject the modern fields outright.
            if (options.getMcp() != null) {
                throw new SandboxException("Sandbox.create mcp is not supported on legacy domains");
            }
            NetworkOptions network=options.getNetwork();
            if(network!=null){
                java.util.List<String> unsupported=new java.util.ArrayList<>();
                if(network.getAllowOut()!=null)unsupported.add("allowOut");
                if(network.getDenyOut()!=null)unsupported.add("denyOut");
                if(network.getMaskRequestHost()!=null)unsupported.add("maskRequestHost");
                if(!unsupported.isEmpty())throw new SandboxException("Sandbox.create network only supports allowPublicTraffic on legacy domains; unsupported options: "+String.join(", ",unsupported));
                payload.add("network",gson.toJsonTree(network));
            }
            if (options.getVolumeMounts() != null && !options.getVolumeMounts().isEmpty()) {
                throw new SandboxException("Sandbox.create volumeMounts is not supported on legacy domains");
            }
            if (options.getSecretEnvs() != null && !options.getSecretEnvs().isEmpty()) {
                throw new SandboxException("Sandbox.create secretEnvs is not supported on legacy domains");
            }
            if (options.getMetadata() == null || !"true".equals(options.getMetadata().get("long_running")))
                payload.addProperty("timeout", options.getTimeout());
            payload.add("metadata", gson.toJsonTree(options.getMetadata() == null ? java.util.Collections.emptyMap() : options.getMetadata()));
            payload.add("envVars", gson.toJsonTree(options.getEnvVars() == null ? java.util.Collections.emptyMap() : options.getEnvVars()));
            payload.addProperty("secure", options.getSecure() == null ? false : options.getSecure());
            payload.addProperty("autoPause", options.getAutoPause() == null ? false : options.getAutoPause());
            if (options.getNodeId() != null && !options.getNodeId().isEmpty()) {
                payload.addProperty("nodeID", options.getNodeId());
            }
        } else {
            if (options.getMetadata() != null) {
                payload.add("metadata", gson.toJsonTree(options.getMetadata()));
            }
            // OpenAPI NewSandbox names the field envVars (JS/Python send envVars).
            if (options.getEnvVars() != null) {
                payload.add("envVars", gson.toJsonTree(options.getEnvVars()));
            }
            if (options.getMetadata() == null || !"true".equals(options.getMetadata().get("long_running")))
                payload.addProperty("timeout", options.getTimeout());
            // JS/Python default secure and allow_internet_access to true on
            // modern clusters; legacy clusters default both to false.
            payload.addProperty("secure", options.getSecure() == null ? true : options.getSecure());
            payload.addProperty("allow_internet_access", options.getAllowInternetAccess() == null ? true : options.getAllowInternetAccess());
            if (options.getAutoPause() != null) {
                payload.addProperty("autoPause", options.getAutoPause());
            }
            if (options.getAutoResumeEnabled() != null) {
                JsonObject autoResume = new JsonObject();
                autoResume.addProperty("enabled", options.getAutoResumeEnabled());
                payload.add("autoResume", autoResume);
            }
            if (options.getNetwork() != null) {
                payload.add("network", gson.toJsonTree(options.getNetwork()));
            }
            if (options.getSecretEnvs() != null && !options.getSecretEnvs().isEmpty()) {
                payload.add("secrets", gson.toJsonTree(options.getSecretEnvs()));
            }
            if (options.getMcp() != null) {
                payload.add("mcp", gson.toJsonTree(options.getMcp()));
            }
            if (options.getVolumeMounts() != null && !options.getVolumeMounts().isEmpty()) {
                com.google.gson.JsonArray mounts = new com.google.gson.JsonArray();
                for (java.util.Map.Entry<String, String> mount : options.getVolumeMounts().entrySet()) {
                    JsonObject entry = new JsonObject();
                    entry.addProperty("name", mount.getValue());
                    entry.addProperty("path", mount.getKey());
                    mounts.add(entry);
                }
                payload.add("volumeMounts", mounts);
            }
            if (options.getNodeId() != null && !options.getNodeId().isEmpty()) {
                payload.addProperty("nodeID", options.getNodeId());
            }
        }

        RequestBody body = RequestBody.create(gson.toJson(payload), JSON);
        Request request = new Request.Builder()
                .url(baseUrl + "/sandboxes")
                .headers(headers)
                .post(body)
                .build();

        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (!response.isSuccessful()) {
                throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            }

            String responseBody = response.body() == null ? "" : response.body().string();
            JsonObject result = gson.fromJson(responseBody, JsonObject.class);
            String idField = result != null && result.has("sandboxID") ? "sandboxID" : "sandbox_id";
            if (result == null || !result.has(idField) || result.get(idField).isJsonNull()) {
                throw new SandboxException("Create sandbox response is missing sandbox_id: " + responseBody);
            }
            // JS/Python kill the sandbox and raise a template error when the
            // template predates the current SDK contract.
            String envdVersion = result.has("envdVersion") && !result.get("envdVersion").isJsonNull()
                    ? result.get("envdVersion").getAsString() : "0";
            if (compareVersions(envdVersion, "0.1.0") < 0) {
                String staleId = result.get(idField).getAsString();
                try {
                    killSandbox(staleId);
                } catch (SandboxException ignored) {
                    // The sandbox is being discarded either way.
                }
                throw new TemplateException(
                        "You need to update the template to use the new SDK. "
                                + "You can do this by running `novita-sandbox-cli template build` in the directory with the template.");
            }
            SandboxInfo info = gson.fromJson(result, SandboxInfo.class);
            info.setId(result.get(idField).getAsString());
            return info;
        } catch (IOException e) {
            throw AsyncSupport.transportFailure("Failed to create sandbox", e);
        }
    }

    /**
     * Whether this connection targets a legacy cluster. JS/Python derive this
     * from the configured domain ({@code config.isLegacyDomain}); an explicit
     * base-URL override never flips legacy behavior. Clients constructed
     * without a configured domain fall back to parsing the base-URL host.
     */
    boolean isLegacyApiDomain() {
        if (configuredDomain != null) return SandboxOpts.isLegacyDomain(configuredDomain);
        try {
            String host = java.net.URI.create(baseUrl).getHost();
            if (host == null) return false;
            if (host.startsWith("api.")) host = host.substring("api.".length());
            return SandboxOpts.isLegacyDomain(host);
        } catch (RuntimeException e) {
            return false;
        }
    }


    /** The configured envd domain this client was built with, or null. */
    String configuredDomain() { return configuredDomain; }

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

    private static int numericVersionPart(String part) {
        try {
            return Integer.parseInt(part.trim());
        } catch (NumberFormatException e) {
            return 0;
        }
    }

    SandboxInfo connectSandbox(String sandboxId, SandboxOpts opts) throws SandboxException {
        java.util.Map<String,Object> body = new java.util.HashMap<>();
        if (opts != null && opts.getTimeout() > 0) body.put("timeout", opts.getTimeout());
        if (opts != null) {
            if (opts.getSecure() != null) body.put("secure", opts.getSecure());
            if (opts.getAllowPublicTraffic() != null) {
                body.put("allowPublicTraffic", opts.getAllowPublicTraffic());
            } else if (opts.getNetwork() != null) {
                body.put("network", opts.getNetwork());
            }
        }
        return request("POST", "/sandboxes/" + segment(sandboxId) + "/connect", body, SandboxInfo.class);
    }

    /**
     * Kills the sandbox, matching JS/Python: 404 means the sandbox was already
     * gone and reports {@code false} instead of failing.
     */
    boolean killSandbox(String sandboxId) throws SandboxException {
        Request request = new Request.Builder()
                .url(baseUrl + "/sandboxes/" + segment(sandboxId))
                .headers(headers)
                .delete()
                .build();

        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (response.code() == 404) return false;
            if (!response.isSuccessful()) {
                throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            }
            return true;
        } catch (IOException e) {
            throw AsyncSupport.transportFailure("Failed to kill sandbox", e);
        }
    }

    /**
     * Pauses the sandbox, matching JS/Python: modern clusters send no request
     * body; legacy clusters send {@code {"sync": false}}; 409 (already
     * paused) reports {@code false}.
     */
    boolean pauseSandbox(String sandboxId) throws SandboxException {
        // JS modern pause POSTs without a body; only the legacy branch sends
        // JSON (sandboxApi.ts:1230-1290).
        RequestBody body = isLegacyApiDomain()
                ? RequestBody.create("{\"sync\":false}", JSON)
                : RequestBody.create(new byte[0], null);
        Request request = new Request.Builder().url(baseUrl + "/sandboxes/" + segment(sandboxId) + "/pause")
                .headers(headers).post(body).build();
        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (response.code() == 409) return false;
            if (!response.isSuccessful()) throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            return true;
        } catch (IOException e) { throw AsyncSupport.transportFailure("Failed to pause sandbox", e); }
    }

    void postSandbox(String sandboxId, String action, JsonObject payload) throws SandboxException {
        RequestBody body = RequestBody.create(gson.toJson(payload == null ? new JsonObject() : payload), JSON);
        Request request = new Request.Builder().url(baseUrl + "/sandboxes/" + segment(sandboxId) + "/" + action)
                .headers(headers).post(body).build();
        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (!response.isSuccessful()) throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
        } catch (IOException e) { throw AsyncSupport.transportFailure("Failed to " + action + " sandbox", e); }
    }

    SandboxInfo getSandboxInfo(String sandboxId) throws SandboxException {
        Request request = new Request.Builder()
                .url(baseUrl + "/sandboxes/" + segment(sandboxId))
                .headers(headers)
                .get()
                .build();

        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (!response.isSuccessful()) {
                throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            }

            String responseBody = response.body() == null ? "" : response.body().string();
            return gson.fromJson(responseBody, SandboxInfo.class);
        } catch (IOException e) {
            throw AsyncSupport.transportFailure("Failed to get sandbox info", e);
        }
    }

    void setTimeout(String sandboxId, int seconds) throws SandboxException {
        JsonObject p = new JsonObject(); p.addProperty("timeout", seconds); postSandbox(sandboxId, "timeout", p);
    }

    /**
     * Reset body per the JS/Python SDKs and the OpenAPI ResetSandbox schema:
     * {@code {resume, secure?, allowPublicTraffic?|network?, timeout?}}. The
     * 2xx response may carry a new {@code trafficAccessToken}; JS/Python
     * derive {@code success} from the status alone, so this returns null for
     * 409 (success=false) and the parsed body for any 2xx.
     */
    JsonObject resetSandbox(String sandboxId, ResetOptions options) throws SandboxException {
        JsonObject p = new JsonObject();
        p.addProperty("resume", options == null || options.getResume() == null ? true : options.getResume());
        if (options != null) {
            if (options.getSecure() != null) p.addProperty("secure", options.getSecure());
            if (options.getAllowPublicTraffic() != null) {
                p.addProperty("allowPublicTraffic", options.getAllowPublicTraffic());
            } else if (options.getNetwork() != null) {
                p.add("network", gson.toJsonTree(options.getNetwork()));
            }
            if (options.getTimeoutSeconds() != null) p.addProperty("timeout", options.getTimeoutSeconds());
        }
        RequestBody body = RequestBody.create(gson.toJson(p), JSON);
        Request request = new Request.Builder().url(baseUrl + "/sandboxes/" + segment(sandboxId) + "/reset")
                .headers(headers).post(body).build();
        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            // 409 means the sandbox could not be reset right now; JS/Python
            // surface it as success=false instead of an error. Null encodes
            // that here: any 2xx (even an empty body) is a success.
            if (response.code() == 409) return null;
            if (!response.isSuccessful()) throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            String responseBody = response.body() == null ? "" : response.body().string();
            if (responseBody.trim().isEmpty()) return new JsonObject();
            try {
                // A literal JSON "null" body parses to null; it is still a 2xx
                // success, so it must not fall through to the 409 sentinel.
                JsonObject parsed = gson.fromJson(responseBody, JsonObject.class);
                return parsed == null ? new JsonObject() : parsed;
            } catch (RuntimeException e) {
                // Legacy reset responses can carry a non-JSON body; JS/Python
                // still report success for any 2xx.
                return new JsonObject();
            }
        } catch (IOException e) { throw AsyncSupport.transportFailure("Failed to reset sandbox", e); }
    }
    /**
     * Updates sandbox network egress rules, matching JS/Python
     * {@code setNetwork}: PUT /sandboxes/{id}/network with only the
     * explicitly provided {@code allowOut}/{@code denyOut} lists.
     */
    void setNetwork(String sandboxId, NetworkOptions options) throws SandboxException {
        JsonObject body = new JsonObject();
        if (options != null) {
            if (options.getAllowOut() != null) body.add("allowOut", gson.toJsonTree(options.getAllowOut()));
            if (options.getDenyOut() != null) body.add("denyOut", gson.toJsonTree(options.getDenyOut()));
        }
        request("PUT", "/sandboxes/" + segment(sandboxId) + "/network", body, null);
    }

    /** Mounts a volume by name: POST /sandboxes/{id}/volume-mounts {name, path}. */
    SandboxInfo mountVolume(String sandboxId, String name, String path) throws SandboxException {
        java.util.Map<String,Object> body = new java.util.HashMap<>();
        body.put("name", name);
        body.put("path", path);
        return request("POST", "/sandboxes/" + segment(sandboxId) + "/volume-mounts", body, SandboxInfo.class);
    }

    /** Unmounts a volume: DELETE /sandboxes/{id}/volume-mounts?path=...&force=true. */
    SandboxInfo unmountVolume(String sandboxId, String path, boolean force) throws SandboxException {
        java.util.Map<String,String> query = new java.util.HashMap<>();
        query.put("path", path);
        if (force) query.put("force", "true");
        return request("DELETE", "/sandboxes/" + segment(sandboxId) + "/volume-mounts", query, null, SandboxInfo.class);
    }

    /**
     * Clone request body per JS/Python: {@code {count, strict, nodeID?, timeout?}}.
     * Returns the raw response so the caller can construct Sandbox instances.
     */
    JsonObject cloneSandbox(String sandboxId, CloneOptions options) throws SandboxException {
        java.util.Map<String,Object> body = new java.util.HashMap<>();
        int count = options == null ? 1 : options.getCount();
        boolean strict = options != null && options.isStrict();
        body.put("count", count);
        body.put("strict", strict);
        if (options != null) {
            if (options.getNodeId() != null && !options.getNodeId().isEmpty()) body.put("nodeID", options.getNodeId());
            if (options.getTimeoutSeconds() != null) body.put("timeout", options.getTimeoutSeconds());
        }
        return request("POST", "/sandboxes/" + segment(sandboxId) + "/clone", body, JsonObject.class);
    }

    /** Legacy-only commit: POST /sandboxes/{id}/commit {alias?}. */
    TemplateInfo commitSandbox(String sandboxId, String alias) throws SandboxException {
        java.util.Map<String,Object> body = new java.util.HashMap<>();
        if (alias != null && !alias.isEmpty()) body.put("alias", alias);
        TemplateInfo info = request("POST", "/sandboxes/" + segment(sandboxId) + "/commit", body, TemplateInfo.class);
        // JS: toTemplateInfo(data, 'snapshot_template').
        if (info != null) info.defaultTemplateType("snapshot_template");
        return info;
    }

    /** Event log query: GET /sandboxes/events with the JS/Python query parameters. */
    SandboxEventsResult getEvents(Long startTime, Long endTime, String sandboxId, String templateId,
                                  String events, String state, Boolean orderAsc, Integer offset, Integer limit) throws SandboxException {
        // LinkedHashMap keeps the JS/Python parameter order for stable URLs.
        java.util.Map<String,String> query = new java.util.LinkedHashMap<>();
        if (startTime != null) query.put("startTime", String.valueOf(startTime));
        if (endTime != null) query.put("endTime", String.valueOf(endTime));
        if (sandboxId != null && !sandboxId.isEmpty()) query.put("sandboxID", sandboxId);
        if (templateId != null && !templateId.isEmpty()) query.put("templateID", templateId);
        if (events != null && !events.isEmpty()) query.put("events", events);
        if (state != null && !state.isEmpty()) query.put("state", state);
        if (orderAsc != null) query.put("orderAsc", String.valueOf(orderAsc));
        if (offset != null) query.put("offset", String.valueOf(offset));
        if (limit != null) query.put("limit", String.valueOf(limit));
        return request("GET", "/sandboxes/events", query, null, SandboxEventsResult.class);
    }

    SandboxInfo[] listSandboxes() throws SandboxException {
        return listAllSandboxes(null);
    }

    SandboxInfo[] listAllSandboxes(SandboxListOptions options) throws SandboxException {
        SandboxListOptions query = new SandboxListOptions();
        if (options != null) {
            query.setQueryMetadata(options.getQueryMetadata()).setStates(options.getStates())
                    .setLimit(options.getLimit()).setNextToken(options.getNextToken());
        }
        java.util.List<SandboxInfo> items = new java.util.ArrayList<>();
        java.util.Set<String> visited = new java.util.HashSet<>();
        if (query.getNextToken() != null) visited.add(query.getNextToken());
        do {
            SandboxListResult page = listSandboxesPage(query);
            java.util.Collections.addAll(items, page.getItems());
            if (!page.isHasMore()) return items.toArray(new SandboxInfo[0]);
            if (!visited.add(page.getNextToken())) throw new SandboxException("Sandbox list repeated a pagination token");
            query.setNextToken(page.getNextToken());
        } while (true);
    }

    /**
     * Lists sandboxes with the JS/Python filters: GET /v2/sandboxes with
     * {@code metadata}, {@code state}, {@code limit} and {@code nextToken},
     * paginated through the {@code x-next-token} response header.
     */
    SandboxListResult listSandboxesPage(SandboxListOptions options) throws SandboxException {
        java.util.Map<String,String> query = new java.util.LinkedHashMap<>();
        if (options != null) {
            if (options.getQueryMetadata() != null && !options.getQueryMetadata().isEmpty()) {
                // JS/Python encode each pair, join them, then let the final
                // query serialization encode the joined value again.
                StringBuilder joined = new StringBuilder();
                for (Map.Entry<String,String> entry : options.getQueryMetadata().entrySet()) {
                    if (joined.length() > 0) joined.append('&');
                    joined.append(segment(entry.getKey() == null ? "" : entry.getKey()))
                          .append('=')
                          .append(segment(entry.getValue() == null ? "" : entry.getValue()));
                }
                query.put("metadata", joined.toString());
            }
            if (options.getStates() != null && !options.getStates().isEmpty()) {
                query.put("state", String.join(",", options.getStates()));
            }
            if (options.getLimit() != null) query.put("limit", String.valueOf(options.getLimit()));
            if (options.getNextToken() != null && !options.getNextToken().isEmpty()) query.put("nextToken", options.getNextToken());
        }
        Request request = new Request.Builder().url(withQuery(baseUrl, "/v2/sandboxes", query)).headers(headers).get().build();
        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (!response.isSuccessful()) throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            String body = response.body() == null ? "" : response.body().string();
            SandboxInfo[] items = gson.fromJson(body, SandboxInfo[].class);
            if (items == null) items = new SandboxInfo[0];
            // JS/Python append the client ID only when listing a legacy
            // cluster; modern clusters use sandboxID as-is.
            if (isLegacyApiDomain()) appendLegacyClientIds(items);
            SandboxListResult result = new SandboxListResult();
            result.setItems(items);
            result.setNextToken(response.header("x-next-token"));
            return result;
        } catch (IOException e) { throw AsyncSupport.transportFailure("Failed to list sandboxes", e); }
    }

    /** Appends the legacy client ID to each listed sandbox ID, matching JS/Python. */
    static void appendLegacyClientIds(SandboxInfo[] items) {
        for (SandboxInfo item : items) {
            if (item.getClientID() != null && !item.getClientID().isEmpty()) {
                item.setId(item.getId() + "-" + item.getClientID());
            }
        }
    }

    /**
     * Lists snapshots, matching JS/Python: GET /snapshots with the optional
     * {@code sandboxID}, {@code limit} and {@code nextToken} filters, paginated
     * through the {@code x-next-token} response header.
     */
    SnapshotListResult listSnapshotsPage(SnapshotListOptions options) throws SandboxException {
        java.util.Map<String,String> query = new java.util.LinkedHashMap<>();
        if (options != null) {
            if (options.getSandboxID() != null && !options.getSandboxID().isEmpty()) query.put("sandboxID", options.getSandboxID());
            if (options.getLimit() != null) query.put("limit", String.valueOf(options.getLimit()));
            if (options.getNextToken() != null && !options.getNextToken().isEmpty()) query.put("nextToken", options.getNextToken());
        }
        Request request = new Request.Builder().url(withQuery(baseUrl, "/snapshots", query)).headers(headers).get().build();
        try (Response response = AsyncSupport.execute(httpClient.newCall(request))) {
            if (!response.isSuccessful()) throw ApiExceptions.from(response.code(), response.body() == null ? response.message() : response.body().string());
            String body = response.body() == null ? "" : response.body().string();
            SnapshotInfo[] items = body.trim().isEmpty() ? new SnapshotInfo[0] : gson.fromJson(body, SnapshotInfo[].class);
            if (items == null) items = new SnapshotInfo[0];
            SnapshotListResult result = new SnapshotListResult();
            result.setItems(items);
            result.setNextToken(response.header("x-next-token"));
            return result;
        } catch (IOException e) { throw AsyncSupport.transportFailure("Failed to list snapshots", e); }
    }

    /** Releases the connection pool and dispatcher threads held by this client. */
    @Override public void close() {
        httpClient.dispatcher().cancelAll();
        httpClient.dispatcher().executorService().shutdown();
        httpClient.connectionPool().evictAll();
    }
}
