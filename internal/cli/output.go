package cli

import (
	"encoding/json"
	"os"
)

// printJSON writes v as indented JSON to stdout. It bypasses the ui
// package on purpose: data a command was asked for is printed even with
// --quiet, and never coloured.
func printJSON(v any) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	enc.SetEscapeHTML(false)
	return enc.Encode(v)
}
