package cli

import (
	"fmt"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	configValidateOnly bool
	configShowRaw      bool
)

var configCmd = &cobra.Command{
	Use:   "config",
	Short: "Show devcontainer configuration",
	Long: `Show the resolved devcontainer.json configuration as JSON (always; there
is no --json flag).

By default, shows devcontainer.json after variable substitution (the base
image's metadata is not merged in; 'dcx status' shows the resolved plan).
Use --raw to show it as written.`,
	Example: `  dcx config                # Show the resolved config
  dcx config --raw          # Show the config as written
  dcx config --validate     # Only validate the config (no output)`,
	RunE: runConfig,
}

// ConfigOutput represents the output of the config command.
type ConfigOutput struct {
	ConfigPath      string                           `json:"configPath"`
	WorkspaceID     string                           `json:"workspaceID"`
	ConfigHash      string                           `json:"configHash,omitempty"`
	WorkspaceFolder string                           `json:"workspaceFolder"`
	PlanType        string                           `json:"planType"`
	Config          *devcontainer.DevContainerConfig `json:"config"`
}

func runConfig(cmd *cobra.Command, args []string) error {
	// Load and parse configuration
	cfg, cfgPath, err := devcontainer.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Validate configuration
	if err := devcontainer.Validate(cfg); err != nil {
		return fmt.Errorf("configuration validation failed: %w", err)
	}

	// If validate-only, we're done
	if configValidateOnly {
		ui.Success("Configuration is valid.")
		return nil
	}

	// If --raw, reload without substitution
	if configShowRaw {
		cfg, err = devcontainer.ParseFile(cfgPath)
		if err != nil {
			return fmt.Errorf("failed to parse configuration: %w", err)
		}
	}

	// Progress output is discarded so it cannot corrupt the printed config.
	svc := newService(newEngine(), report.Discard)

	ids := svc.Identity()

	// Resolve the configuration so the reported ConfigHash matches what
	// `dcx up` stamps into the container label — ComputeConfigHash covers
	// devcontainer.json + Dockerfile + compose files + features, not just
	// the raw devcontainer.json.
	//
	// If the resolve fails (unresolvable features offline, invalid plan),
	// we fall back to leaving the hash empty — matching the previous
	// behavior of silently omitting it on error rather than breaking
	// `dcx config` output for users who just want to inspect their config.
	var configHash string
	if resolved, rerr := svc.Load(cmd.Context()); rerr == nil {
		configHash = resolved.ConfigHash
	}

	// Determine workspace folder
	wsFolder := devcontainer.DetermineContainerWorkspaceFolder(cfg, workspacePath)

	// Build output
	output := ConfigOutput{
		ConfigPath:      cfgPath,
		WorkspaceID:     ids.WorkspaceID,
		ConfigHash:      configHash,
		WorkspaceFolder: wsFolder,
		PlanType:        string(cfg.PlanType()), // image, dockerfile or compose
		Config:          cfg,
	}

	return printJSON(output)
}

func init() {
	configCmd.Flags().BoolVar(&configValidateOnly, "validate", false, "only validate configuration (no output)")
	configCmd.Flags().BoolVar(&configShowRaw, "raw", false, "show original config without variable substitution")
	configCmd.GroupID = "utilities"
	rootCmd.AddCommand(configCmd)
}
