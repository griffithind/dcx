package devcontainer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strconv"
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

// StringOrArray is a property that may be a string or an array of strings
// (build.cacheFrom). Unlike StringOrSlice it is written back in the form it
// was read, so the config hash of an existing config does not change.
type StringOrArray struct {
	Values []string
	single bool
}

// UnmarshalJSON accepts a string or an array of strings.
func (s *StringOrArray) UnmarshalJSON(data []byte) error {
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Values, s.single = []string{str}, true
		return nil
	}
	var arr []string
	if err := json.Unmarshal(data, &arr); err != nil {
		return fmt.Errorf("expected a string or an array of strings, got: %s", string(data))
	}
	s.Values, s.single = arr, false
	return nil
}

// MarshalJSON writes the form that was read.
func (s StringOrArray) MarshalJSON() ([]byte, error) {
	if s.single && len(s.Values) == 1 {
		return json.Marshal(s.Values[0])
	}
	return json.Marshal(s.Values)
}

// List returns the values (nil for a nil receiver).
func (s *StringOrArray) List() []string {
	if s == nil {
		return nil
	}
	return s.Values
}

// StringMap is a map of strings whose values may also be written as
// numbers or booleans ("PORT": 3000), as the reference implementation
// passes them through as text. Strings marshal back unchanged.
type StringMap map[string]string

// UnmarshalJSON accepts string, number and boolean values.
func (m *StringMap) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*m = nil
		return nil
	}
	out := make(StringMap, len(raw))
	for k, v := range raw {
		s, err := scalarString(v)
		if err != nil {
			return fmt.Errorf("%q: %w", k, err)
		}
		if s != nil {
			out[k] = *s
		}
	}
	*m = out
	return nil
}

// NullableStringMap is remoteEnv: string, number or boolean values, and
// null to unset a variable.
type NullableStringMap map[string]*string

// UnmarshalJSON accepts string, number, boolean and null values.
func (m *NullableStringMap) UnmarshalJSON(data []byte) error {
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	if raw == nil {
		*m = nil
		return nil
	}
	out := make(NullableStringMap, len(raw))
	for k, v := range raw {
		s, err := scalarString(v)
		if err != nil {
			return fmt.Errorf("%q: %w", k, err)
		}
		out[k] = s
	}
	*m = out
	return nil
}

// scalarString renders a JSON string, number or boolean as text; null is
// nil.
func scalarString(v json.RawMessage) (*string, error) {
	var x any
	dec := json.NewDecoder(bytes.NewReader(v))
	dec.UseNumber()
	if err := dec.Decode(&x); err != nil {
		return nil, err
	}
	var s string
	switch t := x.(type) {
	case nil:
		return nil, nil
	case string:
		s = t
	case json.Number:
		s = t.String()
	case bool:
		s = strconv.FormatBool(t)
	default:
		return nil, fmt.Errorf("expected a string, number or boolean, got: %s", string(v))
	}
	return &s, nil
}
