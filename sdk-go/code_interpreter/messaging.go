// Package code_interpreter provides sandboxed code execution backed by a
// Jupyter server running inside the sandbox.
//
// It mirrors the JS and Python code-interpreter entrypoints: a Sandbox that
// embeds the core sandbox and adds runCode plus context management.
package code_interpreter

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Context is a Jupyter execution context. Each context keeps its own kernel
// state, so variables defined in one are not visible in another.
type Context struct {
	// ID of the context.
	ID string `json:"id"`
	// Language the context executes.
	Language string `json:"language"`
	// Cwd is the working directory of the context.
	Cwd string `json:"cwd"`
}

// OutputMessage is a single line written to stdout or stderr during execution.
type OutputMessage struct {
	// Line is the text that was written.
	Line string
	// Timestamp is the server-provided Unix epoch timestamp.
	Timestamp int64
	// Error reports whether the line came from stderr.
	Error bool
}

// String returns the output line.
func (o OutputMessage) String() string {
	return o.Line
}

// ExecutionError describes a failure raised while executing code.
type ExecutionError struct {
	// Name of the error, such as "NameError".
	Name string
	// Value is the error message.
	Value string
	// Traceback is the raw formatted traceback.
	Traceback string
}

// Error lets an ExecutionError satisfy the error interface.
func (e *ExecutionError) Error() string {
	if e.Traceback != "" {
		return e.Traceback
	}
	return fmt.Sprintf("%s: %s", e.Name, e.Value)
}

// MIMEType identifies a representation of a result, such as "text" or "png".
type MIMEType = string

// Result is one output of a cell: the value of the last expression, or a
// display() call. A result can carry several representations at once.
type Result struct {
	// IsMainResult marks the value of the cell's last expression.
	IsMainResult bool

	// Text is the plain-text representation.
	Text string
	// HTML is the HTML representation.
	HTML string
	// Markdown is the Markdown representation.
	Markdown string
	// SVG is the SVG representation.
	SVG string
	// PNG is the base64-encoded PNG representation.
	PNG string
	// JPEG is the base64-encoded JPEG representation.
	JPEG string
	// PDF is the base64-encoded PDF representation.
	PDF string
	// LaTeX is the LaTeX representation.
	LaTeX string
	// JSON is the JSON representation.
	JSON string
	// JavaScript is the JavaScript representation.
	JavaScript string

	// Data holds structured data, for example from a DataFrame.
	Data map[string]interface{}
	// Chart holds chart data when a chart was rendered.
	Chart Chart

	// Extra carries any representation not covered above.
	Extra map[string]interface{}
	// Raw is the untouched payload, including every representation.
	Raw map[string]interface{}
}

// Formats lists the representations present on the result.
func (r *Result) Formats() []string {
	formats := make([]string, 0, 8)

	if r.Text != "" {
		formats = append(formats, "text")
	}
	if r.HTML != "" {
		formats = append(formats, "html")
	}
	if r.Markdown != "" {
		formats = append(formats, "markdown")
	}
	if r.SVG != "" {
		formats = append(formats, "svg")
	}
	if r.PNG != "" {
		formats = append(formats, "png")
	}
	if r.JPEG != "" {
		formats = append(formats, "jpeg")
	}
	if r.PDF != "" {
		formats = append(formats, "pdf")
	}
	if r.LaTeX != "" {
		formats = append(formats, "latex")
	}
	if r.JSON != "" {
		formats = append(formats, "json")
	}
	if r.JavaScript != "" {
		formats = append(formats, "javascript")
	}
	if r.Data != nil {
		formats = append(formats, "data")
	}
	if r.Chart != nil {
		formats = append(formats, "chart")
	}

	extra := make([]string, 0, len(r.Extra))
	for key := range r.Extra {
		extra = append(extra, key)
	}
	sort.Strings(extra)
	formats = append(formats, extra...)

	return formats
}

// Logs holds the stdout and stderr lines produced during an execution.
type Logs struct {
	// Stdout lines, in order.
	Stdout []string
	// Stderr lines, in order.
	Stderr []string
}

// Execution is the result of running a cell.
type Execution struct {
	// Results are the cell outputs: the last expression plus any display calls.
	Results []*Result
	// Logs holds everything written to stdout and stderr.
	Logs Logs
	// Error is set when execution failed.
	Error *ExecutionError
	// ExecutionCount is the kernel's execution counter for this cell.
	ExecutionCount int
}

// Text returns the plain-text representation of the main result.
func (e *Execution) Text() (string, bool) {
	for _, result := range e.Results {
		if result.IsMainResult {
			return result.Text, true
		}
	}
	return "", false
}

// knownFormats are the representations handled by dedicated Result fields.
// Anything else lands in Extra.
var knownFormats = map[string]bool{
	"plain":      true,
	"text":       true,
	"html":       true,
	"markdown":   true,
	"svg":        true,
	"png":        true,
	"jpeg":       true,
	"pdf":        true,
	"latex":      true,
	"json":       true,
	"javascript": true,
	"data":       true,
	"chart":      true,
	"extra":      true,
}

// streamMessage is one NDJSON line from the execution stream.
type streamMessage struct {
	Type string `json:"type"`

	// stdout / stderr
	Text      string            `json:"text"`
	Timestamp flexibleTimestamp `json:"timestamp"`

	// error
	Name      string `json:"name"`
	Value     string `json:"value"`
	Traceback string `json:"traceback"`

	// number_of_executions
	ExecutionCount int `json:"execution_count"`

	// result
	IsMainResult bool `json:"is_main_result"`
}

// flexibleTimestamp accepts the timestamp encodings used by different
// Jupyter/envd versions: integer JSON values, numeric strings, and RFC3339
// strings. OutputMessage keeps the stable Go representation in Unix nanos.
type flexibleTimestamp int64

func (t *flexibleTimestamp) UnmarshalJSON(data []byte) error {
	value := strings.TrimSpace(string(data))
	if value == "" || value == "null" {
		*t = 0
		return nil
	}
	if value[0] == '"' {
		var s string
		if err := json.Unmarshal(data, &s); err != nil {
			return err
		}
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			*t = flexibleTimestamp(n)
			return nil
		}
		parsed, err := time.Parse(time.RFC3339Nano, s)
		if err != nil {
			return fmt.Errorf("invalid timestamp %q: %w", s, err)
		}
		*t = flexibleTimestamp(parsed.UnixNano())
		return nil
	}
	var n json.Number
	if err := json.Unmarshal(data, &n); err != nil {
		return err
	}
	parsed, err := strconv.ParseInt(string(n), 10, 64)
	if err != nil {
		return err
	}
	*t = flexibleTimestamp(parsed)
	return nil
}

// parseOutput applies one NDJSON line to the execution, invoking the
// matching callback. Unknown message types are ignored, matching the other
// SDKs so newer servers can add fields without breaking older clients.
func parseOutput(
	execution *Execution,
	line string,
	onStdout func(OutputMessage),
	onStderr func(OutputMessage),
	onResult func(*Result),
	onError func(*ExecutionError),
) error {
	line = strings.TrimSpace(line)
	if line == "" {
		return nil
	}

	var msg streamMessage
	if err := json.Unmarshal([]byte(line), &msg); err != nil {
		return fmt.Errorf("failed to parse execution output: %w", err)
	}

	switch msg.Type {
	case "result":
		result, err := parseResult(line)
		if err != nil {
			return err
		}
		execution.Results = append(execution.Results, result)
		if onResult != nil {
			onResult(result)
		}

	case "stdout":
		execution.Logs.Stdout = append(execution.Logs.Stdout, msg.Text)
		if onStdout != nil {
			onStdout(OutputMessage{Line: msg.Text, Timestamp: int64(msg.Timestamp), Error: false})
		}

	case "stderr":
		execution.Logs.Stderr = append(execution.Logs.Stderr, msg.Text)
		if onStderr != nil {
			onStderr(OutputMessage{Line: msg.Text, Timestamp: int64(msg.Timestamp), Error: true})
		}

	case "error":
		execution.Error = &ExecutionError{
			Name:      msg.Name,
			Value:     msg.Value,
			Traceback: msg.Traceback,
		}
		if onError != nil {
			onError(execution.Error)
		}

	case "number_of_executions":
		execution.ExecutionCount = msg.ExecutionCount
	}

	return nil
}

// parseResult builds a Result from a raw "result" line, mapping every
// representation into its dedicated field and collecting the rest in Extra.
func parseResult(line string) (*Result, error) {
	var raw map[string]interface{}
	if err := json.Unmarshal([]byte(line), &raw); err != nil {
		return nil, fmt.Errorf("failed to parse result: %w", err)
	}

	cleanRaw := make(map[string]interface{}, len(raw))
	for key, value := range raw {
		if key != "type" && key != "is_main_result" {
			cleanRaw[key] = value
		}
	}
	result := &Result{Raw: cleanRaw, Extra: map[string]interface{}{}}

	if main, ok := raw["is_main_result"].(bool); ok {
		result.IsMainResult = main
	}

	// Representations are read as strings; anything else is left in Raw.
	readString := func(key string) string {
		if v, ok := raw[key].(string); ok {
			return v
		}
		return ""
	}

	result.Text = readString("text")
	if result.Text == "" {
		result.Text = readString("plain")
	}
	result.HTML = readString("html")
	result.Markdown = readString("markdown")
	result.SVG = readString("svg")
	result.PNG = readString("png")
	result.JPEG = readString("jpeg")
	result.PDF = readString("pdf")
	result.LaTeX = readString("latex")
	result.JSON = readString("json")
	result.JavaScript = readString("javascript")

	if data, ok := raw["data"].(map[string]interface{}); ok {
		result.Data = data
	}
	if chart, ok := raw["chart"]; ok {
		parsedChart, err := deserializeChart(chart)
		if err != nil {
			return nil, fmt.Errorf("failed to parse chart: %w", err)
		}
		result.Chart = parsedChart
	}

	for key, value := range raw {
		if key == "type" || key == "is_main_result" {
			continue
		}
		if knownFormats[key] {
			continue
		}
		result.Extra[key] = value
	}

	return result, nil
}

// readLines reads the stream line by line, yielding each complete line.
// A trailing line without a newline is still yielded.
func readLines(reader io.Reader, yield func(line string) error) error {
	scanner := bufio.NewScanner(reader)
	// Execution output can contain long lines, such as base64 images.
	scanner.Buffer(make([]byte, 0, 64*1024), 16*1024*1024)

	for scanner.Scan() {
		if err := yield(scanner.Text()); err != nil {
			return err
		}
	}

	return scanner.Err()
}
