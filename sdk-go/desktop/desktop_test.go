package desktop

import (
	"reflect"
	"testing"
)

func TestMapKey(t *testing.T) {
	tests := map[string]string{
		"ENTER": "Return", "ctrl": "Control_L", "page_down": "Page_Down",
		"windows": "Super_L", "F12": "F12", "A": "a",
	}
	for input, want := range tests {
		if got := mapKey(input); got != want {
			t.Errorf("mapKey(%q) = %q, want %q", input, got, want)
		}
	}
}

func TestNonEmptyLines(t *testing.T) {
	if got, want := nonEmptyLines(" 1\n\n2 \n"), []string{"1", "2"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("nonEmptyLines = %#v", got)
	}
}
