package ai.novita.sandbox.core;
/** Errors raised by Git operations inside a sandbox. */
public class GitException extends SandboxException { public GitException(String message){super(message);} public GitException(String message,Throwable cause){super(message,cause);} }
