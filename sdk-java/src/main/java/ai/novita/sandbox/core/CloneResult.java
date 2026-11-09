package ai.novita.sandbox.core;

/** Clone result, matching the JS/Python {@code SandboxCloneResult}. */
public class CloneResult {
    private Sandbox[] sandboxes = new Sandbox[0];
    private int count;
    private String snapshotTemplateId;

    public Sandbox[] getSandboxes() { return sandboxes; }
    public void setSandboxes(Sandbox[] sandboxes) { this.sandboxes = sandboxes == null ? new Sandbox[0] : sandboxes; }
    public int getCount() { return count; }
    public void setCount(int count) { this.count = count; }
    public String getSnapshotTemplateId() { return snapshotTemplateId; }
    public void setSnapshotTemplateId(String snapshotTemplateId) { this.snapshotTemplateId = snapshotTemplateId; }
}
