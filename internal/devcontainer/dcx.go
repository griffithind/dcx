package devcontainer

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/util"
	"github.com/tidwall/jsonc"
)

// dcxConfigPath is the standard location for dcx.json
const dcxConfigPath = ".devcontainer/dcx.json"

// DcxConfig represents the dcx-specific configuration from dcx.json.
type DcxConfig struct {
	// Name is the project name, used for container naming and SSH host.
	// Replaces the hash-based env_key when set.
	Name string `json:"name,omitempty"`

	// Up contains default options for the 'up' command.
	Up DcxUpOptions `json:"up,omitempty"`

	// Shortcuts defines command aliases for the 'run' command.
	Shortcuts map[string]Shortcut `json:"shortcuts,omitempty"`
}

// DcxUpOptions contains default options for the 'up' command.
type DcxUpOptions struct {
	// SSH enables SSH server access when true.
	SSH bool `json:"ssh,omitempty"`

	// NoAgent disables SSH agent forwarding when true.
	NoAgent bool `json:"noAgent,omitempty"`
}

// Shortcut represents a command shortcut configuration.
// Can be either a simple string (the command) or a complex object.
type Shortcut struct {
	// Command is the simple command string (mutually exclusive with Prefix)
	Command string

	// Prefix is the command prefix for passthrough mode
	Prefix string

	// PassArgs indicates whether to pass remaining args to the command
	PassArgs bool

	// Description provides help text for the shortcut
	Description string
}

// UnmarshalJSON handles both simple string and object shortcut formats.
// Examples:
//   - "bin/jobs --skip-recurring"           -> Shortcut{Command: "bin/jobs --skip-recurring"}
//   - {"prefix": "rails", "passArgs": true} -> Shortcut{Prefix: "rails", PassArgs: true}
func (s *Shortcut) UnmarshalJSON(data []byte) error {
	// Try string first
	var str string
	if err := json.Unmarshal(data, &str); err == nil {
		s.Command = str
		return nil
	}

	// Try object format
	type shortcutAlias struct {
		Command     string `json:"command,omitempty"`
		Prefix      string `json:"prefix,omitempty"`
		PassArgs    bool   `json:"passArgs,omitempty"`
		Description string `json:"description,omitempty"`
	}
	var obj shortcutAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return fmt.Errorf("invalid shortcut format: %w", err)
	}

	s.Command = obj.Command
	s.Prefix = obj.Prefix
	s.PassArgs = obj.PassArgs
	s.Description = obj.Description
	return nil
}

// MarshalJSON implements json.Marshaler.
func (s Shortcut) MarshalJSON() ([]byte, error) {
	// If it's a simple command with no other fields, marshal as string
	if s.Command != "" && s.Prefix == "" && !s.PassArgs && s.Description == "" {
		return json.Marshal(s.Command)
	}

	// Otherwise marshal as object
	type shortcutAlias struct {
		Command     string `json:"command,omitempty"`
		Prefix      string `json:"prefix,omitempty"`
		PassArgs    bool   `json:"passArgs,omitempty"`
		Description string `json:"description,omitempty"`
	}
	return json.Marshal(shortcutAlias{
		Command:     s.Command,
		Prefix:      s.Prefix,
		PassArgs:    s.PassArgs,
		Description: s.Description,
	})
}

// LoadDcxConfig loads the dcx-specific configuration if present.
// Returns nil (not an error) if the file doesn't exist.
func LoadDcxConfig(workspacePath string) (*DcxConfig, error) {
	configPath := filepath.Join(workspacePath, dcxConfigPath)

	if !util.IsFile(configPath) {
		return nil, nil // Not an error - dcx.json is optional
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return nil, fmt.Errorf("failed to read dcx.json: %w", err)
	}

	// Strip comments using jsonc (same as devcontainer.json)
	stripped := jsonc.ToJSON(data)

	var cfg DcxConfig
	if err := json.Unmarshal(stripped, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse dcx.json: %w", err)
	}

	return &cfg, nil
}
