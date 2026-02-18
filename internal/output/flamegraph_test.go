package output

import (
	"strings"
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

func TestGenerateFlameGraphSVG(t *testing.T) {
	stacks := []model.StackTrace{
		{Stack: "main;do_work;compute", Count: 100, Type: "on-cpu"},
		{Stack: "main;do_work;io_wait", Count: 50, Type: "on-cpu"},
		{Stack: "idle;cpu_idle", Count: 200, Type: "on-cpu"},
	}

	svg := GenerateFlameGraphSVG(stacks, "CPU Profile")

	if svg == "" {
		t.Fatal("empty SVG")
	}
	if !strings.Contains(svg, "<svg") {
		t.Error("missing SVG tag")
	}
	if !strings.Contains(svg, "CPU Profile") {
		t.Error("missing title")
	}
	if !strings.Contains(svg, "350 samples") {
		t.Error("missing sample count")
	}
	if !strings.Contains(svg, "<rect") {
		t.Error("missing rectangles")
	}
}

func TestGenerateFlameGraphSVGEmpty(t *testing.T) {
	svg := GenerateFlameGraphSVG(nil, "Empty")
	if svg != "" {
		t.Error("expected empty string for nil stacks")
	}
}

func TestGenerateFlameGraphFromFolded(t *testing.T) {
	stacks := []model.StackTrace{
		{Stack: "main;work;compute", Count: 100},
		{Stack: "main;work;io", Count: 50},
	}

	folded := GenerateFlameGraphFromFolded(stacks)

	if !strings.Contains(folded, "main;work;compute 100") {
		t.Error("missing first stack")
	}
	if !strings.Contains(folded, "main;work;io 50") {
		t.Error("missing second stack")
	}
}
