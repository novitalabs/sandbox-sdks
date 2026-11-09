package ai.novita.sandbox.core;

/**
 * Options for {@code Sandbox.clone}, matching the JS/Python clone options.
 */
public class CloneOptions {
    private int count = 1;
    private boolean strict;
    private String nodeId;
    /** Sandbox timeout in seconds for the cloned sandboxes. */
    private Integer timeoutSeconds;

    public int getCount() { return count; }
    public CloneOptions setCount(int count) { this.count = count; return this; }
    public boolean isStrict() { return strict; }
    public CloneOptions setStrict(boolean strict) { this.strict = strict; return this; }
    public String getNodeId() { return nodeId; }
    public CloneOptions setNodeId(String nodeId) { this.nodeId = nodeId; return this; }
    public Integer getTimeoutSeconds() { return timeoutSeconds; }
    public CloneOptions setTimeoutSeconds(Integer timeoutSeconds) { this.timeoutSeconds = timeoutSeconds; return this; }
}
