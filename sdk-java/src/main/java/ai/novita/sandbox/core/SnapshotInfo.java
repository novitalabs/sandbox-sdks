package ai.novita.sandbox.core;

import com.google.gson.annotations.SerializedName;

/** Metadata for a persistent sandbox snapshot. */
public final class SnapshotInfo {
    @SerializedName(value = "snapshotID", alternate = {"snapshot_id", "id"})
    private String snapshotId;
    @SerializedName(value = "sandboxID", alternate = {"sandbox_id"})
    private String sandboxId;
    private String name;
    private String status;

    public String getSnapshotId() { return snapshotId; }
    public String getSandboxId() { return sandboxId; }
    public String getName() { return name; }
    public String getStatus() { return status; }
}
