package ai.novita.sandbox.core;

import com.google.gson.annotations.SerializedName;
import java.time.Instant;

/** A tag attached to a template build. */
public final class TemplateTag {
    private String tag;
    @SerializedName(value = "buildID", alternate = {"build_id"}) private String buildId;
    @SerializedName(value = "createdAt", alternate = {"created_at"}) private String createdAt;
    public String getTag() { return tag; }
    public String getBuildId() { return buildId; }
    public String getCreatedAt() { return createdAt; }
    public Instant getCreatedAtInstant() { try { return createdAt == null ? null : Instant.parse(createdAt); } catch (RuntimeException e) { return null; } }
}
