package cli

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/service"
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
	Long: `Show the resolved devcontainer.json configuration.

By default, shows the configuration after variable substitution.
Use --raw to show the original configuration without substitution.

Examples:
  dcx config                # Show resolved config
  dcx config --raw          # Show original config
  dcx config --validate     # Only validate config (no output)`,
	RunE: runConfig,
}

// ConfigOutput represents the output of the config command.
type ConfigOutput struct {
	ConfigPath      string                           `json:"config_path"`
	WorkspaceID     string                           `json:"workspaceID"`
	ConfigHash      string                           `json:"config_hash,omitempty"`
	WorkspaceFolder string                           `json:"workspace_folder"`
	PlanType        string                           `json:"plan_type"`
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

	// Get identifiers from service
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)
	defer svc.Close()

	ids, err := svc.GetIdentifiers()
	if err != nil {
		return fmt.Errorf("failed to get identifiers: %w", err)
	}

	// Use simple hash of raw JSON to match workspace builder
	var configHash string
	if raw := cfg.GetRawJSON(); len(raw) > 0 {
		configHash = devcontainer.ComputeSimpleHash(raw)
	}

	// Determine plan type
	planType := "unknown"
	if cfg.IsComposePlan() {
		planType = "compose"
	} else if cfg.IsSinglePlan() {
		planType = "single"
	}

	// Determine workspace folder
	wsFolder := devcontainer.DetermineContainerWorkspaceFolder(cfg, workspacePath)

	// Build output
	output := ConfigOutput{
		ConfigPath:      cfgPath,
		WorkspaceID:     ids.WorkspaceID,
		ConfigHash:      configHash,
		WorkspaceFolder: wsFolder,
		PlanType:        planType,
		Config:          cfg,
	}

	// Output as JSON
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func init() {
	configCmd.Flags().BoolVar(&configValidateOnly, "validate", false, "only validate configuration (no output)")
	configCmd.Flags().BoolVar(&configShowRaw, "raw", false, "show original config without variable substitution")
	configCmd.GroupID = "utilities"
	rootCmd.AddCommand(configCmd)
}
