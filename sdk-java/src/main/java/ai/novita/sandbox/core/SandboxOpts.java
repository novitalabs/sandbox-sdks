package ai.novita.sandbox.core;

import java.util.Map;

/**
 * Options for creating or connecting to a sandbox.
 *
 * <p>Field names and wire serialization follow the JS and Python SDKs and the
 * OpenAPI {@code NewSandbox} schema. Optional fields are sent only when set,
 * so server-side defaults apply exactly as they do for the other SDKs.
 */
public class SandboxOpts {
    private String apiKey;
    private String baseUrl;
    private String domain;
    private Map<String, String> metadata;
    private Map<String, String> envVars;
    private int timeout = 300; // 5 minutes default
    private Integer requestTimeoutSeconds;
    private String accessToken, sandboxUrl;
    private Boolean debug;
    private java.util.function.Consumer<String> logger;
    private Map<String,String> headers = new java.util.LinkedHashMap<>();
    private Boolean autoPause;
    private Boolean autoResumeEnabled;
    private Boolean secure;
    private Boolean allowInternetAccess;
    private NetworkOptions network;
    /** Whether sandbox URLs are publicly accessible without a traffic access token (connect/reset). */
    private Boolean allowPublicTraffic;
    /** Maps a sandbox environment variable name to a stored secret name. */
    private Map<String, String> secretEnvs;
    /** Maps a mount path inside the sandbox to a volume name. */
    private Map<String, String> volumeMounts;
    /** Raw MCP server configuration, serialized as the {@code mcp} field. */
    private Map<String, Object> mcp;
    private String nodeId;

    /** Receives method/path request diagnostics. Headers, queries and bodies are omitted. */
    public java.util.function.Consumer<String> getLogger(){return logger;}
    public void setLogger(java.util.function.Consumer<String> value){logger=value;}
    public String getAccessToken() { return accessToken != null ? accessToken : System.getenv("NOVITA_ACCESS_TOKEN"); }
    public void setAccessToken(String value) { accessToken = value; }
    public String getSandboxUrl() { return sandboxUrl != null ? sandboxUrl : System.getenv("NOVITA_SANDBOX_URL"); }
    public void setSandboxUrl(String value) { sandboxUrl = value; }
    public boolean isDebug() { return debug != null ? debug : Boolean.parseBoolean(System.getenv("NOVITA_DEBUG")); }
    public void setDebug(boolean value) { debug = value; }
    public Map<String,String> getHeaders() { return java.util.Collections.unmodifiableMap(headers); }
    public void setHeaders(Map<String,String> value) { headers = value == null ? new java.util.LinkedHashMap<>() : new java.util.LinkedHashMap<>(value); }
    static boolean isLegacyDomain(String domain) {
        String override = System.getenv().getOrDefault("NOVITA_IS_LEGACY", "").trim();
        if (override.equalsIgnoreCase("true")) return true;
        if (override.equalsIgnoreCase("false")) return false;
        if (domain == null) return false;
        String normalized = domain.toLowerCase(java.util.Locale.ROOT);
        return normalized.equals("sandbox.novita.ai") || normalized.equals("us-ga-1.sandbox.novita.ai");
    }
    /** Snapshot of connection and creation options, used without mutating the caller. */
    public SandboxOpts copy() {
        SandboxOpts c = new SandboxOpts();
        c.apiKey=apiKey; c.baseUrl=baseUrl; c.domain=domain; c.accessToken=accessToken; c.sandboxUrl=sandboxUrl; c.debug=debug; c.logger=logger;
        c.headers=new java.util.LinkedHashMap<>(headers); c.timeout=timeout; c.requestTimeoutSeconds=requestTimeoutSeconds;
        c.metadata=metadata == null ? null : new java.util.LinkedHashMap<>(metadata);
        c.envVars=envVars == null ? null : new java.util.LinkedHashMap<>(envVars);
        c.autoPause=autoPause; c.autoResumeEnabled=autoResumeEnabled; c.secure=secure; c.allowInternetAccess=allowInternetAccess;
        c.network=network==null?null:new com.google.gson.Gson().fromJson(new com.google.gson.Gson().toJson(network),NetworkOptions.class);
        c.allowPublicTraffic=allowPublicTraffic;
        c.secretEnvs=secretEnvs==null?null:new java.util.LinkedHashMap<>(secretEnvs);
        c.volumeMounts=volumeMounts==null?null:new java.util.LinkedHashMap<>(volumeMounts);
        c.mcp=mcp==null?null:new com.google.gson.Gson().fromJson(new com.google.gson.Gson().toJson(mcp),new com.google.gson.reflect.TypeToken<Map<String,Object>>(){}.getType());
        c.nodeId=nodeId;
        return c;
    }
    public SandboxOpts() {
    }

    public String getApiKey() {
        return apiKey;
    }

    public void setApiKey(String apiKey) {
        this.apiKey = apiKey;
    }

    public String getBaseUrl() {
        return baseUrl;
    }

    public void setBaseUrl(String baseUrl) {
        this.baseUrl = baseUrl;
    }
    String resolvedBaseUrl() {
        String value = baseUrl;
        if (value == null || value.trim().isEmpty()) value = System.getenv("NOVITA_API_URL");
        if (value == null || value.trim().isEmpty()) value = isDebug() ? "http://localhost:3000" : "https://api." + resolvedDomain();
        value = value.trim();
        return value.contains("://") ? value : "https://" + value;
    }
    /**
     * The configured envd domain: explicit value, then {@code NOVITA_DOMAIN},
     * then the modern default. JS/Python derive legacy behavior from exactly
     * this chain ({@code config.domain}), never from the API URL.
     */
    String resolvedDomain() {
        if (domain != null && !domain.trim().isEmpty()) return domain;
        String d = System.getenv("NOVITA_DOMAIN");
        if (d == null || d.trim().isEmpty()) d = "us-phx-1.sandbox.novita.ai";
        return d;
    }
    public String getDomain() { return domain; }
    public void setDomain(String domain) { this.domain = domain; }

    public Map<String, String> getMetadata() {
        return metadata;
    }

    public void setMetadata(Map<String, String> metadata) {
        this.metadata = metadata;
    }

    public Map<String, String> getEnvVars() {
        return envVars;
    }

    public void setEnvVars(Map<String, String> envVars) {
        this.envVars = envVars;
    }

    /**
     * Sandbox lifetime in seconds, sent to the API on create.
     */
    public int getTimeout() {
        return timeout;
    }

    public void setTimeout(int timeout) {
        this.timeout = timeout;
    }

    /**
     * Per-request HTTP timeout in seconds for control-plane calls.
     */
    public int getRequestTimeoutSeconds() {
        return requestTimeoutSeconds != null ? requestTimeoutSeconds : (isLegacyDomain(resolvedDomain()) ? 1800 : 60);
    }

    public void setRequestTimeoutSeconds(int requestTimeoutSeconds) {
        if (requestTimeoutSeconds < 0) throw new IllegalArgumentException("requestTimeoutSeconds must be non-negative");
        this.requestTimeoutSeconds = requestTimeoutSeconds;
    }

    /** Whether the sandbox automatically pauses after the timeout ({@code autoPause}). */
    public Boolean getAutoPause() { return autoPause; }
    public void setAutoPause(Boolean autoPause) { this.autoPause = autoPause; }

    /** Enables {@code autoResume: {enabled: true}} on the create request. */
    public Boolean getAutoResumeEnabled() { return autoResumeEnabled; }
    public void setAutoResumeEnabled(Boolean autoResumeEnabled) { this.autoResumeEnabled = autoResumeEnabled; }

    /** Secures all system communication with the sandbox ({@code secure}). */
    public Boolean getSecure() { return secure; }
    public void setSecure(Boolean secure) { this.secure = secure; }

    /** Whether the sandbox may access the internet ({@code allow_internet_access}). */
    public Boolean getAllowInternetAccess() { return allowInternetAccess; }
    public void setAllowInternetAccess(Boolean allowInternetAccess) { this.allowInternetAccess = allowInternetAccess; }

    /** Network configuration; legacy creation supports only {@code allowPublicTraffic}. */
    public NetworkOptions getNetwork() { return network; }
    public void setNetwork(NetworkOptions network) { this.network = network; }

    /** Whether sandbox URLs are publicly accessible without a traffic token (connect/reset). */
    public Boolean getAllowPublicTraffic() { return allowPublicTraffic; }
    public void setAllowPublicTraffic(Boolean allowPublicTraffic) { this.allowPublicTraffic = allowPublicTraffic; }

    /**
     * Maps sandbox environment variable names to previously created secret
     * names ({@code secrets}); modern clusters only.
     */
    public Map<String, String> getSecretEnvs() { return secretEnvs; }
    public void setSecretEnvs(Map<String, String> secretEnvs) { this.secretEnvs = secretEnvs; }

    /**
     * Maps mount paths inside the sandbox to volume names
     * ({@code volumeMounts}); modern clusters only.
     */
    public Map<String, String> getVolumeMounts() { return volumeMounts; }
    public void setVolumeMounts(Map<String, String> volumeMounts) { this.volumeMounts = volumeMounts; }

    /** Raw MCP server configuration ({@code mcp}); modern clusters only. */
    public Map<String, Object> getMcp() { return mcp; }
    public void setMcp(Map<String, Object> mcp) { this.mcp = mcp; }

    /** Pins the sandbox to an orchestrator node ({@code nodeID}). */
    public String getNodeId() { return nodeId; }
    public void setNodeId(String nodeId) { this.nodeId = nodeId; }
}
