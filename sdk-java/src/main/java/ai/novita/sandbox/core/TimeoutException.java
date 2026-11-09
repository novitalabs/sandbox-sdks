package ai.novita.sandbox.core;

public class TimeoutException extends SandboxException {
    public TimeoutException(String message) { super(message); }
    public TimeoutException(String message, Throwable cause) { super(message, cause); }
}
