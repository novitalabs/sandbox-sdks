package ai.novita.sandbox.core;

/** Migration API response projection. */
public final class JobPage {
    private java.util.List<MigrationStatus> items = new java.util.ArrayList<>();
    private String nextCursor = null;
    public java.util.List<MigrationStatus> getItems() { return items == null ? java.util.Collections.emptyList() : java.util.Collections.unmodifiableList(items); }
    public String getNextCursor() { return nextCursor == null || nextCursor.isEmpty() ? null : nextCursor; }
}
