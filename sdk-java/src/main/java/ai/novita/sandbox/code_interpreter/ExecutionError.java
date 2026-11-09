package ai.novita.sandbox.code_interpreter;

/** An error raised by the user's code, distinct from an HTTP/transport failure. */
public final class ExecutionError {
    private final String name, value, traceback;
    public ExecutionError(String name, String value, String traceback){this.name=name;this.value=value;this.traceback=traceback;}
    public String getName(){return name;}
    public String getValue(){return value;}
    public String getTraceback(){return traceback;}
    @Override public String toString(){return name + ": " + value + (traceback==null ? "" : "\n"+traceback);}
}
