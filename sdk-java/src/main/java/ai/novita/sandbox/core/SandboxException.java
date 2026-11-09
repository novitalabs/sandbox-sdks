package ai.novita.sandbox.core;

/**
 * Base exception for all Novita Sandbox errors.
 */
public class SandboxException extends Exception {
    public SandboxException(String message) {
        super(message);
    }

    public SandboxException(String message, Throwable cause) {
        super(message, cause);
    }
}
