package ai.novita.sandbox.core;
/** Errors raised while requesting, uploading, or executing a template build. */
public class BuildException extends SandboxException { public BuildException(String message){super(message);} public BuildException(String message,Throwable cause){super(message,cause);} }
