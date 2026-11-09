package ai.novita.sandbox.core;

import com.google.gson.annotations.SerializedName;
import java.util.Collections;
import java.util.List;

/** Failure details returned by the build status endpoint. */
public class BuildFailureReason {
    private String message;
    private String step;
    @SerializedName(value = "logEntries", alternate = {"log_entries"})
    private List<BuildLogEntry> logEntries;

    public String getMessage() { return message; }
    /** The failed instruction text, for example {@code RUN npm install}. */
    public String getStep() { return step; }
    public List<BuildLogEntry> getLogEntries() { return logEntries == null ? Collections.emptyList() : logEntries; }
}
