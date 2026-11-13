package devcontainer

import (
	"encoding/json"
	"fmt"
	"strings"
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

// LifecycleCommand handles all forms of lifecycle commands:
// - string: "echo hello"
// - []string: ["echo", "hello"]
// - map[string]string|[]string: {"task1": "cmd1", "task2": ["cmd2", "arg"]}
type LifecycleCommand struct {
	// Commands contains the parsed lifecycle command entries.
	Commands []LifecycleEntry
}

// LifecycleEntry represents a single lifecycle command entry.
type LifecycleEntry struct {
	// Name is the task name (empty for simple string/array commands).
	Name string
	// Command is the command string (for string form).
	Command string
	// Args is the command arguments (for array form).
	Args []string
}

// GetCommands returns all command strings, joining args with spaces if needed.
func (c *LifecycleCommand) GetCommands() []string {
	if c == nil {
		return nil
	}
	result := make([]string, 0, len(c.Commands))
	for _, entry := range c.Commands {
		if entry.Command != "" {
			result = append(result, entry.Command)
		} else if len(entry.Args) > 0 {
			result = append(result, strings.Join(entry.Args, " "))
		}
	}
	return result
}

// UnmarshalJSON handles string, []string, and map forms.
func (c *LifecycleCommand) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		c.Commands = []LifecycleEntry{{Command: str}}
		return nil
	}

	// Try array of strings
	var arr []string
	if err := json.Unmarshal(data, &arr); err == nil {
		c.Commands = []LifecycleEntry{{Args: arr}}
		return nil
	}

	// Try map (parallel commands)
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err == nil {
		c.Commands = make([]LifecycleEntry, 0, len(m))
		for name, cmd := range m {
			entry := LifecycleEntry{Name: name}
			switch v := cmd.(type) {
			case string:
				entry.Command = v
			case []interface{}:
				entry.Args = make([]string, 0, len(v))
				for _, item := range v {
					if s, ok := item.(string); ok {
						entry.Args = append(entry.Args, s)
					}
				}
			}
			c.Commands = append(c.Commands, entry)
		}
		return nil
	}

	return fmt.Errorf("LifecycleCommand: expected string, []string, or map, got: %s", string(data))
}

// MarshalJSON serializes the lifecycle command back to JSON.
func (c LifecycleCommand) MarshalJSON() ([]byte, error) {
	if len(c.Commands) == 0 {
		return json.Marshal(nil)
	}
	if len(c.Commands) == 1 && c.Commands[0].Name == "" {
		// Single command without name
		entry := c.Commands[0]
		if entry.Command != "" {
			return json.Marshal(entry.Command)
		}
		return json.Marshal(entry.Args)
	}
	// Multiple commands or named commands - use map
	m := make(map[string]interface{}, len(c.Commands))
	for _, entry := range c.Commands {
		name := entry.Name
		if name == "" {
			name = "default"
		}
		if entry.Command != "" {
			m[name] = entry.Command
		} else {
			m[name] = entry.Args
		}
	}
	return json.Marshal(m)
}
