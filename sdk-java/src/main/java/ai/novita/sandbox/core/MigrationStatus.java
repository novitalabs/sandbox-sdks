package ai.novita.sandbox.core;

/** Migration API response projection. */
public final class MigrationStatus {
    private String jobId = "";
    private String status = "";
    private java.util.List<MigrationItemStatus> items = new java.util.ArrayList<>();
    private int completed = 0;
    private int failed = 0;
    private int cancelled = 0;
    private int total = 0;
    public String getJobId() { return jobId == null ? "" : jobId; }
    public String getStatus() { return status == null ? "" : status; }
    public java.util.List<MigrationItemStatus> getItems() { return items == null ? java.util.Collections.emptyList() : java.util.Collections.unmodifiableList(items); }
    public int getCompleted() { return completed; }
    public int getFailed() { return failed; }
    public int getCancelled() { return cancelled; }
    public int getTotal() { return total; }
}
