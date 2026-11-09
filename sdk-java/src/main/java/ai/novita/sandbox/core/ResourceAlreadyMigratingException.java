package ai.novita.sandbox.core;

public final class ResourceAlreadyMigratingException extends MigrationConflictException {
    public ResourceAlreadyMigratingException(String message) { super(message,"MIGRATION_RESOURCE_ALREADY_MIGRATING"); }
}
