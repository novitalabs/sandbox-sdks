package ai.novita.sandbox.core;

/**
 * Per-resource resize outcome. Only the resources included in the request are
 * present.
 */
public class SandboxResizeResult {
    /** Conversion factor from the API's MiB values to user-facing GiB. */
    static final int MIB_PER_GIB = 1024;

    private SandboxResizeItem cpu;
    private SandboxResizeItem memory;
    private SandboxResizeItem disk;

    public SandboxResizeItem getCpu() { return cpu; }
    public SandboxResizeItem getMemory() { return memory; }
    public SandboxResizeItem getDisk() { return disk; }

    /** Maps the MiB values the API returned to the units users see. */
    void applyUnits() {
        if (cpu != null) cpu.applyUnit("cores", false);
        if (memory != null) memory.applyUnit("GiB", true);
        if (disk != null) disk.applyUnit("GiB", true);
    }
}
