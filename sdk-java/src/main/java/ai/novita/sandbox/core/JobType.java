package ai.novita.sandbox.core;

/** Migration list filter values. Whole-account jobs are operator-created. */
public final class JobType {
    private JobType() { }
    public static final String SANDBOX_BATCH="sandbox_batch", TEMPLATE="template", CUSTOMER="customer";
}
