package devcontainer

import (
	"encoding/json"
	"fmt"
)

// DcxCustomizations represents DCX-specific settings from customizations.dcx
// in devcontainer.json. This replaces the old separate dcx.json file.
type DcxCustomizations struct {
	// Shortcuts defines command aliases for the 'run' command.
	Shortcuts map[string]Shortcut `json:"shortcuts,omitempty"`

	// Secrets defines runtime secrets to be mounted at /run/secrets/<name>.
	// Commands are executed on the host to fetch secret values.
	Secrets map[string]SecretConfig `json:"secrets,omitempty"`

	// BuildSecrets defines build-time secrets for Docker BuildKit.
	// These are only available during docker build via --mount=type=secret.
	BuildSecrets map[string]SecretConfig `json:"buildSecrets,omitempty"`
}

// SecretConfig is a shell command to execute on the host to fetch a secret value.
// The command's stdout is captured as the secret value.
// Examples:
//   - "op read op://vault/item"
//   - "echo $MY_ENV_VAR"
//   - "cat /path/to/secret"
type SecretConfig string

// Shortcut represents a command shortcut configuration.
// Can be either a simple string (the command) or a complex object.
type Shortcut struct {
	// Command is the simple command string (mutually exclusive with Prefix)
	Command string `json:"command,omitempty"`

	// Prefix is the command prefix for passthrough mode
	Prefix string `json:"prefix,omitempty"`

	// PassArgs indicates whether to pass remaining args to the command
	PassArgs bool `json:"passArgs,omitempty"`

	// Description provides help text for the shortcut
	Description string `json:"description,omitempty"`
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

	// Otherwise marshal as object using type alias to avoid recursion
	type Alias Shortcut
	return json.Marshal((*Alias)(&s))
}

// GetDcxCustomizations extracts DCX customizations from a DevContainerConfig.
// Returns nil if no customizations.dcx section exists.
func GetDcxCustomizations(cfg *DevContainerConfig) *DcxCustomizations {
	if cfg == nil || cfg.Customizations == nil {
		return nil
	}
	dcxRaw, ok := cfg.Customizations["dcx"]
	if !ok {
		return nil
	}
	// Marshal and unmarshal to parse the structure
	data, err := json.Marshal(dcxRaw)
	if err != nil {
		return nil
	}
	var dcx DcxCustomizations
	if err := json.Unmarshal(data, &dcx); err != nil {
		return nil
	}
	return &dcx
}
