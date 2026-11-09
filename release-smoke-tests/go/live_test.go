package consumer_test

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	novita "github.com/novitalabs/sandbox-sdks/sdk-go/v2"
	interpreter "github.com/novitalabs/sandbox-sdks/sdk-go/v2/code_interpreter"
	"github.com/novitalabs/sandbox-sdks/sdk-go/v2/core"
)

// Opt-in only: creates one billable sandbox, then kills it even after a test failure.
func TestLiveCustomerWorkflow(t *testing.T) {
	if os.Getenv("NOVITA_RUN_LIVE") != "1" {
		t.Skip("set NOVITA_RUN_LIVE=1 to use a real sandbox")
	}
	if os.Getenv("NOVITA_API_KEY") == "" {
		t.Fatal("NOVITA_API_KEY is required")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
	defer cancel()
	client, err := novita.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	sandbox, err := client.CodeInterpreter.Create(ctx, &core.SandboxOpts{
		Template: os.Getenv("NOVITA_CODE_TEMPLATE"), Timeout: 300,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		cleanup, done := context.WithTimeout(context.Background(), 30*time.Second)
		defer done()
		if _, err := sandbox.Kill(cleanup); err != nil {
			t.Errorf("cleanup sandbox %s: %v", sandbox.ID(), err)
		}
	})
	t.Logf("created sandbox %s", sandbox.ID())
	if _, err = sandbox.Files.WriteStream(ctx, "/tmp/consumer.txt", strings.NewReader("consumer-ok"), nil); err != nil {
		t.Fatal(err)
	}
	result, err := sandbox.Commands.Run(ctx, "cat /tmp/consumer.txt", nil)
	if err != nil || result.Stdout != "consumer-ok" {
		t.Fatalf("file/command roundtrip: %+v, %v", result, err)
	}
	execution, err := sandbox.RunCode(ctx, "print('consumer-ok')\n6 * 7", nil)
	if err != nil || execution.Error != nil || len(execution.Results) == 0 {
		t.Fatalf("execute: %+v, %v", execution, err)
	}
	if execution.Results[len(execution.Results)-1].Text != "42" {
		t.Fatalf("expected 42: %+v", execution)
	}
	if !strings.Contains(strings.Join(execution.Logs.Stdout, ""), "consumer-ok") {
		t.Fatalf("expected consumer stdout: %+v", execution.Logs)
	}
	codeContext, err := sandbox.CreateCodeContext(ctx, &interpreter.CreateCodeContextOpts{Language: "python"})
	if err != nil {
		t.Fatal(err)
	}
	if execution, err = sandbox.RunCode(ctx, "customer_value = 41", &interpreter.RunCodeOpts{Context: codeContext}); err != nil || execution.Error != nil {
		t.Fatalf("context assignment: %+v, %v", execution, err)
	}
	reconnected, err := client.CodeInterpreter.Connect(ctx, sandbox.ID(), &core.SandboxOpts{Timeout: 300})
	if err != nil {
		t.Fatal(err)
	}
	execution, err = reconnected.RunCode(ctx, "customer_value + 1", &interpreter.RunCodeOpts{Context: codeContext})
	if err != nil || execution.Error != nil || len(execution.Results) == 0 || execution.Results[len(execution.Results)-1].Text != "42" {
		t.Fatalf("reconnect/context persistence: %+v, %v", execution, err)
	}
	if err = sandbox.RemoveCodeContext(ctx, codeContext.ID, nil); err != nil {
		t.Fatal(err)
	}
}
