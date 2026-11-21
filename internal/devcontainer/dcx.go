package devcontainer

import (
	"encoding/json"
)

// DcxCustomizations represents DCX-specific settings from customizations.dcx
// in devcontainer.json.
type DcxCustomizations struct {
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

// dcxCustomizationsKey is dcx's key under "customizations".
const dcxCustomizationsKey = "dcx"

// DcxCustomizations extracts DCX customizations from a DevContainerConfig.
// Returns nil if no customizations.dcx section exists.
func (cfg *DevContainerConfig) DcxCustomizations() *DcxCustomizations {
	if cfg == nil || cfg.Customizations == nil {
		return nil
	}
	dcxRaw, ok := cfg.Customizations[dcxCustomizationsKey]
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
