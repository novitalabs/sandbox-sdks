package ai.novita.sandbox.core;
import java.util.List;

/** Response of POST /templates/tags, matching JS/Python {@code TemplateTagInfo}. */
public class TemplateTagInfo {
    @com.google.gson.annotations.SerializedName(value = "buildID", alternate = {"build_id"})
    private String buildID;
    private List<String> tags;

    public String getBuildId() { return buildID; }
    public List<String> getTags() { return tags; }
}
