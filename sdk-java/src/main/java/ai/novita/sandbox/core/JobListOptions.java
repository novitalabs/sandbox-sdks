package ai.novita.sandbox.core;

/** Optional migration list filters. */
public final class JobListOptions {
    private String resourceId = null;
    private String type = null;
    private String status = null;
    private String sourceRegion = null;
    private String targetRegion = null;
    private String cursor = null;
    private Integer limit = null;
    public String getResourceId() { return resourceId; }
    public JobListOptions setResourceId(String value) { resourceId=value; return this; }
    public String getType() { return type; }
    public JobListOptions setType(String value) { type=value; return this; }
    public String getStatus() { return status; }
    public JobListOptions setStatus(String value) { status=value; return this; }
    public String getSourceRegion() { return sourceRegion; }
    public JobListOptions setSourceRegion(String value) { sourceRegion=value; return this; }
    public String getTargetRegion() { return targetRegion; }
    public JobListOptions setTargetRegion(String value) { targetRegion=value; return this; }
    public String getCursor() { return cursor; }
    public JobListOptions setCursor(String value) { cursor=value; return this; }
    public Integer getLimit() { return limit; }
    public JobListOptions setLimit(Integer value) { limit=value; return this; }
}
