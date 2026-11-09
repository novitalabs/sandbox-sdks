package ai.novita.sandbox.core;

/**
 * One page of the snapshot list plus pagination state, matching the JS/Python
 * paginator surface ({@code hasNext} / {@code nextToken}).
 */
public class SnapshotListResult {
    private SnapshotInfo[] items = new SnapshotInfo[0];
    private String nextToken;

    public SnapshotInfo[] getItems() { return items; }
    public void setItems(SnapshotInfo[] items) { this.items = items == null ? new SnapshotInfo[0] : items; }
    /** Token for the next page; null when this is the last page. */
    public String getNextToken() { return nextToken; }
    public void setNextToken(String nextToken) { this.nextToken = nextToken; }
    public boolean isHasMore() { return nextToken != null && !nextToken.isEmpty(); }
}
