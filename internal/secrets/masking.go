package secrets

import (
	"bytes"
	"io"
	"sort"
)

const maskString = "********"

// MaskingWriter wraps an io.Writer and masks secret values in the output.
type MaskingWriter struct {
	inner  io.Writer
	values [][]byte
	mask   []byte
}

// NewMaskingWriter creates a new MaskingWriter that replaces secret values with a mask.
// Values are sorted by length (longest first) to handle overlapping values correctly.
func NewMaskingWriter(w io.Writer, secrets []Secret) *MaskingWriter {
	// Extract values and sort by length (longest first)
	values := make([][]byte, 0, len(secrets))
	for _, s := range secrets {
		if len(s.Value) > 0 {
			values = append(values, s.Value)
		}
	}

	// Sort by length descending to handle overlapping values
	sort.Slice(values, func(i, j int) bool {
		return len(values[i]) > len(values[j])
	})

	return &MaskingWriter{
		inner:  w,
		values: values,
		mask:   []byte(maskString),
	}
}

// Write implements io.Writer. It replaces all secret values with the mask.
func (w *MaskingWriter) Write(p []byte) (n int, err error) {
	if len(w.values) == 0 {
		return w.inner.Write(p)
	}

	// Replace all secret values with mask
	masked := p
	for _, value := range w.values {
		masked = bytes.ReplaceAll(masked, value, w.mask)
	}

	// Write the masked output, but return the original length
	_, err = w.inner.Write(masked)
	if err != nil {
		return 0, err
	}

	// Return original length to satisfy io.Writer contract
	return len(p), nil
}

// MaskString replaces secret values in a string.
func MaskString(s string, secrets []Secret) string {
	if len(secrets) == 0 {
		return s
	}

	result := s
	for _, secret := range secrets {
		if len(secret.Value) > 0 {
			result = bytes.NewBuffer(
				bytes.ReplaceAll([]byte(result), secret.Value, []byte(maskString)),
			).String()
		}
	}
	return result
}
