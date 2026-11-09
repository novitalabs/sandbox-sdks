package ai.novita.sandbox.core;

/**
 * Handle for a running filesystem watch stream.
 *
 * <p>{@link #close()} cancels the underlying HTTP call, which unblocks the
 * reader thread immediately instead of waiting for the next event.
 */
public class WatchHandle implements AutoCloseable {
    private final ConnectRpcClient.StreamHandle stream;
    private volatile boolean closed;
    private volatile Throwable failure;

    WatchHandle(ConnectRpcClient.StreamHandle stream) { this.stream = stream; }

    void setFailure(Throwable t) { if(!closed)failure = t; }

    public boolean isClosed() { return closed; }

    /** Non-null if the watch terminated because of an error. */
    public Throwable getFailure() { return failure; }

    @Override public void close() {
        closed = true;
        stream.cancel();
    }
}
