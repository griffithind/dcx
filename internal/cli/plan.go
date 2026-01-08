package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/pipeline"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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
  dcx plan --json       # Output plan as JSON for scripting
  dcx plan -w /path     # Show plan for specific workspace`,
	RunE: runPlan,
}

func init() {
	planCmd.GroupID = "info"
	rootCmd.AddCommand(planCmd)
}

// PlanOutput represents the plan output for JSON.
type PlanOutput struct {
	Action      string                    `json:"action"`
	Reason      string                    `json:"reason"`
	Changes     []string                  `json:"changes,omitempty"`
	Workspace   WorkspacePlanOutput       `json:"workspace"`
	ImagesBuild []pipeline.ImageBuildPlan `json:"imagesToBuild,omitempty"`
	Containers  []pipeline.ContainerPlan  `json:"containersToCreate,omitempty"`
	Services    []string                  `json:"servicesToStart,omitempty"`
}

// WorkspacePlanOutput represents workspace info for plan output.
type WorkspacePlanOutput struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Path string `json:"path"`
}

func runPlan(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	out := output.Global()
	c := out.Color()

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

	// JSON output mode
	if out.IsJSON() {
		return out.JSON(PlanOutput{
			Action:  string(plan.Action),
			Reason:  plan.Reason,
			Changes: plan.Changes,
			Workspace: WorkspacePlanOutput{
				ID:   plan.Workspace.ID,
				Name: plan.Workspace.Name,
				Path: plan.Workspace.LocalRoot,
			},
			ImagesBuild: plan.ImagesToBuild,
			Containers:  plan.ContainersToCreate,
			Services:    plan.ServicesToStart,
		})
	}

	// Text output mode
	out.Println(c.Header("Devcontainer Execution Plan"))
	out.Println(c.Dim("==========================="))
	out.Println()

	// Workspace info
	kv := output.NewKeyValueTable(out.Writer())
	kv.Add("Workspace", ws.Name)
	kv.Add("Path", c.Code(ws.LocalRoot))
	kv.Add("ID", c.Dim(ws.ID))
	kv.Render()
	out.Println()

	// Action
	if plan.Action == pipeline.ActionNone {
		out.Println(output.FormatSuccess("Up to date - no changes needed"))
		return nil
	}

	// Show action with color
	var actionStr string
	switch plan.Action {
	case pipeline.ActionCreate:
		actionStr = c.StateRunning(string(plan.Action))
	case pipeline.ActionRecreate:
		actionStr = c.Warning(string(plan.Action))
	case pipeline.ActionRebuild:
		actionStr = c.StateError(string(plan.Action))
	case pipeline.ActionStart:
		actionStr = c.Info(string(plan.Action))
	default:
		actionStr = string(plan.Action)
	}

	actionKV := output.NewKeyValueTable(out.Writer())
	actionKV.Add("Action", actionStr)
	actionKV.Add("Reason", plan.Reason)
	actionKV.Render()
	out.Println()

	// Changes detected
	if len(plan.Changes) > 0 {
		out.Println(c.Header("Changes Detected"))
		for _, change := range plan.Changes {
			out.Printf("  %s %s", output.Symbols.Bullet, change)
		}
		out.Println()
	}

	// Images to build
	if len(plan.ImagesToBuild) > 0 {
		out.Println(c.Header("Images to Build"))
		table := output.NewTable(out.Writer(), []string{"Tag", "Base", "Reason"})
		for _, img := range plan.ImagesToBuild {
			base := img.BaseImage
			if base == "" && img.Dockerfile != "" {
				base = "(Dockerfile)"
			}
			table.AddRow(img.Tag, base, img.Reason)
		}
		table.RenderWithDivider()
		out.Println()
	}

	// Features
	if len(ws.Resolved.Features) > 0 {
		out.Println(c.Header("Features to Install"))
		for i, f := range ws.Resolved.Features {
			out.Printf("  %d. %s", i+1, c.Code(f.ID))
		}
		out.Println()
	}

	// Containers
	if len(plan.ContainersToCreate) > 0 {
		out.Println(c.Header("Containers to Create"))
		for _, cont := range plan.ContainersToCreate {
			primary := ""
			if cont.IsPrimary {
				primary = c.Dim(" (primary)")
			}
			out.Printf("  %s %s%s", output.Symbols.Bullet, cont.Name, primary)
		}
		out.Println()
	}

	// Services (compose)
	if len(plan.ServicesToStart) > 0 {
		out.Println(c.Header("Services to Start"))
		out.Printf("  %s", strings.Join(plan.ServicesToStart, ", "))
		out.Println()
	}

	// Hashes (verbose only)
	if out.IsVerbose() {
		out.Println(c.Header("Configuration Hashes"))
		hashKV := output.NewKeyValueTable(out.Writer())
		hashKV.Add("  Config", ws.Hashes.Config)
		if ws.Hashes.Dockerfile != "" {
			hashKV.Add("  Dockerfile", ws.Hashes.Dockerfile)
		}
		if ws.Hashes.Compose != "" {
			hashKV.Add("  Compose", ws.Hashes.Compose)
		}
		if ws.Hashes.Features != "" {
			hashKV.Add("  Features", ws.Hashes.Features)
		}
		hashKV.Add("  Overall", ws.Hashes.Overall)
		hashKV.Render()
		out.Println()
	}

	out.Println(c.Dim("Run 'dcx up' to execute this plan."))

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
