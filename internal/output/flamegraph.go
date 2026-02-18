package output

import (
	"fmt"
	"sort"
	"strings"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
)

// GenerateFlameGraphSVG converts folded stacks to a simple FlameGraph SVG.
// For a production-quality SVG, consider calling Brendan Gregg's flamegraph.pl.
// This is a simplified inline implementation for when FlameGraph tools aren't available.
func GenerateFlameGraphSVG(stacks []model.StackTrace, title string) string {
	if len(stacks) == 0 {
		return ""
	}

	// Sort by stack name for consistent output
	sort.Slice(stacks, func(i, j int) bool {
		return stacks[i].Stack < stacks[j].Stack
	})

	var totalSamples int
	for _, s := range stacks {
		totalSamples += s.Count
	}

	// SVG dimensions
	width := 1200
	height := 400
	frameHeight := 16
	fontSize := 12

	var sb strings.Builder
	sb.WriteString(fmt.Sprintf(`<?xml version="1.0" standalone="no"?>
<!DOCTYPE svg PUBLIC "-//W3C//DTD SVG 1.1//EN" "http://www.w3.org/Graphics/SVG/1.1/DTD/svg11.dtd">
<svg version="1.1" width="%d" height="%d" xmlns="http://www.w3.org/2000/svg">
<style>
  .func { font-family: monospace; font-size: %dpx; }
  rect:hover { stroke: black; stroke-width: 1; }
</style>
<text x="10" y="20" class="func" style="font-size:14px; font-weight:bold">%s — %d samples</text>
`, width, height, fontSize, title, totalSamples))

	// Build a simplified flame chart
	// Group frames by depth
	type frame struct {
		name   string
		count  int
		depth  int
		xStart float64
		xWidth float64
	}

	var frames []frame
	x := 0.0
	for _, stack := range stacks {
		functions := strings.Split(stack.Stack, ";")
		stackWidth := float64(stack.Count) / float64(totalSamples) * float64(width-20)

		for depth, fn := range functions {
			frames = append(frames, frame{
				name:   fn,
				count:  stack.Count,
				depth:  depth,
				xStart: x + 10,
				xWidth: stackWidth,
			})
		}
		x += stackWidth
	}

	colors := []string{
		"#ff6633", "#ff8855", "#ffaa77", "#ffcc99",
		"#ff5533", "#ff7744", "#ff9966", "#ffbb88",
		"#e85533", "#e87744", "#e89966", "#eebb88",
	}

	for _, f := range frames {
		if f.xWidth < 1 {
			continue // too small to render
		}
		y := float64(height-30) - float64(f.depth*frameHeight)
		if y < 30 {
			continue
		}
		color := colors[f.depth%len(colors)]
		label := f.name
		if len(label) > int(f.xWidth/7) {
			maxChars := int(f.xWidth / 7)
			if maxChars > 3 {
				label = label[:maxChars-2] + ".."
			} else {
				label = ""
			}
		}

		sb.WriteString(fmt.Sprintf(
			`<rect x="%.1f" y="%.1f" width="%.1f" height="%d" fill="%s" rx="1"/>`,
			f.xStart, y, f.xWidth, frameHeight-1, color))
		if label != "" {
			sb.WriteString(fmt.Sprintf(
				`<text x="%.1f" y="%.1f" class="func">%s</text>`,
				f.xStart+2, y+float64(frameHeight-3), label))
		}
		sb.WriteString("\n")
	}

	sb.WriteString("</svg>\n")
	return sb.String()
}

// GenerateFlameGraphFromFolded writes a folded stack file that can be piped to flamegraph.pl.
func GenerateFlameGraphFromFolded(stacks []model.StackTrace) string {
	var sb strings.Builder
	for _, s := range stacks {
		sb.WriteString(fmt.Sprintf("%s %d\n", s.Stack, s.Count))
	}
	return sb.String()
}
