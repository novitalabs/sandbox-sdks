package ai.novita.sandbox.core;

/**
 * Filters and pagination for {@code Sandbox.listSnapshots}, matching the JS/Python
 * {@code SnapshotListOpts} (which also carry connection options).
 */
public class SnapshotListOptions extends SandboxOpts {
    /** Optional source-sandbox filter; omit to list team-wide snapshots. */
    private String sandboxID;
    /** Number of snapshots per page. */
    private Integer limit;
    /** Token of the next page, from {@link SnapshotListResult#getNextToken()}. */
    private String nextToken;

    public String getSandboxID() { return sandboxID; }
    public SnapshotListOptions setSandboxID(String sandboxID) { this.sandboxID = sandboxID; return this; }
    public Integer getLimit() { return limit; }
    public SnapshotListOptions setLimit(Integer limit) { this.limit = limit; return this; }
    public String getNextToken() { return nextToken; }
    public SnapshotListOptions setNextToken(String nextToken) { this.nextToken = nextToken; return this; }
}
