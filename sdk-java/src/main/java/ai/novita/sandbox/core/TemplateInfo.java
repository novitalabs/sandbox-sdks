package ai.novita.sandbox.core;
import java.util.*;

/**
 * Template metadata from the sandbox control plane.
 * Template build fields are normalized from the control-plane response.
 */
public class TemplateInfo {
    @com.google.gson.annotations.SerializedName(value = "templateID", alternate = {"template_id"})
    private String templateID;
    @com.google.gson.annotations.SerializedName(value = "buildID", alternate = {"build_id"})
    private String buildID;
    private String name, alias, status;
    @com.google.gson.annotations.SerializedName(value = "buildStatus", alternate = {"build_status"})
    private String buildStatus;
    /** Raw builds array from GET /templates/{id}; the first entry is the latest build. */
    private List<TemplateBuildRef> builds;
    @com.google.gson.annotations.SerializedName(value = "aliases")
    private List<String> aliases;
    private List<String> names;
    @com.google.gson.annotations.SerializedName(value = "templateType", alternate = {"template_type"})
    private String templateType;
    private Integer buildCount;
    private Integer cpuCount;
    @com.google.gson.annotations.SerializedName(value = "memoryMB", alternate = {"memory_mb", "memoryMb"})
    private Integer memoryMB;
    @com.google.gson.annotations.SerializedName(value = "diskSizeMB", alternate = {"disk_size_mb", "diskSizeMb"})
    private Integer diskSizeMB;
    private Integer spawnCount;
    private String envdVersion;
    @com.google.gson.annotations.SerializedName(value = "public")
    private Boolean isPublic;
    private Map<String, String> metadata;
    @com.google.gson.annotations.SerializedName(value = "createdAt", alternate = {"created_at"})
    private String createdAt;
    @com.google.gson.annotations.SerializedName(value = "updatedAt", alternate = {"updated_at"})
    private String updatedAt;
    @com.google.gson.annotations.SerializedName(value = "lastSpawnedAt", alternate = {"last_spawned_at"})
    private String lastSpawnedAt;
    /** Object response {@code {id, email?}}, matching JS/Python; never a plain string. */
    private TemplateCreatedBy createdBy;
    /** Legacy convenience fields kept for existing callers. */
    private String image;
    private List<String> tags;

    /** Creator reference, matching JS {@code TemplateInfo['createdBy']}. */
    public static class TemplateCreatedBy {
        private String id;
        private String email;
        public String getId() { return id; }
        public String getEmail() { return email; }
    }

    /** Minimal build reference used to derive the latest build fields. */
    static class TemplateBuildRef {
        @com.google.gson.annotations.SerializedName(value = "buildID", alternate = {"build_id"})
        String buildID;
        String status;
    }

    /**
     * Derives {@code buildID}/{@code buildStatus} from the first entry of the
     * {@code builds} array, matching JS/Python {@code Template.get}:
     * {@code buildID: build?.buildID ?? ''} unconditionally overrides any
     * top-level value (Gson skips transient fields, so the field must not be
     * transient or the array would never deserialize).
     */
    void deriveLatestBuild() {
        TemplateBuildRef latest = builds != null && !builds.isEmpty() ? builds.get(0) : null;
        buildID = latest == null || latest.buildID == null ? "" : latest.buildID;
        buildStatus = latest == null ? null : latest.status;
    }

    /** Sets templateType when the response omits it, matching JS {@code toTemplateInfo(data, type)}. */
    void defaultTemplateType(String type) { if (templateType == null) templateType = type; }

    public String getTemplateId() { return templateID; }
    public String getBuildId() { return buildID; }
    public String getName() { return name; }
    public String getAlias() { return alias; }
    public String getStatus() { return status; }
    public String getTemplateType() { return templateType; }
    public String getImage() { return image; }
    public String getBuildStatus() { return buildStatus; }
    public List<String> getAliases() { return aliases; }
    public List<String> getNames() { return names; }
    public Integer getBuildCount() { return buildCount; }
    public Integer getCpuCount() { return cpuCount; }
    public Integer getMemoryMb() { return memoryMB; }
    public Integer getDiskSizeMb() { return diskSizeMB; }
    public Integer getSpawnCount() { return spawnCount; }
    public String getEnvdVersion() { return envdVersion; }
    public Boolean isPublic() { return isPublic; }
    public Map<String, String> getMetadata() { return metadata; }
    public String getCreatedAt() { return createdAt; }
    public String getUpdatedAt() { return updatedAt; }
    public String getLastSpawnedAt() { return lastSpawnedAt; }
    public TemplateCreatedBy getCreatedBy() { return createdBy; }
    public List<String> getTags() { return tags; }
}
