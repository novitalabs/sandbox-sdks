package ai.novita.sandbox.core;

/**
 * Options for {@code Sandbox.reset}, matching the JS/Python reset options.
 *
 * <p>The reset endpoint boots a fresh VM from the latest snapshot's rootfs.
 * {@code resume} defaults to {@code true}, keeping the sandbox running after
 * the reset; passing {@code false} pauses it instead.
 */
public class ResetOptions {
    private Boolean resume;
    private Boolean secure;
    private NetworkOptions network;
    /** Sandbox timeout in seconds after the reset. */
    private Integer timeoutSeconds;
    private Boolean allowPublicTraffic;

    public Boolean getResume() { return resume; }
    public ResetOptions setResume(Boolean resume) { this.resume = resume; return this; }
    public Boolean getSecure() { return secure; }
    public ResetOptions setSecure(Boolean secure) { this.secure = secure; return this; }
    public NetworkOptions getNetwork() { return network; }
    public ResetOptions setNetwork(NetworkOptions network) { this.network = network; return this; }
    public Integer getTimeoutSeconds() { return timeoutSeconds; }
    public ResetOptions setTimeoutSeconds(Integer timeoutSeconds) { this.timeoutSeconds = timeoutSeconds; return this; }
    public Boolean getAllowPublicTraffic() { return allowPublicTraffic; }
    public ResetOptions setAllowPublicTraffic(Boolean allowPublicTraffic) { this.allowPublicTraffic = allowPublicTraffic; return this; }
}
