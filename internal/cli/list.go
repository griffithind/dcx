package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/containerstate"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/labels"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var listShowAll bool

var listCmd = &cobra.Command{
	Use:     "list",
	Aliases: []string{"ls", "ps"},
	Short:   "List all dcx-managed environments",
	Long: `List all devcontainer environments managed by dcx.

By default, shows running environments grouped by workspace.
Use --all to include stopped environments.

Examples:
  dcx list              # List running environments
  dcx list --all        # List all environments (including stopped)`,
	RunE: runListEnvironments,
}

// EnvironmentInfo represents a dcx-managed environment for listing.
type EnvironmentInfo struct {
	WorkspaceID        string          `json:"workspaceID"`
	ProjectName   string          `json:"projectName,omitempty"`
	WorkspacePath string          `json:"workspacePath"`
	State         string          `json:"state"`
	Plan          string          `json:"plan"`
	Containers    []ContainerItem `json:"containers"`
	CreatedAt     time.Time       `json:"createdAt"`
}

// ContainerItem represents a container in the environment.
type ContainerItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	IsPrimary bool      `json:"isPrimary"`
	CreatedAt time.Time `json:"createdAt"`
}

func runListEnvironments(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// List all dcx-managed containers
	containers, err := dockerClient.ListContainers(ctx, map[string]string{
		labels.LabelManaged: "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Group containers by environment
	envMap := make(map[string]*EnvironmentInfo)
	for _, cont := range containers {
		lbls := labels.FromMap(cont.Labels)

		// Skip non-running containers unless --all is specified
		if !listShowAll && !cont.Running {
			continue
		}

		workspaceID := lbls.WorkspaceID
		if workspaceID == "" {
			continue
		}

		env, exists := envMap[workspaceID]
		if !exists {
			env = &EnvironmentInfo{
				WorkspaceID:        workspaceID,
				ProjectName:   lbls.WorkspaceName,
				WorkspacePath: lbls.WorkspacePath,
				Plan:          lbls.BuildMethod,
				Containers:    []ContainerItem{},
				CreatedAt:     cont.Created,
			}
			envMap[workspaceID] = env
		}

		// Add container to environment
		env.Containers = append(env.Containers, ContainerItem{
			ID:        cont.ID[:12],
			Name:      cont.Name,
			Status:    cont.Status,
			IsPrimary: lbls.IsPrimary,
			CreatedAt: cont.Created,
		})

		// Track oldest creation time for the environment
		if cont.Created.Before(env.CreatedAt) {
			env.CreatedAt = cont.Created
		}
	}

	// Determine state for each environment
	stateMgr := containerstate.NewManager(dockerClient)
	for _, env := range envMap {
		s, _, _ := stateMgr.GetState(ctx, env.WorkspaceID)
		env.State = string(s)
	}

	// Convert map to slice and sort by workspace path
	environments := make([]*EnvironmentInfo, 0, len(envMap))
	for _, env := range envMap {
		environments = append(environments, env)
	}
	sort.Slice(environments, func(i, j int) bool {
		return environments[i].WorkspacePath < environments[j].WorkspacePath
	})

	// Text output mode
	if len(environments) == 0 {
		ui.Println("No dcx-managed environments found.")
		if !listShowAll {
			ui.Println(ui.Dim("Use --all to include stopped environments."))
		}
		return nil
	}

	headers := []string{"Name", "State", "Containers", "Workspace"}
	var rows [][]string
	for _, env := range environments {
		// Build container summary
		containerNames := make([]string, 0, len(env.Containers))
		for _, cont := range env.Containers {
			name := cont.Name
			if cont.IsPrimary {
				name = name + "*"
			}
			containerNames = append(containerNames, name)
		}

		// Truncate workspace path for display
		workspace := env.WorkspacePath
		if len(workspace) > 50 {
			workspace = "..." + workspace[len(workspace)-47:]
		}

		// Use project name as identifier if available
		identifier := env.WorkspaceID
		if env.ProjectName != "" {
			identifier = env.ProjectName
		}

		rows = append(rows, []string{
			identifier,
			formatListState(env.State),
			strings.Join(containerNames, ", "),
			ui.Code(workspace),
		})
	}

	return ui.RenderTable(headers, rows)
}

// formatListState returns a colored state string.
func formatListState(s string) string {
	return ui.StateColor(s)
}

func init() {
	listCmd.Flags().BoolVar(&listShowAll, "all", false, "show all environments (including stopped)")
	listCmd.GroupID = "info"
	rootCmd.AddCommand(listCmd)
}
