package ai.novita.sandbox.core;

import java.util.concurrent.TimeUnit;
import java.util.concurrent.TimeoutException;

/** Lifecycle handle for a running process, including PTY operations. */
public class CommandHandle implements AutoCloseable {
    private volatile int pid;
    private volatile CommandResult result;
    private volatile boolean closed;
    private volatile Throwable failure;
    private volatile ConnectRpcClient.StreamHandle streamHandle;
    private Sandbox sandbox;
    private volatile boolean pty;
    private final StringBuilder stdout = new StringBuilder(), stderr = new StringBuilder();
    synchronized void appendOutput(String out, String err){stdout.append(out);stderr.append(err);}
    public synchronized String getStdout(){return stdout.toString();}
    public synchronized String getStderr(){return stderr.toString();}
    /** Stops receiving output while leaving the remote process running. */
    public void disconnect(){ synchronized(this){closed=true;notifyAll();} if(streamHandle!=null)streamHandle.cancel(); }

    void setSandbox(Sandbox s){sandbox=s;}
    void setPty(boolean value){pty=value;}
    synchronized void setResult(CommandResult r){result=r;notifyAll();}
    synchronized void setFailure(Throwable e){if(result==null&&!closed){failure=e;notifyAll();}}
    synchronized void setStreamHandle(ConnectRpcClient.StreamHandle h){streamHandle=h;if(result!=null||closed)h.cancel();}
    /** An End event is authoritative; no HTTP EOF or Connect trailer is needed. */
    synchronized void finish(CommandResult value){result=value;notifyAll();if(streamHandle!=null)streamHandle.cancel();}

    public int getPid(){return pid;}
    public synchronized void setPid(int p){pid=p; notifyAll();}

    synchronized int awaitPid() throws SandboxException { return awaitPid(60); }
    synchronized int awaitPid(int timeoutSeconds) throws SandboxException {
        long deadline = timeoutSeconds==0 ? Long.MAX_VALUE : System.nanoTime() + TimeUnit.SECONDS.toNanos(timeoutSeconds);
        while (pid <= 0 && failure == null && result == null && !closed) {
            long remaining = deadline - System.nanoTime();
            if (remaining <= 0) throw new ai.novita.sandbox.core.TimeoutException("process did not return a pid before the request deadline");
            try { wait(Math.max(1L, TimeUnit.NANOSECONDS.toMillis(remaining))); }
            catch (InterruptedException e) { Thread.currentThread().interrupt(); throw new SandboxException("interrupted waiting for process pid", e); }
        }
        if (failure != null) throw streamFailure(failure);
        if (pid <= 0) throw new SandboxException("process did not return a pid");
        return pid;
    }

    /** Returns the result if the stream has already finished, otherwise {@code null}. */
    public CommandResult getResult(){return result;}

    /**
     * Waits indefinitely for the process to finish.
     *
     * @return the final result
     * @throws SandboxException if the stream failed or the wait was interrupted
     */
    public synchronized CommandResult waitFor() throws SandboxException {
        while (result == null && failure == null && !closed) {
            try { wait(); }
            catch (InterruptedException e) { Thread.currentThread().interrupt(); throw new SandboxException("command wait interrupted", e); }
        }
        if (failure != null) throw streamFailure(failure);
        if (result == null) throw new SandboxException("process ended without a result");
        return checkedResult();
    }

    /**
     * Waits up to {@code timeout} for the process to finish.
     *
     * @param timeout maximum time to wait
     * @param unit time unit of {@code timeout}
     * @return the final result
     * @throws TimeoutException if the process is still running when the timeout elapses
     * @throws SandboxException if the stream failed or the wait was interrupted
     */
    public synchronized CommandResult waitFor(long timeout, TimeUnit unit) throws SandboxException, TimeoutException {
        long deadline = System.nanoTime() + unit.toNanos(timeout);
        while (result == null && failure == null && !closed) {
            long remaining = deadline - System.nanoTime();
            if (remaining <= 0) throw new TimeoutException("command did not finish within " + timeout + " " + unit);
            try { wait(Math.max(1L, TimeUnit.NANOSECONDS.toMillis(remaining))); }
            catch (InterruptedException e) { Thread.currentThread().interrupt(); throw new SandboxException("command wait interrupted", e); }
        }
        if (failure != null) throw streamFailure(failure);
        if (result == null) throw new SandboxException("process ended without a result");
        return checkedResult();
    }

    public CommandResult waitResult() throws SandboxException{return waitFor();}

    public void sendStdin(byte[] data)throws SandboxException{ensureActive();sandbox.commands().sendInput(awaitPid(),data,false);}
    public void closeStdin()throws SandboxException{ensureActive();sandbox.commands().closeStdin(awaitPid());}
    public void sendPtyInput(byte[] data)throws SandboxException{ensureActive();sandbox.commands().sendInput(awaitPid(),data,true);}
    public void resizePty(PtySize size)throws SandboxException{ensureActive();sandbox.commands().resize(awaitPid(),size);}
    public java.util.concurrent.CompletableFuture<Void> resizePtyAsync(PtySize size){return AsyncSupport.runAsync(()->{try{resizePty(size);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}

    public void kill() throws SandboxException{kill(true);}

    /**
     * Terminates the process.
     *
     * <p>Sends the signal and then cancels the underlying HTTP call, which is what
     * actually unblocks the stream reader (interrupting the thread would not).
     */
    public void kill(boolean force)throws SandboxException{
        try { if(sandbox!=null&&pid>0) {
            if(force)sandbox.commands().kill(pid); else sandbox.commands().sendSignal(pid,"SIGTERM");
        } } finally { disconnect(); }
    }

    public java.util.concurrent.CompletableFuture<Void> killAsync(boolean force){return AsyncSupport.runAsync(()->{try{kill(force);}catch(Exception e){throw new java.util.concurrent.CompletionException(e);}});}
    public boolean isClosed(){return closed;}
    public void close() throws SandboxException{if(!closed)kill();}

    private void ensureActive() throws SandboxException {
        if (sandbox == null) throw new SandboxException("command is detached");
        if (closed || result != null) throw new SandboxException("process has already exited");
        if (failure != null) throw streamFailure(failure);
    }

    private CommandResult checkedResult() throws CommandExitException {
        if(result.getExitCode()!=0)throw new CommandExitException(result);
        return result;
    }

    private static SandboxException streamFailure(Throwable failure) {
        Throwable cause = unwrap(failure);
        if (cause instanceof SandboxException) return (SandboxException) cause;
        if (cause instanceof java.io.IOException) return ConnectRpcClient.transportFailure("command stream failed", (java.io.IOException)cause);
        return new SandboxException("command stream failed", cause);
    }

    private static Throwable unwrap(Throwable t) {
        if (t instanceof java.util.concurrent.CompletionException && t.getCause() != null) return t.getCause();
        if (t instanceof java.util.concurrent.ExecutionException && t.getCause() != null) return t.getCause();
        return t;
    }
}
