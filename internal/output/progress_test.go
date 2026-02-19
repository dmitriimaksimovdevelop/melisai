package output

import (
	"bytes"
	"io"
	"os"
	"strings"
	"testing"
)

func captureStderr(fn func()) string {
	old := os.Stderr
	r, w, _ := os.Pipe()
	os.Stderr = w

	fn()

	w.Close()
	os.Stderr = old

	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	return buf.String()
}

func TestProgressLogEnabled(t *testing.T) {
	out := captureStderr(func() {
		p := NewProgress(true)
		p.Log("hello %s", "world")
	})

	if !strings.Contains(out, "hello world") {
		t.Errorf("expected 'hello world' in output, got %q", out)
	}
}

func TestProgressLogDisabled(t *testing.T) {
	out := captureStderr(func() {
		p := NewProgress(false)
		p.Log("should not appear")
	})

	if out != "" {
		t.Errorf("quiet mode should produce no output, got %q", out)
	}
}

func TestVerboseProgressDebug(t *testing.T) {
	out := captureStderr(func() {
		p := NewVerboseProgress(true, true)
		p.Debug("debug info %d", 42)
	})

	if !strings.Contains(out, "DEBUG: debug info 42") {
		t.Errorf("expected 'DEBUG: debug info 42' in output, got %q", out)
	}
}

func TestVerboseProgressDebugDisabledWhenNotVerbose(t *testing.T) {
	out := captureStderr(func() {
		p := NewVerboseProgress(true, false)
		p.Debug("should not appear")
	})

	if strings.Contains(out, "should not appear") {
		t.Errorf("debug should not appear when verbose=false, got %q", out)
	}
}

func TestVerboseImpliesEnabled(t *testing.T) {
	out := captureStderr(func() {
		p := NewVerboseProgress(false, true) // enabled=false but verbose=true
		p.Log("visible despite enabled=false")
	})

	if !strings.Contains(out, "visible despite enabled=false") {
		t.Errorf("verbose should override enabled=false, got %q", out)
	}
}
