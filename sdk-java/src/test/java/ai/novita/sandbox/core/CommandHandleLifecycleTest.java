package ai.novita.sandbox.core;

import org.junit.jupiter.api.Test;

import java.util.concurrent.CompletableFuture;
import java.util.concurrent.TimeUnit;

import static org.junit.jupiter.api.Assertions.assertFalse;
import static org.junit.jupiter.api.Assertions.assertSame;

class CommandHandleLifecycleTest {
    @Test
    void waitForBlocksUntilTerminalResultIsPublished() throws Exception {
        CommandHandle handle = new CommandHandle();
        CompletableFuture<CommandResult> waiting = CompletableFuture.supplyAsync(() -> {
            try {
                return handle.waitFor();
            } catch (SandboxException e) {
                throw new java.util.concurrent.CompletionException(e);
            }
        });

        Thread.sleep(50);
        assertFalse(waiting.isDone());

        CommandResult result = new CommandResult();
        result.setExitCode(0);
        handle.setResult(result);

        assertSame(result, waiting.get(1, TimeUnit.SECONDS));
    }
}
