package ai.novita.sandbox.core;

import com.google.gson.JsonObject;

/**
 * Absolute resource totals for a sandbox, not increments.
 *
 * <p>CPU is a core count. Memory and disk are user-facing GiB values that the
 * SDK converts to the MiB values the API expects. A changed total must be an
 * exact number of MiB, which allows fractional GiB values that match a
 * template's size. Unset fields are left unchanged, so at least one field must
 * be set.
 */
public class SandboxResourceTargets {
    /** Total virtual CPUs. Must be positive. */
    private Integer cpu;
    /** Total memory in GiB. */
    private Double memoryGiB;
    /** Total disk size in GiB. */
    private Double diskGiB;

    public Integer getCpu() { return cpu; }
    public SandboxResourceTargets setCpu(Integer cpu) { this.cpu = cpu; return this; }
    public Double getMemoryGiB() { return memoryGiB; }
    public SandboxResourceTargets setMemoryGiB(Double memoryGiB) { this.memoryGiB = memoryGiB; return this; }
    /** Convenience overload so integer GiB totals need no boxing. */
    public SandboxResourceTargets setMemoryGiB(double memoryGiB) { this.memoryGiB = memoryGiB; return this; }
    public Double getDiskGiB() { return diskGiB; }
    public SandboxResourceTargets setDiskGiB(Double diskGiB) { this.diskGiB = diskGiB; return this; }
    /** Convenience overload so integer GiB totals need no boxing. */
    public SandboxResourceTargets setDiskGiB(double diskGiB) { this.diskGiB = diskGiB; return this; }

    SandboxResourceTargets copy() {
        SandboxResourceTargets clone = new SandboxResourceTargets();
        clone.cpu = cpu;
        clone.memoryGiB = memoryGiB;
        clone.diskGiB = diskGiB;
        return clone;
    }

    /**
     * Validates the public GiB totals and returns the MiB request body.
     *
     * @throws InvalidArgumentException when no target is set or a target is not
     *         a positive/exact value
     */
    JsonObject toApiBody() throws SandboxException {
        JsonObject body = new JsonObject();
        if (cpu != null) {
            if (cpu <= 0) {
                throw new InvalidArgumentException(
                        "resource target cpu must be a positive whole number");
            }
            body.addProperty("vcpu", cpu);
        }
        if (memoryGiB != null) {
            body.addProperty("memory_mb", gibToMib(memoryGiB, "memoryGiB"));
        }
        if (diskGiB != null) {
            body.addProperty("disk_size_mb", gibToMib(diskGiB, "diskGiB"));
        }
        if (body.size() == 0) {
            throw new InvalidArgumentException(
                    "at least one of cpu, memoryGiB or diskGiB is required");
        }
        return body;
    }

    private static long gibToMib(double value, String name) throws SandboxException {
        if (!Double.isFinite(value) || value <= 0) {
            throw new InvalidArgumentException(
                    "resource target " + name + " must be a positive number");
        }
        double mib = value * SandboxResizeResult.MIB_PER_GIB;
        if (mib != Math.rint(mib)) {
            throw new InvalidArgumentException(
                    "resource target " + name + " must be an exact number of MiB; "
                            + "use a multiple of 1/" + SandboxResizeResult.MIB_PER_GIB + " GiB");
        }
        if (mib >= (double) Long.MAX_VALUE) {
            throw new InvalidArgumentException("resource target " + name + " is too large");
        }
        return (long) mib;
    }
}
