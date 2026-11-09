package example.customer;

import ai.novita.sandbox.core.*;
import ai.novita.sandbox.code_interpreter.*;
import org.junit.jupiter.api.Test;
import org.junit.jupiter.api.Timeout;
import org.junit.jupiter.api.condition.EnabledIfEnvironmentVariable;
import java.io.ByteArrayInputStream;
import java.nio.charset.StandardCharsets;
import static org.junit.jupiter.api.Assertions.*;

/** Opt-in: creates one billable sandbox and cleans it up in finally. */
@EnabledIfEnvironmentVariable(named="NOVITA_RUN_LIVE", matches="1")
@Timeout(180)
class LiveCustomerTest {
    @Test void customerCanCreateExecuteReconnectAndCleanUp() throws Exception {
        assertNotNull(System.getenv("NOVITA_API_KEY"), "NOVITA_API_KEY is required");
        SandboxOpts opts = new SandboxOpts();
        opts.setApiKey(System.getenv("NOVITA_API_KEY"));
        String template = System.getenv("NOVITA_CODE_TEMPLATE");
        if (template == null || template.isEmpty()) template = "code-interpreter-v1";
        try (Novita client = Novita.create(opts);
             CodeInterpreterSandbox sandbox = CodeInterpreterSandbox.wrap(client.createSandbox(template))) {
            CodeInterpreterSandbox connected = null;
            try {
                String id = sandbox.getSandbox().getId();
                System.out.println("Created sandbox: " + id);
                sandbox.getSandbox().files().writeStream("/tmp/consumer.txt", new ByteArrayInputStream("consumer-ok".getBytes(StandardCharsets.UTF_8)));
                assertEquals("consumer-ok", sandbox.getSandbox().commands().run("cat /tmp/consumer.txt", null).getStdout());
                ExecutionResult output = sandbox.runCode("print('consumer-ok')\n6 * 7", null);
                assertNull(output.getExecutionError());
                assertTrue(output.getStdout().contains("consumer-ok"));
                assertEquals("42", output.getRichResults().get(output.getRichResults().size() - 1).getText());
                try (ExecutionContext context = sandbox.createContext("python", "/home/user")) {
                    assertNull(context.execute("customer_value = 41").getExecutionError());
                    connected = CodeInterpreterSandbox.wrap(client.connectSandbox(id));
                    output = connected.runCode("customer_value + 1", new RunCodeOpts().setContextId(context.getId()));
                    assertNull(output.getExecutionError());
                    assertEquals("42", output.getRichResults().get(output.getRichResults().size() - 1).getText());
                }
            } finally {
                // Closing a Java sandbox also terminates it, so remove the context first.
                if (connected != null) connected.close();
            }
        }
    }
}
