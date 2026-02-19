package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/dmitriimaksimovdevelop/melisai/internal/model"
	"github.com/mark3labs/mcp-go/mcp"
)

// --- getArgs / stringArg helpers ---

func TestGetArgs_NilArguments(t *testing.T) {
	req := mcp.CallToolRequest{}
	args := getArgs(req)
	if args == nil {
		t.Fatal("getArgs returned nil, expected empty map")
	}
	if len(args) != 0 {
		t.Fatalf("expected empty map, got %v", args)
	}
}

func TestGetArgs_ValidMap(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"key": "value",
			},
		},
	}
	args := getArgs(req)
	if v, ok := args["key"]; !ok || v != "value" {
		t.Fatalf("expected key=value, got %v", args)
	}
}

func TestGetArgs_WrongType(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: "not a map",
		},
	}
	args := getArgs(req)
	if len(args) != 0 {
		t.Fatalf("expected empty map for wrong type, got %v", args)
	}
}

func TestStringArg_Present(t *testing.T) {
	args := map[string]interface{}{"name": "hello"}
	if got := stringArg(args, "name", "default"); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestStringArg_Missing(t *testing.T) {
	args := map[string]interface{}{}
	if got := stringArg(args, "name", "default"); got != "default" {
		t.Fatalf("expected 'default', got %q", got)
	}
}

func TestStringArg_NilValue(t *testing.T) {
	args := map[string]interface{}{"name": nil}
	if got := stringArg(args, "name", "default"); got != "default" {
		t.Fatalf("expected 'default' for nil value, got %q", got)
	}
}

func TestStringArg_EmptyString(t *testing.T) {
	args := map[string]interface{}{"name": ""}
	if got := stringArg(args, "name", "default"); got != "default" {
		t.Fatalf("expected 'default' for empty string, got %q", got)
	}
}

func TestStringArg_WrongType(t *testing.T) {
	args := map[string]interface{}{"name": 42}
	if got := stringArg(args, "name", "default"); got != "default" {
		t.Fatalf("expected 'default' for wrong type, got %q", got)
	}
}

// --- newTextResult / errResult ---

func TestNewTextResult(t *testing.T) {
	result := newTextResult("hello world")
	if result.IsError {
		t.Fatal("newTextResult should not set IsError")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if tc.Text != "hello world" {
		t.Fatalf("expected 'hello world', got %q", tc.Text)
	}
}

func TestErrResult(t *testing.T) {
	result := errResult("something failed")
	if !result.IsError {
		t.Fatal("errResult should set IsError=true")
	}
	if len(result.Content) != 1 {
		t.Fatalf("expected 1 content item, got %d", len(result.Content))
	}
	tc, ok := result.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if tc.Text != "something failed" {
		t.Fatalf("expected 'something failed', got %q", tc.Text)
	}
}

// --- handleExplainAnomaly ---

func TestHandleExplainAnomaly_ValidID(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"anomaly_id": "cpu_saturation",
			},
		},
	}
	res, err := handleExplainAnomaly(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected success, got IsError")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}
	if !strings.Contains(tc.Text, "CPU Saturation") {
		t.Errorf("expected 'CPU Saturation' in output, got: %s", tc.Text)
	}
}

func TestHandleExplainAnomaly_AllKnownIDs(t *testing.T) {
	for id := range anomalyExplanations {
		req := mcp.CallToolRequest{
			Params: mcp.CallToolParams{
				Arguments: map[string]interface{}{
					"anomaly_id": id,
				},
			},
		}
		res, err := handleExplainAnomaly(context.Background(), req)
		if err != nil {
			t.Fatalf("anomaly %q: unexpected error: %v", id, err)
		}
		if res.IsError {
			t.Fatalf("anomaly %q: expected success, got IsError", id)
		}
		tc, ok := res.Content[0].(mcp.TextContent)
		if !ok {
			t.Fatalf("anomaly %q: expected TextContent", id)
		}
		if tc.Text == "" {
			t.Fatalf("anomaly %q: empty explanation", id)
		}
	}
}

func TestHandleExplainAnomaly_MissingArgument(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{},
		},
	}
	res, err := handleExplainAnomaly(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error: %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for missing anomaly_id")
	}
	tc := res.Content[0].(mcp.TextContent)
	if !strings.Contains(tc.Text, "anomaly_id is required") {
		t.Errorf("expected 'anomaly_id is required', got: %s", tc.Text)
	}
}

func TestHandleExplainAnomaly_NilArguments(t *testing.T) {
	req := mcp.CallToolRequest{}
	res, err := handleExplainAnomaly(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected Go error (should not panic): %v", err)
	}
	if !res.IsError {
		t.Fatal("expected IsError for nil arguments")
	}
}

func TestHandleExplainAnomaly_UnknownID(t *testing.T) {
	req := mcp.CallToolRequest{
		Params: mcp.CallToolParams{
			Arguments: map[string]interface{}{
				"anomaly_id": "unknown_anomaly_xyz",
			},
		},
	}
	res, err := handleExplainAnomaly(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("unknown ID should not be an error, just a fallback message")
	}
	tc := res.Content[0].(mcp.TextContent)
	if !strings.Contains(tc.Text, "No specific explanation") {
		t.Errorf("expected fallback message, got: %s", tc.Text)
	}
	if !strings.Contains(tc.Text, "unknown_anomaly_xyz") {
		t.Errorf("expected anomaly ID in fallback message, got: %s", tc.Text)
	}
}

// --- anomalyExplanations coverage ---

func TestAnomalyExplanations_NotEmpty(t *testing.T) {
	if len(anomalyExplanations) == 0 {
		t.Fatal("anomalyExplanations should not be empty")
	}
	for id, desc := range anomalyExplanations {
		if desc == "" {
			t.Errorf("anomaly %q has empty description", id)
		}
		if !strings.Contains(desc, "**") {
			t.Errorf("anomaly %q should have markdown bold header", id)
		}
		if !strings.Contains(desc, "Recommendations:") && !strings.Contains(desc, "Recommendation") {
			t.Errorf("anomaly %q should include recommendations", id)
		}
	}
}

// --- handleListAnomalies ---

func TestHandleListAnomalies(t *testing.T) {
	req := mcp.CallToolRequest{}
	res, err := handleListAnomalies(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.IsError {
		t.Fatal("expected success, got IsError")
	}
	tc, ok := res.Content[0].(mcp.TextContent)
	if !ok {
		t.Fatal("expected TextContent")
	}

	// Verify it's valid JSON array
	var entries []struct {
		ID       string `json:"id"`
		Category string `json:"category"`
		Brief    string `json:"brief"`
	}
	if err := json.Unmarshal([]byte(tc.Text), &entries); err != nil {
		t.Fatalf("response is not valid JSON: %v\ntext: %s", err, tc.Text)
	}

	// Should have same count as anomalyExplanations
	if len(entries) != len(anomalyExplanations) {
		t.Errorf("expected %d entries, got %d", len(anomalyExplanations), len(entries))
	}

	// Verify all entries have non-empty fields
	for _, e := range entries {
		if e.ID == "" {
			t.Error("entry has empty ID")
		}
		if e.Category == "" {
			t.Errorf("entry %q has empty category", e.ID)
		}
		if e.Brief == "" {
			t.Errorf("entry %q has empty brief", e.ID)
		}
		// Brief should NOT contain markdown ** markers
		if strings.Contains(e.Brief, "**") {
			t.Errorf("entry %q brief still has markdown: %s", e.ID, e.Brief)
		}
	}

	// Verify sorted by category
	for i := 1; i < len(entries); i++ {
		if entries[i].Category < entries[i-1].Category {
			t.Errorf("entries not sorted by category: %s < %s", entries[i].Category, entries[i-1].Category)
		}
	}
}

// --- Anomaly explanations cover all thresholds ---

func TestAnomalyExplanations_CoversAllThresholds(t *testing.T) {
	thresholds := model.DefaultThresholds()
	for _, th := range thresholds {
		if _, ok := anomalyExplanations[th.Metric]; !ok {
			t.Errorf("threshold metric %q has no explanation in anomalyExplanations", th.Metric)
		}
	}
}

// --- Server creation ---

func TestNewServer(t *testing.T) {
	srv := NewServer("1.0.0-test")
	if srv == nil {
		t.Fatal("NewServer returned nil")
	}
	if srv.mcpServer == nil {
		t.Fatal("mcpServer is nil")
	}
}
