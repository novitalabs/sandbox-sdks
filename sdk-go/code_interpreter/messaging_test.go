package code_interpreter

import (
	"reflect"
	"testing"
)

func TestParseResultDeserializesLineChart(t *testing.T) {
	result, err := parseResult(`{"type":"result","is_main_result":true,"plain":"chart","data":{"columns":["x","y"]},"chart":{"type":"line","title":"Trend","x_label":"Time","y_label":"Value","x_scale":"datetime","y_scale":"linear","x_ticks":["2026-01-01"],"x_tick_labels":["Jan"],"y_ticks":[1],"y_tick_labels":["1"],"elements":[{"label":"series","points":[["2026-01-01",1.5]]}]},"custom":"value"}`)
	if err != nil {
		t.Fatal(err)
	}
	chart, ok := result.Chart.(*LineChart)
	if !ok {
		t.Fatalf("Chart = %T, want *LineChart", result.Chart)
	}
	if chart.Title != "Trend" || chart.XScale != ScaleDatetime || len(chart.Elements) != 1 || chart.Elements[0].Points[0].Y != 1.5 {
		t.Fatalf("chart = %#v", chart)
	}
	if result.Text != "chart" || !result.IsMainResult || result.Data["columns"] == nil || result.Extra["custom"] != "value" {
		t.Fatalf("result = %#v", result)
	}
	if _, exists := result.Raw["type"]; exists {
		t.Fatal("Raw retained the protocol type field")
	}
	wantFormats := []string{"text", "data", "chart", "custom"}
	if !reflect.DeepEqual(result.Formats(), wantFormats) {
		t.Fatalf("Formats = %#v, want %#v", result.Formats(), wantFormats)
	}
}

func TestReportTextResultFormats(t *testing.T) {
	result := &Result{Text: "hello"}
	if !reflect.DeepEqual(result.Formats(), []string{"text"}) {
		t.Fatalf("plain-text result formats = %v", result.Formats())
	}
}

func TestParseResultDeserializesSuperAndUnknownCharts(t *testing.T) {
	result, err := parseResult(`{"type":"result","chart":{"type":"superchart","title":"Dashboard","data":[{"type":"pie","title":"Share","elements":[{"label":"A","angle":2.5,"radius":1}]},{"type":"future_chart","title":"Future","elements":[]}]}}`)
	if err != nil {
		t.Fatal(err)
	}
	chart, ok := result.Chart.(*SuperChart)
	if !ok || len(chart.Elements) != 2 {
		t.Fatalf("Chart = %#v", result.Chart)
	}
	if _, ok := chart.Elements[0].(*PieChart); !ok {
		t.Fatalf("first chart = %T", chart.Elements[0])
	}
	unknown, ok := chart.Elements[1].(*UnknownChart)
	if !ok || unknown.ChartType() != ChartUnknown || unknown.RawChart()["type"] != "future_chart" {
		t.Fatalf("unknown chart = %#v", chart.Elements[1])
	}
}

func TestParseOutputMapsExecutionCountAndCallbacks(t *testing.T) {
	execution := &Execution{}
	var callback *Result
	var stdout OutputMessage
	if err := parseOutput(execution, `{"type":"number_of_executions","execution_count":7}`, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := parseOutput(execution, `{"type":"result","text":"ok","is_main_result":true}`, nil, nil, func(result *Result) { callback = result }, nil); err != nil {
		t.Fatal(err)
	}
	if err := parseOutput(execution, `{"type":"stdout","text":"hello","timestamp":123456}`, func(message OutputMessage) { stdout = message }, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if execution.ExecutionCount != 7 || callback == nil || callback.Text != "ok" {
		t.Fatalf("execution = %#v callback = %#v", execution, callback)
	}
	if stdout.Line != "hello" || stdout.Timestamp != 123456 || stdout.Error {
		t.Fatalf("stdout = %#v", stdout)
	}
}

func TestParseOutputAcceptsTimestampString(t *testing.T) {
	execution := &Execution{}
	var got OutputMessage
	if err := parseOutput(execution, `{"type":"stdout","text":"hello","timestamp":"123456"}`, func(message OutputMessage) { got = message }, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got.Timestamp != 123456 {
		t.Fatalf("timestamp = %d, want 123456", got.Timestamp)
	}
	if err := parseOutput(execution, `{"type":"stderr","text":"oops","timestamp":"2026-09-14T07:12:00.123Z"}`, nil, func(message OutputMessage) { got = message }, nil, nil); err != nil {
		t.Fatal(err)
	}
	if got.Timestamp == 0 || !got.Error {
		t.Fatalf("timestamp/error = %#v", got)
	}
}
