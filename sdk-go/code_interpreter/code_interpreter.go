// Package code_interpreter runs code inside a Novita Agent Sandbox through the
// Jupyter server exposed by the sandbox on port 49999.
//
// Use the root novita package as the default entrypoint. The returned Sandbox
// embeds the core sandbox so that lifecycle, filesystem and command APIs stay
// available. Import novita from github.com/novitalabs/sandbox-sdks/sdk-go:
//
//	client, err := novita.New(nil)
//	if err != nil {
//		return err
//	}
//	sandbox, err := client.CodeInterpreter.Create(ctx, nil)
//	if err != nil {
//		return err
//	}
//	defer sandbox.Kill(ctx)
//
//	execution, err := sandbox.RunCode(ctx, "print('hello')", nil)
//	if err != nil {
//		return err
//	}
//	fmt.Println(execution.Logs.Stdout)
//	if execution.Error != nil {
//		return execution.Error
//	}
//
// This subpackage provides execution options, result types and direct runtime
// constructors for advanced integrations and compatibility.
//
// Execution failures are reported on Execution.Error rather than as a Go
// error, matching the JS and Python SDKs. The returned error is reserved for
// transport, protocol and timeout failures.
package code_interpreter
