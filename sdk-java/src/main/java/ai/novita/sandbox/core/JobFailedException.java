package ai.novita.sandbox.core;

/** Terminal migration outcome, including individual item results. */
public final class JobFailedException extends SandboxException {
    private final MigrationStatus status;
    public JobFailedException(MigrationStatus status) { super("Migration job "+status.getJobId()+" failed: completed="+status.getCompleted()+", failed="+status.getFailed()+", cancelled="+status.getCancelled()+", total="+status.getTotal()); this.status=status; }
    public MigrationStatus getStatus() { return status; }
}
