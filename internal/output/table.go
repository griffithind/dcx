package output

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"
)

// Table provides consistent table formatting.
type Table struct {
	writer    *tabwriter.Writer
	output    io.Writer
	headers   []string
	rows      [][]string
	minWidth  int
	tabWidth  int
	padding   int
	separator string
}

// TableOption configures table behavior.
type TableOption func(*Table)

// WithMinWidth sets the minimum column width.
func WithMinWidth(width int) TableOption {
	return func(t *Table) {
		t.minWidth = width
	}
}

// WithPadding sets the column padding.
func WithPadding(padding int) TableOption {
	return func(t *Table) {
		t.padding = padding
	}
}

// WithSeparator sets the column separator.
func WithSeparator(sep string) TableOption {
	return func(t *Table) {
		t.separator = sep
	}
}

// NewTable creates a new table with the given headers.
func NewTable(out io.Writer, headers []string, opts ...TableOption) *Table {
	t := &Table{
		output:    out,
		headers:   headers,
		rows:      make([][]string, 0),
		minWidth:  0,
		tabWidth:  8,
		padding:   2,
		separator: "\t",
	}

	for _, opt := range opts {
		opt(t)
	}

	t.writer = tabwriter.NewWriter(out, t.minWidth, t.tabWidth, t.padding, ' ', 0)
	return t
}

// AddRow adds a row to the table.
func (t *Table) AddRow(cells ...string) {
	t.rows = append(t.rows, cells)
}

// Render writes the table to the output.
func (t *Table) Render() error {
	c := Color()

	// Print headers
	if len(t.headers) > 0 {
		coloredHeaders := make([]string, len(t.headers))
		for i, h := range t.headers {
			coloredHeaders[i] = c.Header(strings.ToUpper(h))
		}
		fmt.Fprintln(t.writer, strings.Join(coloredHeaders, t.separator))
	}

	// Print rows
	for _, row := range t.rows {
		fmt.Fprintln(t.writer, strings.Join(row, t.separator))
	}

	return t.writer.Flush()
}

// RenderWithDivider writes the table with a divider after headers.
func (t *Table) RenderWithDivider() error {
	c := Color()

	// Print headers
	if len(t.headers) > 0 {
		coloredHeaders := make([]string, len(t.headers))
		for i, h := range t.headers {
			coloredHeaders[i] = c.Header(strings.ToUpper(h))
		}
		fmt.Fprintln(t.writer, strings.Join(coloredHeaders, t.separator))

		// Print divider
		dividers := make([]string, len(t.headers))
		for i, h := range t.headers {
			dividers[i] = strings.Repeat(Symbols.Separator, len(h))
		}
		fmt.Fprintln(t.writer, c.Dim(strings.Join(dividers, t.separator)))
	}

	// Print rows
	for _, row := range t.rows {
		fmt.Fprintln(t.writer, strings.Join(row, t.separator))
	}

	return t.writer.Flush()
}

// RowCount returns the number of rows.
func (t *Table) RowCount() int {
	return len(t.rows)
}

// Clear removes all rows from the table.
func (t *Table) Clear() {
	t.rows = make([][]string, 0)
}

// KeyValueTable provides a simple key-value display format.
type KeyValueTable struct {
	output  io.Writer
	writer  *tabwriter.Writer
	entries []struct {
		key   string
		value string
	}
}

// NewKeyValueTable creates a new key-value table.
func NewKeyValueTable(out io.Writer) *KeyValueTable {
	return &KeyValueTable{
		output: out,
		writer: tabwriter.NewWriter(out, 0, 8, 2, ' ', 0),
	}
}

// Add adds a key-value pair.
func (t *KeyValueTable) Add(key, value string) {
	t.entries = append(t.entries, struct {
		key   string
		value string
	}{key, value})
}

// AddIf adds a key-value pair only if the value is non-empty.
func (t *KeyValueTable) AddIf(key, value string) {
	if value != "" {
		t.Add(key, value)
	}
}

// Render writes the key-value table.
func (t *KeyValueTable) Render() error {
	c := Color()
	for _, e := range t.entries {
		fmt.Fprintf(t.writer, "%s\t%s\n", c.Label(e.key+":"), e.value)
	}
	return t.writer.Flush()
}

// Clear removes all entries.
func (t *KeyValueTable) Clear() {
	t.entries = nil
}
