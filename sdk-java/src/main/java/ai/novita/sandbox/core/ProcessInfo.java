package ai.novita.sandbox.core;

import java.util.ArrayList;
import java.util.Collections;
import java.util.HashMap;
import java.util.List;
import java.util.Map;

/** Information about a command or PTY process running in a sandbox. */
public final class ProcessInfo {
    private final int pid;
    private final String tag;
    private final String command;
    private final List<String> args;
    private final Map<String, String> environment;
    private final String workingDirectory;

    ProcessInfo(int pid, String tag, String command, List<String> args,
                Map<String, String> environment, String workingDirectory) {
        this.pid = pid;
        this.tag = tag;
        this.command = command;
        this.args = Collections.unmodifiableList(
                args == null ? new ArrayList<>() : new ArrayList<>(args));
        this.environment = Collections.unmodifiableMap(
                environment == null ? new HashMap<>() : new HashMap<>(environment));
        this.workingDirectory = workingDirectory;
    }

    public int getPid() { return pid; }
    public String getTag() { return tag; }
    public String getCommand() { return command; }
    public List<String> getArgs() { return args; }
    public Map<String, String> getEnvironment() { return environment; }
    public String getWorkingDirectory() { return workingDirectory; }
}
