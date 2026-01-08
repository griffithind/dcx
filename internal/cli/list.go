package cli

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/state"
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
  dcx list --all        # List all environments (including stopped)
  dcx list --json       # Output as JSON for scripting`,
	RunE: runListEnvironments,
}

// EnvironmentInfo represents a dcx-managed environment for listing.
type EnvironmentInfo struct {
	EnvKey        string          `json:"envKey"`
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
	out := output.Global()
	c := out.Color()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// List all dcx-managed containers
	containers, err := dockerClient.ListContainers(ctx, map[string]string{
		docker.LabelManaged: "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Group containers by environment
	envMap := make(map[string]*EnvironmentInfo)
	for _, cont := range containers {
		labels := docker.LabelsFromMap(cont.Labels)

		// Skip non-running containers unless --all is specified
		if !listShowAll && !cont.Running {
			continue
		}

		envKey := labels.EnvKey
		if envKey == "" {
			continue
		}

		env, exists := envMap[envKey]
		if !exists {
			env = &EnvironmentInfo{
				EnvKey:        envKey,
				ProjectName:   labels.ProjectName,
				WorkspacePath: labels.WorkspacePath,
				Plan:          labels.Plan,
				Containers:    []ContainerItem{},
				CreatedAt:     cont.Created,
			}
			envMap[envKey] = env
		}

		// Add container to environment
		env.Containers = append(env.Containers, ContainerItem{
			ID:        cont.ID[:12],
			Name:      cont.Name,
			Status:    cont.Status,
			IsPrimary: labels.Primary,
			CreatedAt: cont.Created,
		})

		// Track oldest creation time for the environment
		if cont.Created.Before(env.CreatedAt) {
			env.CreatedAt = cont.Created
		}
	}

	// Determine state for each environment
	stateMgr := state.NewManager(dockerClient)
	for _, env := range envMap {
		s, _, _ := stateMgr.GetState(ctx, env.EnvKey)
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

	// JSON output mode
	if out.IsJSON() {
		return out.JSON(environments)
	}

	// Text output mode
	if len(environments) == 0 {
		out.Println("No dcx-managed environments found.")
		if !listShowAll {
			out.Println(c.Dim("Use --all to include stopped environments."))
		}
		return nil
	}

	table := output.NewTable(out.Writer(), []string{"Name", "State", "Containers", "Workspace"})
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
		identifier := env.EnvKey
		if env.ProjectName != "" {
			identifier = env.ProjectName
		}

		table.AddRow(
			identifier,
			formatListState(env.State),
			strings.Join(containerNames, ", "),
			c.Code(workspace),
		)
	}

	return table.RenderWithDivider()
}

// formatListState returns a colored state string.
func formatListState(s string) string {
	c := output.Color()
	switch state.State(s) {
	case state.StateRunning:
		return c.StateRunning(s)
	case state.StateCreated:
		return c.StateStopped(s)
	case state.StateStale:
		return c.Warning(s)
	case state.StateBroken:
		return c.StateError(s)
	case state.StateAbsent:
		return c.StateUnknown(s)
	default:
		return s
	}
}

func init() {
	listCmd.Flags().BoolVar(&listShowAll, "all", false, "show all environments (including stopped)")
	rootCmd.AddCommand(listCmd)
}
