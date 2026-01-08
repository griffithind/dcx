package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"text/tabwriter"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/pipeline"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	planOutputJSON bool
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
  dcx plan -p /path     # Show plan for specific workspace`,
	RunE: runPlan,
}

func init() {
	planCmd.Flags().BoolVar(&planOutputJSON, "json", false, "output plan as JSON")
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

	if planOutputJSON {
		return outputPlanJSON(plan)
	}

	return outputPlanTable(plan)
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

func outputPlanJSON(plan *pipeline.PlanResult) error {
	output := map[string]interface{}{
		"action":  plan.Action,
		"reason":  plan.Reason,
		"changes": plan.Changes,
		"workspace": map[string]interface{}{
			"id":   plan.Workspace.ID,
			"name": plan.Workspace.Name,
			"path": plan.Workspace.LocalRoot,
		},
		"images_to_build":     plan.ImagesToBuild,
		"containers_to_create": plan.ContainersToCreate,
		"services_to_start":    plan.ServicesToStart,
	}

	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(output)
}

func outputPlanTable(plan *pipeline.PlanResult) error {
	ws := plan.Workspace

	fmt.Println("Devcontainer Execution Plan")
	fmt.Println("===========================")
	fmt.Println()

	// Workspace info
	fmt.Printf("Workspace: %s\n", ws.Name)
	fmt.Printf("Path:      %s\n", ws.LocalRoot)
	fmt.Printf("ID:        %s\n", ws.ID)
	fmt.Println()

	// Action
	actionColor := ""
	actionReset := ""
	switch plan.Action {
	case pipeline.ActionNone:
		fmt.Println("Status: Up to date - no changes needed")
		return nil
	case pipeline.ActionCreate:
		actionColor = "\033[32m" // green
	case pipeline.ActionRecreate:
		actionColor = "\033[33m" // yellow
	case pipeline.ActionRebuild:
		actionColor = "\033[31m" // red
	case pipeline.ActionStart:
		actionColor = "\033[36m" // cyan
	}
	if actionColor != "" {
		actionReset = "\033[0m"
	}

	fmt.Printf("Action: %s%s%s\n", actionColor, plan.Action, actionReset)
	fmt.Printf("Reason: %s\n", plan.Reason)
	fmt.Println()

	// Changes detected
	if len(plan.Changes) > 0 {
		fmt.Println("Changes Detected:")
		for _, change := range plan.Changes {
			fmt.Printf("  - %s\n", change)
		}
		fmt.Println()
	}

	// Images to build
	if len(plan.ImagesToBuild) > 0 {
		fmt.Println("Images to Build:")
		w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
		fmt.Fprintln(w, "  TAG\tBASE\tREASON")
		fmt.Fprintln(w, "  ---\t----\t------")
		for _, img := range plan.ImagesToBuild {
			base := img.BaseImage
			if base == "" && img.Dockerfile != "" {
				base = "(Dockerfile)"
			}
			fmt.Fprintf(w, "  %s\t%s\t%s\n", img.Tag, base, img.Reason)
		}
		w.Flush()
		fmt.Println()
	}

	// Features
	if len(ws.Resolved.Features) > 0 {
		fmt.Println("Features to Install:")
		for i, f := range ws.Resolved.Features {
			fmt.Printf("  %d. %s\n", i+1, f.ID)
		}
		fmt.Println()
	}

	// Containers
	if len(plan.ContainersToCreate) > 0 {
		fmt.Println("Containers to Create:")
		for _, c := range plan.ContainersToCreate {
			primary := ""
			if c.IsPrimary {
				primary = " (primary)"
			}
			fmt.Printf("  - %s%s\n", c.Name, primary)
		}
		fmt.Println()
	}

	// Services (compose)
	if len(plan.ServicesToStart) > 0 {
		fmt.Println("Services to Start:")
		fmt.Printf("  %s\n", strings.Join(plan.ServicesToStart, ", "))
		fmt.Println()
	}

	// Hashes
	if verbose {
		fmt.Println("Configuration Hashes:")
		fmt.Printf("  Config:     %s\n", ws.Hashes.Config)
		if ws.Hashes.Dockerfile != "" {
			fmt.Printf("  Dockerfile: %s\n", ws.Hashes.Dockerfile)
		}
		if ws.Hashes.Compose != "" {
			fmt.Printf("  Compose:    %s\n", ws.Hashes.Compose)
		}
		if ws.Hashes.Features != "" {
			fmt.Printf("  Features:   %s\n", ws.Hashes.Features)
		}
		fmt.Printf("  Overall:    %s\n", ws.Hashes.Overall)
		fmt.Println()
	}

	fmt.Println("Run 'dcx up' to execute this plan.")

	return nil
}
