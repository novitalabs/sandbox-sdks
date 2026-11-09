package ai.novita.sandbox.core;

/**
 * The request was refused (HTTP 403): authenticated but not permitted, e.g.
 * an alias owned by another user. JS/Python branch on this status separately
 * from invalid credentials (401).
 */
public class ForbiddenException extends SandboxException {
    public ForbiddenException(String message) { super(message); }
}
