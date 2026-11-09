package ai.novita.sandbox.core;

public class CommandResult {
    private String stdout;
    private String stderr;
    private String error;
    private int exitCode;
    public String getStdout() { return stdout; }
    public void setStdout(String stdout) { this.stdout = stdout; }
    public String getStderr() { return stderr; }
    public void setStderr(String stderr) { this.stderr = stderr; }
    public int getExitCode() { return exitCode; }
    public void setExitCode(int exitCode) { this.exitCode = exitCode; }
    public String getError() { return error; }
    public void setError(String error) { this.error = error; }
}
