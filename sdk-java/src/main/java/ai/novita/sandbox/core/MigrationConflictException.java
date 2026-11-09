package ai.novita.sandbox.core;

/** Base for migration conflicts; unknown platform reason codes are retained. */
public class MigrationConflictException extends SandboxException {
    private final String reason;
    public MigrationConflictException(String message, String reason) { super(message); this.reason=reason; }
    public String getReason() { return reason; }
}
