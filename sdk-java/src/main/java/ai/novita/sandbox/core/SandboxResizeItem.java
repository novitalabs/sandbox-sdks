package ai.novita.sandbox.core;

/**
 * Outcome of one resource in a resize request.
 *
 * <p>{@code requested}, {@code previous} and {@code current} use
 * {@code unit}: cores for CPU, GiB for memory and disk.
 */
public class SandboxResizeItem {
    /** One of {@code applied}, {@code skipped}, {@code failed} or {@code unknown}. */
    private String status;
    /** Requested total: cores for CPU, GiB for memory and disk. */
    private Double requested;
    /** Total before the call, in the same unit. */
    private Double previous;
    /** Total after the call, in the same unit. */
    private Double current;
    /** Unit of the totals: {@code cores} or {@code GiB}. */
    private String unit;
    /** HTTP status this resource would have returned on its own. */
    private Integer code;
    /** Reason code when the resource was not applied. */
    private String reason;
    /** Human-readable reason when the resource was not applied. */
    private String message;

    public String getStatus() { return status; }
    public Double getRequested() { return requested; }
    public Double getPrevious() { return previous; }
    public Double getCurrent() { return current; }
    public String getUnit() { return unit; }
    public Integer getCode() { return code; }
    public String getReason() { return reason; }
    public String getMessage() { return message; }

    /** Maps the MiB value the API returned to the GiB value users see. */
    void applyUnit(String unit, boolean toGiB) {
        this.unit = unit;
        if (!toGiB) return;
        if (requested != null) requested = requested / SandboxResizeResult.MIB_PER_GIB;
        if (previous != null) previous = previous / SandboxResizeResult.MIB_PER_GIB;
        if (current != null) current = current / SandboxResizeResult.MIB_PER_GIB;
    }
}
