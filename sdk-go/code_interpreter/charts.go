package code_interpreter

import (
	"encoding/json"
	"fmt"
)

type ChartType string

const (
	ChartLine          ChartType = "line"
	ChartScatter       ChartType = "scatter"
	ChartBar           ChartType = "bar"
	ChartPie           ChartType = "pie"
	ChartBoxAndWhisker ChartType = "box_and_whisker"
	ChartSuper         ChartType = "superchart"
	ChartUnknown       ChartType = "unknown"
)

type ScaleType string

const (
	ScaleLinear      ScaleType = "linear"
	ScaleDatetime    ScaleType = "datetime"
	ScaleCategorical ScaleType = "categorical"
	ScaleLog         ScaleType = "log"
	ScaleSymlog      ScaleType = "symlog"
	ScaleLogit       ScaleType = "logit"
	ScaleFunction    ScaleType = "function"
	ScaleFunctionLog ScaleType = "functionlog"
	ScaleAsinh       ScaleType = "asinh"
	ScaleUnknown     ScaleType = "unknown"
)

// Chart is implemented by every deserialized chart result.
type Chart interface {
	ChartType() ChartType
	RawChart() map[string]any
}

type ChartBase struct {
	Type  ChartType      `json:"type"`
	Title string         `json:"title"`
	Raw   map[string]any `json:"-"`
}

func (c ChartBase) ChartType() ChartType     { return c.Type }
func (c ChartBase) RawChart() map[string]any { return c.Raw }

type Chart2D struct {
	ChartBase
	XLabel string `json:"x_label,omitempty"`
	YLabel string `json:"y_label,omitempty"`
	XUnit  string `json:"x_unit,omitempty"`
	YUnit  string `json:"y_unit,omitempty"`
}

type Point struct {
	X any
	Y any
}

func (p *Point) UnmarshalJSON(data []byte) error {
	var values []any
	if err := json.Unmarshal(data, &values); err != nil {
		return err
	}
	if len(values) != 2 {
		return fmt.Errorf("chart point must contain exactly two values")
	}
	p.X, p.Y = values[0], values[1]
	return nil
}

type PointData struct {
	Label  string  `json:"label"`
	Points []Point `json:"points"`
}

type PointChart struct {
	Chart2D
	XTicks      []any       `json:"x_ticks"`
	XTickLabels []string    `json:"x_tick_labels"`
	XScale      ScaleType   `json:"x_scale"`
	YTicks      []any       `json:"y_ticks"`
	YTickLabels []string    `json:"y_tick_labels"`
	YScale      ScaleType   `json:"y_scale"`
	Elements    []PointData `json:"elements"`
}

type LineChart struct{ PointChart }
type ScatterChart struct{ PointChart }

type BarData struct {
	Label string `json:"label"`
	Group string `json:"group"`
	Value string `json:"value"`
}

type BarChart struct {
	Chart2D
	Elements []BarData `json:"elements"`
}

type PieData struct {
	Label  string  `json:"label"`
	Angle  float64 `json:"angle"`
	Radius float64 `json:"radius"`
}

type PieChart struct {
	ChartBase
	Elements []PieData `json:"elements"`
}

type BoxAndWhiskerData struct {
	Label         string    `json:"label"`
	Min           float64   `json:"min"`
	FirstQuartile float64   `json:"first_quartile"`
	Median        float64   `json:"median"`
	ThirdQuartile float64   `json:"third_quartile"`
	Max           float64   `json:"max"`
	Outliers      []float64 `json:"outliers"`
}

type BoxAndWhiskerChart struct {
	Chart2D
	Elements []BoxAndWhiskerData `json:"elements"`
}

type SuperChart struct {
	ChartBase
	Elements []Chart `json:"-"`
}

type UnknownChart struct {
	ChartBase
	Elements []any `json:"elements,omitempty"`
}

func deserializeChart(value any) (Chart, error) {
	raw, ok := value.(map[string]any)
	if !ok || len(raw) == 0 {
		return nil, nil
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	chartType, _ := raw["type"].(string)
	setRaw := func(base *ChartBase) {
		base.Raw = raw
		base.Type = ChartType(chartType)
	}
	switch ChartType(chartType) {
	case ChartLine:
		var chart LineChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		setRaw(&chart.ChartBase)
		return &chart, nil
	case ChartScatter:
		var chart ScatterChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		setRaw(&chart.ChartBase)
		return &chart, nil
	case ChartBar:
		var chart BarChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		setRaw(&chart.ChartBase)
		return &chart, nil
	case ChartPie:
		var chart PieChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		setRaw(&chart.ChartBase)
		return &chart, nil
	case ChartBoxAndWhisker:
		var chart BoxAndWhiskerChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		setRaw(&chart.ChartBase)
		return &chart, nil
	case ChartSuper:
		var base ChartBase
		if err := json.Unmarshal(data, &base); err != nil {
			return nil, err
		}
		setRaw(&base)
		values, _ := raw["elements"].([]any)
		if values == nil {
			values, _ = raw["data"].([]any)
		}
		chart := &SuperChart{ChartBase: base}
		for _, value := range values {
			child, err := deserializeChart(value)
			if err != nil {
				return nil, err
			}
			if child != nil {
				chart.Elements = append(chart.Elements, child)
			}
		}
		return chart, nil
	default:
		var chart UnknownChart
		if err := json.Unmarshal(data, &chart); err != nil {
			return nil, err
		}
		chart.Type = ChartUnknown
		chart.Raw = raw
		return &chart, nil
	}
}
