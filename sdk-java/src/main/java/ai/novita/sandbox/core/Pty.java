package ai.novita.sandbox.core;

import java.util.function.Consumer;

/** Operations for creating and interacting with pseudo-terminals. */
public final class Pty {
    private final Sandbox sandbox;

    Pty(Sandbox sandbox){this.sandbox=sandbox;}

    public CommandHandle create(PtyOptions options) throws SandboxException {
        if(options==null||options.getSize()==null)throw new SandboxException("PTY size is required");
        return sandbox.commands().startPty(options);
    }

    public CommandHandle create(PtySize size, Consumer<byte[]> onData) throws SandboxException {
        return create(new PtyOptions().setSize(size).setOnData(onData));
    }

    public CommandHandle connect(int pid) throws SandboxException { return sandbox.commands().connect(pid); }
    public CommandHandle connect(int pid, Consumer<byte[]> onData) throws SandboxException { return sandbox.commands().connect(pid,new ProcessOptions().setOnPty(onData)); }
    public void sendInput(int pid, byte[] data) throws SandboxException { sandbox.commands().sendInput(pid,data,true); }
    public void resize(int pid, PtySize size) throws SandboxException { sandbox.commands().resize(pid,size); }
    public boolean kill(int pid) throws SandboxException { return sandbox.commands().kill(pid); }
}
