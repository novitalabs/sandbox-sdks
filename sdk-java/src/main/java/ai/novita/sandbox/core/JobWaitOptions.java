package ai.novita.sandbox.core;

/** Total wait budget includes HTTP requests, callbacks and polling delays. */
public final class JobWaitOptions {
    private java.time.Duration timeout = java.time.Duration.ofMinutes(30);
    private java.time.Duration pollInterval = java.time.Duration.ofSeconds(10);
    private java.util.function.Consumer<MigrationStatus> onProgress = null;
    public java.time.Duration getTimeout() { return timeout; }
    public JobWaitOptions setTimeout(java.time.Duration value) { timeout=value; return this; }
    public java.time.Duration getPollInterval() { return pollInterval; }
    public JobWaitOptions setPollInterval(java.time.Duration value) { pollInterval=value; return this; }
    public java.util.function.Consumer<MigrationStatus> getOnProgress() { return onProgress; }
    public JobWaitOptions setOnProgress(java.util.function.Consumer<MigrationStatus> value) { onProgress=value; return this; }
}
