package ai.novita.sandbox.core;

/** Wire constants; status models keep unknown strings unchanged. */
public final class JobStatus {
    private JobStatus() { }
    public static final String MIGRATING="migrating", COMPLETED="completed", REVERTING="reverting", CANCELLED="cancelled", FAILED="failed";
    public static boolean isTerminal(String value) { return COMPLETED.equals(value)||CANCELLED.equals(value)||FAILED.equals(value); }
}
