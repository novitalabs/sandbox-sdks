package ai.novita.sandbox.core;

import java.util.List;
import java.util.Map;

/**
 * Filters and pagination for {@code Sandbox.list}, matching JS/Python
 * {@code SandboxListOpts} (which also carries connection options).
 */
public class SandboxListOptions extends SandboxOpts {
    /**
     * Filter by metadata, matching {@code query.metadata} in JS/Python;
     * multiple entries are combined with AND.
     */
    private Map<String, String> queryMetadata;
    /** Filter by sandbox state, e.g. {@code ["running", "paused"]}. */
    private List<String> states;
    /** Number of sandboxes per page (server default 100). */
    private Integer limit;
    /** Token of the next page, from {@link SandboxListResult#getNextToken()}. */
    private String nextToken;

    public Map<String, String> getQueryMetadata() { return queryMetadata; }
    public SandboxListOptions setQueryMetadata(Map<String, String> queryMetadata) { this.queryMetadata = queryMetadata; return this; }
    public List<String> getStates() { return states; }
    public SandboxListOptions setStates(List<String> states) { this.states = states; return this; }
    public Integer getLimit() { return limit; }
    public SandboxListOptions setLimit(Integer limit) { this.limit = limit; return this; }
    public String getNextToken() { return nextToken; }
    public SandboxListOptions setNextToken(String nextToken) { this.nextToken = nextToken; return this; }
}
