package ai.novita.sandbox.core;

/**
 * Query filters for {@code Sandbox.getEvents}, matching JS
 * {@code SandboxEventsOpts} (which also carries connection options, hence the
 * {@link SandboxOpts} base).
 */
public class SandboxEventsOptions extends SandboxOpts {
    private Long startTime;
    private Long endTime;
    private String sandboxID;
    private String templateID;
    /** Comma-separated event names, e.g. {@code "created,running"}. */
    private String events;
    private String state;
    private Boolean orderAsc;
    private Integer offset;
    private Integer limit;

    public Long getStartTime() { return startTime; }
    public SandboxEventsOptions setStartTime(Long startTime) { this.startTime = startTime; return this; }
    public Long getEndTime() { return endTime; }
    public SandboxEventsOptions setEndTime(Long endTime) { this.endTime = endTime; return this; }
    public String getSandboxID() { return sandboxID; }
    public SandboxEventsOptions setSandboxID(String sandboxID) { this.sandboxID = sandboxID; return this; }
    public String getTemplateID() { return templateID; }
    public SandboxEventsOptions setTemplateID(String templateID) { this.templateID = templateID; return this; }
    public String getEvents() { return events; }
    public SandboxEventsOptions setEvents(String events) { this.events = events; return this; }
    public String getState() { return state; }
    public SandboxEventsOptions setState(String state) { this.state = state; return this; }
    public Boolean getOrderAsc() { return orderAsc; }
    public SandboxEventsOptions setOrderAsc(Boolean orderAsc) { this.orderAsc = orderAsc; return this; }
    public Integer getOffset() { return offset; }
    public SandboxEventsOptions setOffset(Integer offset) { this.offset = offset; return this; }
    public Integer getLimit() { return limit; }
    public SandboxEventsOptions setLimit(Integer limit) { this.limit = limit; return this; }
}
