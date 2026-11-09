package ai.novita.sandbox.core;

import okhttp3.Call;
import okhttp3.Response;
import java.io.IOException;
import java.util.ArrayList;
import java.util.List;
import java.util.concurrent.*;
import java.util.function.Supplier;

/** Internal shared executor for blocking SDK operations and HTTP cancellation. */
public final class AsyncSupport {
    private AsyncSupport() { }
    private static final ExecutorService EXECUTOR = Executors.newCachedThreadPool(r -> {
        Thread t = new Thread(r, "novita-sdk-async"); t.setDaemon(true); return t;
    });
    private static final ThreadLocal<State> CURRENT = new ThreadLocal<>();
    private static final class State {
        final List<Call> calls = new ArrayList<>();
        volatile boolean cancelled;
        private Thread worker;
        synchronized boolean begin(){if(cancelled)return false;worker=Thread.currentThread();return true;}
        synchronized void finish(){worker=null;calls.clear();}
        synchronized void add(Call call) { if (cancelled) call.cancel(); else calls.add(call); }
        synchronized void cancel(boolean interrupt) { cancelled = true; calls.forEach(Call::cancel); if(interrupt&&worker!=null)worker.interrupt(); }
    }
    public static boolean isCancelled() { State state = CURRENT.get(); return state != null && state.cancelled; }
    public static void throwIfCancelled() { if(isCancelled() || Thread.currentThread().isInterrupted()) throw new CancellationException("SDK operation cancelled"); }
    public static SandboxException transportFailure(String message, IOException error) {
        if(error instanceof java.io.InterruptedIOException)return new ai.novita.sandbox.core.TimeoutException(message + ": " + error.getMessage(),error);
        return new SandboxException(message,error);
    }
    public static Call track(Call call) { State state = CURRENT.get(); if (state != null) state.add(call); return call; }
    public static Response execute(Call call) throws IOException { return track(call).execute(); }
    public static <T> CompletableFuture<T> supplyAsync(Supplier<T> work) {
        State state = new State();
        CompletableFuture<T> result = new CompletableFuture<T>() {
            @Override public boolean cancel(boolean interrupt) {
                if (!super.cancel(interrupt)) return false;
                state.cancel(interrupt);
                return true;
            }
        };
        EXECUTOR.execute(() -> {
            if (!state.begin()) return;
            CURRENT.set(state);
            try { result.complete(work.get()); }
            catch (Throwable error) { result.completeExceptionally(error); }
            finally { CURRENT.remove(); state.finish(); }
        });
        return result;
    }
    public static CompletableFuture<Void> runAsync(Runnable work) {
        return supplyAsync(() -> { work.run(); return null; });
    }
}
