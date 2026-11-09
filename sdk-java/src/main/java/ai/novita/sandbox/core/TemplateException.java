package ai.novita.sandbox.core;

/**
 * Raised when a sandbox template predates the current SDK contract, matching
 * JS {@code TemplateError} and Python {@code TemplateException}.
 */
public class TemplateException extends SandboxException {
    public TemplateException(String message) {
        super(message);
    }
}
