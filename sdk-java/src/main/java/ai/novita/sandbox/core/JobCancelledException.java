package ai.novita.sandbox.core;

/** Terminal migration outcome, including individual item results. */
public final class JobCancelledException extends SandboxException {
    private final MigrationStatus status;
    public JobCancelledException(MigrationStatus status) { super("Migration job "+status.getJobId()+" cancelled: completed="+status.getCompleted()+", failed="+status.getFailed()+", cancelled="+status.getCancelled()+", total="+status.getTotal()); this.status=status; }
    public MigrationStatus getStatus() { return status; }
}
