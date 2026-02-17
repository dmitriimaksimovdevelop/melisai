package output

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/baikal/sysdiag/internal/model"
)

func TestWriteJSONToFile(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{
			Tool:          "sysdiag",
			Version:       "0.1.0",
			SchemaVersion: "1.0.0",
			Hostname:      "test",
			Profile:       "quick",
		},
		Categories: map[string][]model.Result{},
		Summary: model.Summary{
			HealthScore: 100,
			Anomalies:   []model.Anomaly{},
			Resources:   map[string]model.USEMetric{},
		},
	}

	tmpDir := t.TempDir()
	outPath := filepath.Join(tmpDir, "report.json")

	if err := WriteJSON(report, outPath); err != nil {
		t.Fatalf("WriteJSON: %v", err)
	}

	// Verify file exists and has content
	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	if len(data) < 10 {
		t.Error("output file too small")
	}

	content := string(data)
	if !containsStr(content, `"schema_version": "1.0.0"`) {
		t.Error("output missing schema_version")
	}
	if !containsStr(content, `"health_score": 100`) {
		t.Error("output missing health_score")
	}
}

func TestWriteJSONStdout(t *testing.T) {
	report := &model.Report{
		Metadata: model.Metadata{
			Tool: "sysdiag",
		},
		Categories: map[string][]model.Result{},
		Summary: model.Summary{
			Resources: map[string]model.USEMetric{},
		},
	}

	// "-" means stdout; this should not error
	// Redirect stdout to verify
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	err := WriteJSON(report, "-")

	w.Close()
	os.Stdout = oldStdout

	if err != nil {
		t.Fatalf("WriteJSON to stdout: %v", err)
	}

	buf := make([]byte, 4096)
	n, _ := r.Read(buf)
	if n == 0 {
		t.Error("no output to stdout")
	}
}

func containsStr(s, substr string) bool {
	for i := 0; i+len(substr) <= len(s); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
