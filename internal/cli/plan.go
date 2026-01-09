package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/pipeline"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/pterm/pterm"
	"github.com/spf13/cobra"
)

var planCmd = &cobra.Command{
	Use:   "plan",
	Short: "Show what would be done without executing",
	Long: `Preview the execution plan for starting the devcontainer environment.

This command analyzes your devcontainer configuration and shows:
- What images would be built or pulled
- What containers would be created or recreated
- Why the action is needed (new, stale, etc.)
- What changes were detected

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

	// Find config path
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = findConfigPath(workspacePath)
		if cfgPath == "" {
			return fmt.Errorf("devcontainer.json not found in %s", workspacePath)
		}
	}

	// Parse configuration
	cfg, err := config.ParseFile(cfgPath)
	if err != nil {
		return fmt.Errorf("failed to parse configuration: %w", err)
	}

	// Build workspace
	builder := workspace.NewBuilder(nil)
	ws, err := builder.Build(ctx, workspace.BuildOptions{
		ConfigPath:    cfgPath,
		WorkspaceRoot: workspacePath,
		Config:        cfg,
	})
	if err != nil {
		return fmt.Errorf("failed to build workspace: %w", err)
	}

	// Try to get container state if Docker is available
	var containerState string
	var existingContainer bool
	dockerClient, err := docker.NewClient()
	if err == nil {
		defer dockerClient.Close()
		stateMgr := state.NewManager(dockerClient)
		s, info, _ := stateMgr.GetState(ctx, ws.ID)
		containerState = string(s)
		existingContainer = info != nil

		if info != nil {
			ws.State = &workspace.RuntimeState{
				ContainerID:   info.ID,
				ContainerName: info.Name,
				Status:        workspace.ContainerStatus(s),
			}
		}
	}

	// Build the plan result
	plan := &pipeline.PlanResult{
		Workspace: ws,
	}

	// Determine action
	if !existingContainer {
		plan.Action = pipeline.ActionCreate
		plan.Reason = "no existing container found"
	} else if ws.IsStale() {
		plan.Action = pipeline.ActionRecreate
		plan.Changes = ws.GetStalenessChanges()
		plan.Reason = fmt.Sprintf("configuration changed: %v", plan.Changes)
	} else if containerState == string(state.StateCreated) {
		plan.Action = pipeline.ActionStart
		plan.Reason = "container exists but is stopped"
	} else {
		plan.Action = pipeline.ActionNone
		plan.Reason = "container is up to date and running"
	}

	// Build image plans
	if plan.Action != pipeline.ActionNone && plan.Action != pipeline.ActionStart {
		plan.ImagesToBuild = buildImagePlans(ws)
		plan.ContainersToCreate = []pipeline.ContainerPlan{{
			Name:      ws.Resolved.ServiceName,
			Image:     ws.Resolved.FinalImage,
			IsPrimary: true,
		}}
		if ws.Resolved.Compose != nil {
			plan.ServicesToStart = append([]string{ws.Resolved.Compose.Service}, ws.Resolved.Compose.RunServices...)
		}
	}

	// Text output mode
	ui.Println(ui.Bold("Devcontainer Execution Plan"))
	ui.Println(ui.Dim("==========================="))
	ui.Println("")

	// Workspace info
	ui.Printf("%s %s", ui.FormatLabel("Workspace", ws.Name), "")
	ui.Printf("%s %s", ui.FormatLabel("Path", ui.Code(ws.LocalRoot)), "")
	ui.Printf("%s %s", ui.FormatLabel("ID", ui.Dim(ws.ID)), "")
	ui.Println("")

	// Action
	if plan.Action == pipeline.ActionNone {
		ui.Success("Up to date - no changes needed")
		return nil
	}

	// Show action with color
	var actionStr string
	switch plan.Action {
	case pipeline.ActionCreate:
		actionStr = pterm.FgGreen.Sprint(string(plan.Action))
	case pipeline.ActionRecreate:
		actionStr = pterm.FgYellow.Sprint(string(plan.Action))
	case pipeline.ActionRebuild:
		actionStr = pterm.FgRed.Sprint(string(plan.Action))
	case pipeline.ActionStart:
		actionStr = pterm.FgCyan.Sprint(string(plan.Action))
	default:
		actionStr = string(plan.Action)
	}

	ui.Printf("%s %s", ui.FormatLabel("Action", actionStr), "")
	ui.Printf("%s %s", ui.FormatLabel("Reason", plan.Reason), "")
	ui.Println("")

	// Changes detected
	if len(plan.Changes) > 0 {
		ui.Println(ui.Bold("Changes Detected"))
		for _, change := range plan.Changes {
			ui.Printf("  %s %s", ui.Symbols.Bullet, change)
		}
		ui.Println("")
	}

	// Images to build
	if len(plan.ImagesToBuild) > 0 {
		ui.Println(ui.Bold("Images to Build"))
		headers := []string{"Tag", "Base", "Reason"}
		var rows [][]string
		for _, img := range plan.ImagesToBuild {
			base := img.BaseImage
			if base == "" && img.Dockerfile != "" {
				base = "(Dockerfile)"
			}
			rows = append(rows, []string{img.Tag, base, img.Reason})
		}
		ui.RenderTable(headers, rows)
		ui.Println("")
	}

	// Features
	if len(ws.Resolved.Features) > 0 {
		ui.Println(ui.Bold("Features to Install"))
		for i, f := range ws.Resolved.Features {
			ui.Printf("  %d. %s", i+1, ui.Code(f.ID))
		}
		ui.Println("")
	}

	// Containers
	if len(plan.ContainersToCreate) > 0 {
		ui.Println(ui.Bold("Containers to Create"))
		for _, cont := range plan.ContainersToCreate {
			primary := ""
			if cont.IsPrimary {
				primary = ui.Dim(" (primary)")
			}
			ui.Printf("  %s %s%s", ui.Symbols.Bullet, cont.Name, primary)
		}
		ui.Println("")
	}

	// Services (compose)
	if len(plan.ServicesToStart) > 0 {
		ui.Println(ui.Bold("Services to Start"))
		ui.Printf("  %s", strings.Join(plan.ServicesToStart, ", "))
		ui.Println("")
	}

	// Hashes (verbose only)
	if ui.IsVerbose() {
		ui.Println(ui.Bold("Configuration Hashes"))
		ui.Printf("  %s", ui.FormatLabel("Config", ws.Hashes.Config))
		if ws.Hashes.Dockerfile != "" {
			ui.Printf("  %s", ui.FormatLabel("Dockerfile", ws.Hashes.Dockerfile))
		}
		if ws.Hashes.Compose != "" {
			ui.Printf("  %s", ui.FormatLabel("Compose", ws.Hashes.Compose))
		}
		if ws.Hashes.Features != "" {
			ui.Printf("  %s", ui.FormatLabel("Features", ws.Hashes.Features))
		}
		ui.Printf("  %s", ui.FormatLabel("Overall", ws.Hashes.Overall))
		ui.Println("")
	}

	ui.Println(ui.Dim("Run 'dcx up' to execute this plan."))

	return nil
}

func buildImagePlans(ws *workspace.Workspace) []pipeline.ImageBuildPlan {
	var plans []pipeline.ImageBuildPlan

	switch ws.Resolved.PlanType {
	case workspace.PlanTypeImage:
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, pipeline.ImageBuildPlan{
				Tag:       fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				BaseImage: ws.Resolved.Image,
				Features:  featureIDs,
				Reason:    "feature installation",
			})
		}

	case workspace.PlanTypeDockerfile:
		plans = append(plans, pipeline.ImageBuildPlan{
			Tag:        fmt.Sprintf("dcx-build-%s", ws.ID[:8]),
			Dockerfile: ws.Resolved.Dockerfile.Path,
			Context:    ws.Resolved.Dockerfile.Context,
			BuildArgs:  ws.Resolved.Dockerfile.Args,
			Reason:     "Dockerfile build",
		})
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, pipeline.ImageBuildPlan{
				Tag:       fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				BaseImage: fmt.Sprintf("dcx-build-%s", ws.ID[:8]),
				Features:  featureIDs,
				Reason:    "feature installation",
			})
		}

	case workspace.PlanTypeCompose:
		if len(ws.Resolved.Features) > 0 {
			featureIDs := make([]string, len(ws.Resolved.Features))
			for i, f := range ws.Resolved.Features {
				featureIDs[i] = f.ID
			}
			plans = append(plans, pipeline.ImageBuildPlan{
				Tag:      fmt.Sprintf("dcx-derived-%s", ws.ID[:8]),
				Features: featureIDs,
				Reason:   "feature installation for compose service",
			})
		}
	}

	return plans
}
