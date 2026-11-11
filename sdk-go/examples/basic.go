package main

import (
	"context"
	"fmt"
	"log"

	novita "github.com/novitalabs/sandbox-sdks/sdk-go"
	"github.com/novitalabs/sandbox-sdks/sdk-go/core"
)

func main() {
	ctx := context.Background()

	// Configure the high-level client; the API key is read from NOVITA_API_KEY.
	client, err := novita.New(nil)
	if err != nil {
		log.Fatalf("Failed to initialize Novita: %v", err)
	}

	// Set the template and lifetime for this sandbox.
	sandbox, err := client.Sandbox.Create(ctx, &core.SandboxOpts{
		Template: "base",
		Timeout:  300, // seconds
	})
	if err != nil {
		log.Fatalf("Failed to create sandbox: %v", err)
	}
	defer func() {
		if _, err := sandbox.Kill(ctx); err != nil {
			log.Printf("Failed to kill sandbox: %v", err)
		}
	}()

	fmt.Printf("Created sandbox: %s\n", sandbox.ID())

	// Write a file.
	if _, err := sandbox.Files.WriteText(ctx, "/tmp/hello.txt", "Hello, World from Go SDK!", nil); err != nil {
		log.Fatalf("Failed to write file: %v", err)
	}

	// Run a command and print its stdout.
	result, err := sandbox.Commands.Run(ctx, "cat /tmp/hello.txt", nil)
	if err != nil {
		log.Fatalf("Failed to run command: %v", err)
	}

	fmt.Printf("Output: %s\n", result.Stdout)

	// List the directory we just wrote into.
	entries, err := sandbox.Files.List(ctx, "/tmp", nil)
	if err != nil {
		log.Fatalf("Failed to list directory: %v", err)
	}
	for _, entry := range entries {
		fmt.Printf("  %s (%s, %d bytes)\n", entry.Name, entry.Type, entry.Size)
	}
}
