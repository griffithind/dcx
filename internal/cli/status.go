package cli

import (
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/ssh/host"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var statusDetailed bool

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show devcontainer status",
	Long: `Show the current state of the devcontainer environment.

This command queries Docker for containers managed by dcx and displays
their current state (ABSENT, CREATED, RUNNING, STALE, or BROKEN).

Use --detailed for comprehensive container and configuration information.

This is an offline-safe command that does not require network access.`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusDetailed, "detailed", "d", false, "show detailed environment information")
}

func runStatus(cmd *cobra.Command, args []string) error {
	// Initialize CLI context
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	ids := cliCtx.Identifiers

	// DCX customizations will be loaded later with cfg
	var dcxCustom *devcontainer.DcxCustomizations

	// Try to load config and compute hash for staleness detection
	var currentState state.ContainerState
	var containerInfo *state.ContainerInfo
	var cfg *devcontainer.DevContainerConfig
	var configHash string

	cfg, _, err = devcontainer.Load(cliCtx.WorkspacePath(), cliCtx.ConfigPath())
	if err == nil {
		// Get DCX customizations for shortcuts display
		dcxCustom = devcontainer.GetDcxCustomizations(cfg)
		// Config exists, check for staleness
		if raw := cfg.GetRawJSON(); len(raw) > 0 {
			configHash = devcontainer.ComputeSimpleHash(raw)
		}
		if configHash != "" {
			currentState, containerInfo, err = cliCtx.Service.GetStateManager().GetStateWithProjectAndHash(cliCtx.Ctx, ids.ProjectName, ids.WorkspaceID, configHash)
		} else {
			currentState, containerInfo, err = cliCtx.GetState()
		}
	} else {
		// No config or error loading it, just get basic state
		currentState, containerInfo, err = cliCtx.GetState()
	}

	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	// Text output mode
	ui.Printf("%s", ui.FormatLabel("Workspace", ui.Code(cliCtx.WorkspacePath())))
	if ids.ProjectName != "" {
		ui.Printf("%s", ui.FormatLabel("Project", ids.ProjectName))
	}
	ui.Printf("%s", ui.FormatLabel("Workspace ID", ids.WorkspaceID))
	ui.Printf("%s", ui.FormatLabel("State", ui.StateColor(string(currentState))))

	// Show SSH status
	if containerInfo != nil && host.HasSSHConfig(containerInfo.Name) {
		ui.Printf("%s", ui.FormatLabel("SSH", ui.Code(fmt.Sprintf("ssh %s", ids.SSHHost))))
	} else if currentState != state.StateAbsent {
		ui.Printf("%s", ui.FormatLabel("SSH", ui.Dim("not configured (run 'dcx up' to configure)")))
	}

	// Show shortcuts count
	if dcxCustom != nil && len(dcxCustom.Shortcuts) > 0 {
		ui.Printf("%s", ui.FormatLabel("Shortcuts", fmt.Sprintf("%d defined (use 'dcx run --list' to view)", len(dcxCustom.Shortcuts))))
	}

	// Container details
	if containerInfo != nil {
		ui.Println("")
		ui.Println(ui.Bold("Primary Container"))

		ui.Printf("  %s", ui.FormatLabel("ID", containerInfo.ID[:12]))
		ui.Printf("  %s", ui.FormatLabel("Name", containerInfo.Name))
		ui.Printf("  %s", ui.FormatLabel("Status", containerInfo.Status))
		if containerInfo.ConfigHash != "" {
			ui.Printf("  %s", ui.FormatLabel("Config", containerInfo.ConfigHash[:12]))
		}

		// Detailed mode: show more container info
		if statusDetailed {
			fullContainer, inspectErr := cliCtx.DockerClient.InspectContainer(cliCtx.Ctx, containerInfo.ID)
			if inspectErr == nil {
				ui.Println("")
				ui.Println(ui.Bold("Container Details"))
				ui.Printf("  %s", ui.FormatLabel("Image", fullContainer.Image))
				if fullContainer.StartedAt != "" {
					ui.Printf("  %s", ui.FormatLabel("Started", fullContainer.StartedAt))
				}
				ui.Printf("  %s", ui.FormatLabel("Running", fmt.Sprintf("%t", containerInfo.Running)))
			}
		}
	}

	// Detailed mode: show configuration
	if statusDetailed && cfg != nil {
		ui.Println("")
		ui.Println(ui.Bold("Configuration"))
		if cfg.Image != "" {
			ui.Printf("  %s", ui.FormatLabel("Image", cfg.Image))
		}
		if cfg.Build != nil {
			if cfg.Build.Dockerfile != "" {
				ui.Printf("  %s", ui.FormatLabel("Dockerfile", cfg.Build.Dockerfile))
			}
			if cfg.Build.Context != "" {
				ui.Printf("  %s", ui.FormatLabel("Build Context", cfg.Build.Context))
			}
		}
		if composeFiles := cfg.GetDockerComposeFiles(); len(composeFiles) > 0 {
			ui.Printf("  %s", ui.FormatLabel("Compose Files", strings.Join(composeFiles, ", ")))
		}
		if cfg.Service != "" {
			ui.Printf("  %s", ui.FormatLabel("Service", cfg.Service))
		}
		if cfg.RemoteUser != "" {
			ui.Printf("  %s", ui.FormatLabel("Remote User", cfg.RemoteUser))
		}
		if cfg.WorkspaceFolder != "" {
			ui.Printf("  %s", ui.FormatLabel("Workspace Folder", cfg.WorkspaceFolder))
		}
		if len(cfg.Features) > 0 {
			featureList := make([]string, 0, len(cfg.Features))
			for f := range cfg.Features {
				featureList = append(featureList, f)
			}
			ui.Printf("  %s", ui.FormatLabel("Features", strings.Join(featureList, ", ")))
		}
		if configHash != "" {
			ui.Printf("  %s", ui.FormatLabel("Config Hash", configHash[:12]))
		}
	}

	// Detailed mode: show labels
	if statusDetailed && containerInfo != nil {
		labelMap := containerInfo.Labels.ToMap()
		dcxLabels := make(map[string]string)
		for k, v := range labelMap {
			if strings.HasPrefix(k, "dcx.") {
				dcxLabels[k] = v
			}
		}
		if len(dcxLabels) > 0 {
			ui.Println("")
			ui.Println(ui.Bold("Labels"))
			for k, v := range dcxLabels {
				ui.Printf("  %s: %s", ui.Dim(k), v)
			}
		}
	}

	return nil
}
