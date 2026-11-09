package ai.novita.sandbox.core;

/** A single sandbox lifecycle event, matching the JS/Python {@code SandboxEventItem}. */
public class SandboxEventItem {
    private String eventID;
    private long recordAt;
    private String templateID;
    private String templateName;
    private String sandboxID;
    private String eventName;
    private String state;
    private String errorMsg;
    private int statusCode;

    public String getEventID() { return eventID; }
    public void setEventID(String eventID) { this.eventID = eventID; }
    public long getRecordAt() { return recordAt; }
    public void setRecordAt(long recordAt) { this.recordAt = recordAt; }
    public String getTemplateID() { return templateID; }
    public void setTemplateID(String templateID) { this.templateID = templateID; }
    public String getTemplateName() { return templateName; }
    public void setTemplateName(String templateName) { this.templateName = templateName; }
    public String getSandboxID() { return sandboxID; }
    public void setSandboxID(String sandboxID) { this.sandboxID = sandboxID; }
    public String getEventName() { return eventName; }
    public void setEventName(String eventName) { this.eventName = eventName; }
    public String getState() { return state; }
    public void setState(String state) { this.state = state; }
    public String getErrorMsg() { return errorMsg; }
    public void setErrorMsg(String errorMsg) { this.errorMsg = errorMsg; }
    public int getStatusCode() { return statusCode; }
    public void setStatusCode(int statusCode) { this.statusCode = statusCode; }
}
