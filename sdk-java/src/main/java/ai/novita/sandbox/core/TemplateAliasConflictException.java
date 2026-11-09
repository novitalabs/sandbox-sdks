package ai.novita.sandbox.core;

public final class TemplateAliasConflictException extends MigrationConflictException {
    public TemplateAliasConflictException(String message) { super(message,"MIGRATION_TEMPLATE_ALIAS_CONFLICT"); }
}
