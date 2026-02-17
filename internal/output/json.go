package output

import (
	"encoding/json"
	"fmt"
	"io"
	"os"

	"github.com/baikal/sysdiag/internal/model"
)

// WriteJSON serializes the report as indented JSON.
// If path is "-" or empty, writes to stdout.
func WriteJSON(report *model.Report, path string) error {
	var w io.Writer = os.Stdout
	if path != "" && path != "-" {
		f, err := os.Create(path)
		if err != nil {
			return fmt.Errorf("create output file: %w", err)
		}
		defer f.Close()
		w = f
	}

	enc := json.NewEncoder(w)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	if err := enc.Encode(report); err != nil {
		return fmt.Errorf("encode JSON: %w", err)
	}
	return nil
}
