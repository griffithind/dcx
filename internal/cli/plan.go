package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show what would be done without executing",
	Long: `Preview the execution plan for starting the devcontainer environment.

This command analyzes your devcontainer configuration and shows:
- Current container state (if any)
- What action would be taken (create, recreate, start, none)
- Full configuration including features, mounts, environment, ports
- Security options and lifecycle hooks

No changes are made to your environment.

Examples:
  dcx plan              # Show execution plan for current directory
  dcx plan -w /path     # Show plan for specific workspace`,
	RunE: runPlan,
}

func init() {
	planCmd.GroupID = "info"
	rootCmd.AddCommand(planCmd)
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := container.NewDockerClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer func() { _ = dockerClient.Close() }()

	// Create service and get plan
	svc := service.NewDevContainerService(dockerClient, workspacePath, configPath, verbose)
	defer svc.Close()

	plan, err := svc.Plan(ctx, service.PlanOptions{})
	if err != nil {
		return err
	}

	// Display the plan
	displayPlan(plan)

	return nil
}

func displayPlan(plan *service.PlanResult) {
	resolved := plan.Resolved
	cfg := resolved.RawConfig

	ui.Println(ui.Bold("Devcontainer Execution Plan"))
	ui.Println(ui.Dim("==========================="))
	ui.Println("")

	// Workspace info
	ui.Printf("%s", ui.FormatLabel("Workspace", resolved.Name))
	ui.Printf("%s", ui.FormatLabel("Path", ui.Code(resolved.LocalRoot)))
	if resolved.Name != "" {
		ui.Printf("%s", ui.FormatLabel("Project", ui.Dim(resolved.Name)))
	}
	ui.Println("")

	// Current state (if container exists)
	if plan.ContainerInfo != nil {
		ui.Println(ui.Bold("Current State"))
		statusStr := "stopped"
		if plan.ContainerInfo.Running {
			statusStr = "running"
		}
		ui.Printf("  %s", ui.FormatLabel("Status", ui.StateColor(statusStr)))
		containerInfo := plan.ContainerInfo.Name
		if len(plan.ContainerInfo.ID) >= 12 {
			containerInfo += " (" + plan.ContainerInfo.ID[:12] + ")"
		}
		ui.Printf("  %s", ui.FormatLabel("Container", containerInfo))
		ui.Println("")
	}

	// Action
	ui.Printf("%s", ui.FormatLabel("Action", colorAction(plan.Action)))
	if plan.Reason != "" {
		ui.Printf("%s", ui.FormatLabel("Reason", plan.Reason))
	}
	ui.Println("")

	// Changes detected
	if len(plan.Changes) > 0 {
		ui.Println(ui.Bold("Changes Detected"))
		for _, change := range plan.Changes {
			ui.Printf("  %s %s", ui.Symbols.Bullet, change)
		}
		ui.Println("")
	}

	// Configuration section
	ui.Println(ui.Bold("Configuration"))
	planType := "image"
	if cfg.Build != nil {
		planType = "dockerfile"
	} else if cfg.DockerComposeFile != nil {
		planType = "compose"
	}
	ui.Printf("  %s", ui.FormatLabel("Type", planType))
	if cfg.Image != "" {
		ui.Printf("  %s", ui.FormatLabel("Image", cfg.Image))
	}
	if cfg.RemoteUser != "" {
		ui.Printf("  %s", ui.FormatLabel("User", cfg.RemoteUser))
	}
	if cfg.WorkspaceFolder != "" {
		ui.Printf("  %s", ui.FormatLabel("Workspace Folder", cfg.WorkspaceFolder))
	}

	// Features
	if len(cfg.Features) > 0 {
		ui.Println("")
		ui.Println(ui.Bold("Features"))
		for featureID := range cfg.Features {
			ui.Printf("  %s %s", ui.Symbols.Bullet, featureID)
		}
	}

	// Mounts
	if len(cfg.Mounts) > 0 {
		ui.Println("")
		ui.Println(ui.Bold("Mounts"))
		for _, m := range cfg.Mounts {
			ui.Printf("  %s %s", ui.Symbols.Bullet, formatMount(m))
		}
	}

	// Environment
	if len(cfg.ContainerEnv) > 0 || len(cfg.RemoteEnv) > 0 {
		ui.Println("")
		ui.Println(ui.Bold("Environment"))
		for k, v := range cfg.ContainerEnv {
			ui.Printf("  %s %s=%s", ui.Symbols.Bullet, k, v)
		}
		for k, v := range cfg.RemoteEnv {
			ui.Printf("  %s %s=%s %s", ui.Symbols.Bullet, k, v, ui.Dim("(remote)"))
		}
	}

	// Ports
	if len(cfg.ForwardPorts) > 0 {
		ui.Println("")
		ui.Println(ui.Bold("Ports"))
		for _, p := range cfg.ForwardPorts {
			ui.Printf("  %s %v", ui.Symbols.Bullet, p)
		}
	}

	// Security options
	privileged := cfg.Privileged != nil && *cfg.Privileged
	if privileged || len(cfg.CapAdd) > 0 || len(cfg.SecurityOpt) > 0 {
		ui.Println("")
		ui.Println(ui.Bold("Security"))
		if privileged {
			ui.Printf("  %s", ui.FormatLabel("Privileged", "true"))
		}
		if len(cfg.CapAdd) > 0 {
			ui.Printf("  %s", ui.FormatLabel("Cap Add", strings.Join(cfg.CapAdd, ", ")))
		}
		if len(cfg.SecurityOpt) > 0 {
			ui.Printf("  %s", ui.FormatLabel("Security Opt", strings.Join(cfg.SecurityOpt, ", ")))
		}
	}

	// Lifecycle hooks
	hasHooks := cfg.OnCreateCommand != nil || cfg.PostCreateCommand != nil ||
		cfg.PostStartCommand != nil || cfg.PostAttachCommand != nil ||
		cfg.InitializeCommand != nil || cfg.UpdateContentCommand != nil
	if hasHooks {
		ui.Println("")
		ui.Println(ui.Bold("Lifecycle Hooks"))
		if cfg.InitializeCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("initializeCommand", formatCommand(cfg.InitializeCommand)))
		}
		if cfg.OnCreateCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("onCreateCommand", formatCommand(cfg.OnCreateCommand)))
		}
		if cfg.UpdateContentCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("updateContentCommand", formatCommand(cfg.UpdateContentCommand)))
		}
		if cfg.PostCreateCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("postCreateCommand", formatCommand(cfg.PostCreateCommand)))
		}
		if cfg.PostStartCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("postStartCommand", formatCommand(cfg.PostStartCommand)))
		}
		if cfg.PostAttachCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("postAttachCommand", formatCommand(cfg.PostAttachCommand)))
		}
	}

	// Other settings
	hasOtherSettings := cfg.Init != nil || cfg.OverrideCommand != nil || cfg.ShutdownAction != ""
	if hasOtherSettings {
		ui.Println("")
		ui.Println(ui.Bold("Settings"))
		if cfg.Init != nil {
			ui.Printf("  %s", ui.FormatLabel("Init Process", fmt.Sprintf("%t", *cfg.Init)))
		}
		if cfg.OverrideCommand != nil {
			ui.Printf("  %s", ui.FormatLabel("Override Command", fmt.Sprintf("%t", *cfg.OverrideCommand)))
		}
		if cfg.ShutdownAction != "" {
			ui.Printf("  %s", ui.FormatLabel("Shutdown Action", cfg.ShutdownAction))
		}
	}

	// Hashes (verbose only)
	if ui.IsVerbose() && resolved.Hashes != nil {
		ui.Println("")
		ui.Println(ui.Bold("Configuration Hash"))
		ui.Printf("  %s", ui.FormatLabel("Config", resolved.Hashes.Config))
	}

	ui.Println("")
	if plan.Action != state.PlanActionNone {
		ui.Println(ui.Dim("Run 'dcx up' to execute this plan."))
	}
}

func colorAction(action state.PlanAction) string {
	switch action {
	case state.PlanActionCreate:
		return pterm.FgGreen.Sprint(string(action))
	case state.PlanActionRecreate:
		return pterm.FgYellow.Sprint(string(action))
	case state.PlanActionRebuild:
		return pterm.FgRed.Sprint(string(action))
	case state.PlanActionStart:
		return pterm.FgCyan.Sprint(string(action))
	case state.PlanActionNone:
		return pterm.FgGreen.Sprint("none (up to date)")
	default:
		return string(action)
	}
}

func formatCommand(cmd interface{}) string {
	switch v := cmd.(type) {
	case string:
		return v
	case []interface{}:
		parts := make([]string, len(v))
		for i, p := range v {
			parts[i] = fmt.Sprintf("%v", p)
		}
		return strings.Join(parts, " ")
	case map[string]interface{}:
		// Parallel commands
		var cmds []string
		for name := range v {
			cmds = append(cmds, name)
		}
		return fmt.Sprintf("{%s}", strings.Join(cmds, ", "))
	default:
		return fmt.Sprintf("%v", cmd)
	}
}

func formatMount(mount interface{}) string {
	switch v := mount.(type) {
	case string:
		return v
	case map[string]interface{}:
		source := fmt.Sprintf("%v", v["source"])
		target := fmt.Sprintf("%v", v["target"])
		mountType := "bind"
		if t, ok := v["type"]; ok {
			mountType = fmt.Sprintf("%v", t)
		}
		return fmt.Sprintf("%s → %s (%s)", source, target, mountType)
	default:
		return fmt.Sprintf("%v", mount)
	}
}
