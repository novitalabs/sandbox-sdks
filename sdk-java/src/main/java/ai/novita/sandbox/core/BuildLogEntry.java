package ai.novita.sandbox.core;

import java.time.Instant;

/** One build event, including warnings emitted while resolving image config. */
public class BuildLogEntry {
    private String timestamp, level, message;
    public BuildLogEntry() { }
    public BuildLogEntry(String level, String message) {
        this.timestamp = Instant.now().toString();
        this.level = level;
        this.message = message;
    }
    public String getTimestamp() { return timestamp; }
    public String getLevel() { return level; }
    public String getMessage() { return message; }
    public Instant getTime() {
        try { return timestamp == null ? null : Instant.parse(timestamp); }
        catch (java.time.format.DateTimeParseException e) { return null; }
    }
}
