package ai.novita.sandbox.core;

public final class SharedDependencyException extends MigrationConflictException {
    public SharedDependencyException(String message) { super(message,"MIGRATION_SHARED_DEPENDENCY"); }
}
