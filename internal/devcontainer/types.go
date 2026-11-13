package devcontainer

import (
	"encoding/json"
	"fmt"
)

// StringOrSlice handles fields that can be string or []string.
// Used for: dockerComposeFile, runServices, etc.
type StringOrSlice []string

// UnmarshalJSON handles both string and []string forms.
func (s *StringOrSlice) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		*s = []string{str}
		return nil
	}

	// Try array
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return fmt.Errorf("StringOrSlice: expected string or []string, got: %s", string(data))
	}
	*s = arr
	return nil
}

// MarshalJSON serializes back to JSON (single string if length 1).
func (s StringOrSlice) MarshalJSON() ([]byte, error) {
	if len(s) == 1 {
		return json.Marshal(s[0])
	}
	return json.Marshal([]string(s))
}
