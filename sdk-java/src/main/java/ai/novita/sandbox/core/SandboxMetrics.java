package ai.novita.sandbox.core;
/**
 * Sandbox metrics, matching the JS/Python model: memory and disk are bytes,
 * and legacy responses carry MiB values that are normalized on read.
 */
public class SandboxMetrics {
    private String timestamp;
    @com.google.gson.annotations.SerializedName(value = "cpuUsedPct", alternate = {"cpu_used_pct"})
    private Double cpuUsedPct;
    private Integer cpuCount;
    @com.google.gson.annotations.SerializedName(value = "memUsed", alternate = {"memUsedMiB", "mem_used_mib"})
    private Long memUsed;
    @com.google.gson.annotations.SerializedName(value = "memTotal", alternate = {"memTotalMiB", "mem_total_mib"})
    private Long memTotal;
    private Long diskUsed;
    private Long diskTotal;
    /** True after legacy MiB values have been converted to bytes. */
    private transient boolean legacyNormalized;

    public String getTimestamp() { return timestamp; }
    public Double getCpuUsedPct() { return cpuUsedPct; }
    public Integer getCpuCount() { return cpuCount; }
    public Long getMemUsed() { return memUsed; }
    public Long getMemTotal() { return memTotal; }
    public Long getDiskUsed() { return diskUsed; }
    public Long getDiskTotal() { return diskTotal; }

    /**
     * Converts legacy MiB memory values to bytes and reports unknown disk
     * usage as {@code -1}, exactly like the JS/Python legacy metrics branch.
     */
    void applyLegacyNormalization() {
        if (legacyNormalized) return;
        legacyNormalized = true;
        if (memUsed != null) memUsed = memUsed * 1024 * 1024;
        if (memTotal != null) memTotal = memTotal * 1024 * 1024;
        diskUsed = -1L;
        diskTotal = -1L;
    }
}
