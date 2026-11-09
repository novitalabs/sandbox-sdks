package ai.novita.sandbox.core;

/** Raised when an envd command exits with a non-zero status. */
public final class CommandExitException extends SandboxException {
    private final CommandResult result;

    public CommandExitException(CommandResult result) {
        super(result.getError() == null || result.getError().isEmpty()
                ? "Command exited with code " + result.getExitCode()
                : result.getError());
        this.result = result;
    }

    public CommandResult getResult() { return result; }
    public int getExitCode() { return result.getExitCode(); }
    public String getStdout() { return result.getStdout(); }
    public String getStderr() { return result.getStderr(); }
    public String getCommandError() { return result.getError(); }
}
