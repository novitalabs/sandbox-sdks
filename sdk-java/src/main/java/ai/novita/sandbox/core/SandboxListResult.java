package ai.novita.sandbox.core;

/**
 * One page of the sandbox list plus pagination state, matching the JS/Python
 * paginator surface ({@code hasNext} / {@code nextToken}).
 */
public class SandboxListResult {
    private SandboxInfo[] items = new SandboxInfo[0];
    private String nextToken;

    public SandboxInfo[] getItems() { return items; }
    public void setItems(SandboxInfo[] items) { this.items = items == null ? new SandboxInfo[0] : items; }
    /** Token for the next page; null when this is the last page. */
    public String getNextToken() { return nextToken; }
    public void setNextToken(String nextToken) { this.nextToken = nextToken; }
    public boolean isHasMore() { return nextToken != null && !nextToken.isEmpty(); }
}
