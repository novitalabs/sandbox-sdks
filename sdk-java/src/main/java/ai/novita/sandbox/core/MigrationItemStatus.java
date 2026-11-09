package ai.novita.sandbox.core;

/** Migration API response projection. */
public final class MigrationItemStatus {
    private String itemId = "";
    private String resourceType = "";
    private String resourceId = "";
    private String status = "";
    public String getItemId() { return itemId == null ? "" : itemId; }
    public String getResourceType() { return resourceType == null ? "" : resourceType; }
    public String getResourceId() { return resourceId == null ? "" : resourceId; }
    public String getStatus() { return status == null ? "" : status; }
}
