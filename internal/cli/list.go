package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var (
	listOutputJSON bool
	listShowAll    bool
)

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
	EnvKey        string          `json:"env_key"`
	ProjectName   string          `json:"project_name,omitempty"`
	WorkspacePath string          `json:"workspace_path"`
	State         string          `json:"state"`
	Plan          string          `json:"plan"`
	Containers    []ContainerItem `json:"containers"`
	CreatedAt     time.Time       `json:"created_at"`
}

// ContainerItem represents a container in the environment.
type ContainerItem struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	IsPrimary bool      `json:"is_primary"`
	CreatedAt time.Time `json:"created_at"`
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
		docker.LabelManaged: "true",
	})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	// Group containers by environment
	envMap := make(map[string]*EnvironmentInfo)
	for _, c := range containers {
		labels := docker.LabelsFromMap(c.Labels)

		// Skip non-running containers unless --all is specified
		if !listShowAll && !c.Running {
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
				CreatedAt:     c.Created,
			}
			envMap[envKey] = env
		}

		// Add container to environment
		env.Containers = append(env.Containers, ContainerItem{
			ID:        c.ID[:12],
			Name:      c.Name,
			Status:    c.Status,
			IsPrimary: labels.Primary,
			CreatedAt: c.Created,
		})

		// Track oldest creation time for the environment
		if c.Created.Before(env.CreatedAt) {
			env.CreatedAt = c.Created
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

	if listOutputJSON {
		return outputJSON(environments)
	}

	return outputTable(environments)
}

func outputJSON(environments []*EnvironmentInfo) error {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	return enc.Encode(environments)
}

func outputTable(environments []*EnvironmentInfo) error {
	if len(environments) == 0 {
		fmt.Println("No dcx-managed environments found.")
		if !listShowAll {
			fmt.Println("Use --all to include stopped environments.")
		}
		return nil
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ENV KEY\tSTATE\tCONTAINERS\tWORKSPACE")
	fmt.Fprintln(w, "-------\t-----\t----------\t---------")

	for _, env := range environments {
		// Build container summary
		containerNames := make([]string, 0, len(env.Containers))
		for _, c := range env.Containers {
			name := c.Name
			if c.IsPrimary {
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

		fmt.Fprintf(w, "%s\t%s\t%s\t%s\n",
			identifier,
			env.State,
			strings.Join(containerNames, ", "),
			workspace,
		)
	}

	return w.Flush()
}

func init() {
	listCmd.Flags().BoolVar(&listOutputJSON, "json", false, "output as JSON")
	listCmd.Flags().BoolVar(&listShowAll, "all", false, "show all environments (including stopped)")
	rootCmd.AddCommand(listCmd)
}
