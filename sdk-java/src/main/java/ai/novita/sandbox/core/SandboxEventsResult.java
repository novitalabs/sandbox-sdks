package ai.novita.sandbox.core;

/** Event query result, matching the JS/Python {@code SandboxEventsResult}. */
public class SandboxEventsResult {
    private SandboxEventItem[] items = new SandboxEventItem[0];
    private long total;
    private boolean hasMore;

    public SandboxEventItem[] getItems() { return items; }
    public void setItems(SandboxEventItem[] items) { this.items = items == null ? new SandboxEventItem[0] : items; }
    public long getTotal() { return total; }
    public void setTotal(long total) { this.total = total; }
    public boolean isHasMore() { return hasMore; }
    public void setHasMore(boolean hasMore) { this.hasMore = hasMore; }
}
