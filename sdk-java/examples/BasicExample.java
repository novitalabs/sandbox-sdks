package ai.novita.sandbox.examples;

import ai.novita.sandbox.core.Sandbox;
import ai.novita.sandbox.core.CommandResult;
import ai.novita.sandbox.core.Novita;

/**
 * Basic example demonstrating Novita Sandbox usage.
 */
public class BasicExample {
    public static void main(String[] args) {
        try (Novita client = Novita.create()) {
            // Create a sandbox
            Sandbox sandbox = client.createSandbox("base");
            System.out.println("Created sandbox: " + sandbox.getId());

            try {
                // Write a file
                String content = "Hello, World from Java SDK!";
                sandbox.files().write("/tmp/hello.txt", content.getBytes(), null);
                System.out.println("Wrote file to sandbox");

                // Run a command
                CommandResult result = sandbox.commands().run("cat /tmp/hello.txt", null);
                System.out.println("Command output: " + result.getStdout());

            } finally {
                // Always clean up
                sandbox.kill();
                System.out.println("Sandbox terminated");
            }

        } catch (Exception e) {
            System.err.println("Error: " + e.getMessage());
            e.printStackTrace();
        }
    }
}
