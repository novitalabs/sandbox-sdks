package ai.novita.sandbox.core;

public final class CancelTooLateException extends MigrationConflictException {
    public CancelTooLateException(String message) { super(message,"MIGRATION_PAST_CANCEL_LINE"); }
}
