package cli

import (
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	listShowAll bool
	listJSONOut bool
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
  dcx list --all        # List all environments (including stopped)`,
	RunE: runListEnvironments,
}

func runListEnvironments(cmd *cobra.Command, args []string) error {
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	envs, err := cliCtx.Service.List(cliCtx.Ctx, listShowAll)
	if err != nil {
		return err
	}
	if listJSONOut {
		return printJSON(listToJSON(envs))
	}
	return renderList(envs, listShowAll)
}

// renderList prints environments as a table.
func renderList(envs []service.Environment, all bool) error {
	if len(envs) == 0 {
		ui.Println("No dcx-managed environments found.")
		if !all {
			ui.Println(ui.Dim("Use --all to include stopped environments."))
		}
		return nil
	}

	headers := []string{"Name", "State", "Containers", "Workspace"}
	rows := make([][]string, 0, len(envs))
	for _, env := range envs {
		names := make([]string, 0, len(env.Containers))
		for _, c := range env.Containers {
			name := c.Name
			if c.IsPrimary {
				name += "*"
			}
			names = append(names, name)
		}
		workspace := env.WorkspacePath
		if len(workspace) > 50 {
			workspace = "..." + workspace[len(workspace)-47:]
		}
		name := env.Name
		if name == "" {
			name = env.WorkspaceID
		}
		rows = append(rows, []string{name, formatListState(string(env.State)), strings.Join(names, ", "), ui.Code(workspace)})
	}
	return ui.RenderTable(headers, rows)
}

// formatListState returns a colored state string.
func formatListState(s string) string {
	return ui.StateColor(s)
}

func init() {
	listCmd.Flags().BoolVar(&listShowAll, "all", false, "show all environments (including stopped)")
	listCmd.Flags().BoolVar(&listJSONOut, "json", false, "print machine-readable JSON (stable keys) instead of a table")
	listCmd.GroupID = "info"
	rootCmd.AddCommand(listCmd)
}

// environmentJSON is one entry of the `dcx list --json` contract.
type environmentJSON struct {
	WorkspaceID   string                `json:"workspaceID"`
	Name          string                `json:"name"`
	WorkspacePath string                `json:"workspacePath"`
	State         string                `json:"state"`
	BuildMethod   string                `json:"buildMethod"`
	CreatedAt     time.Time             `json:"createdAt"`
	Containers    []environmentItemJSON `json:"containers"`
}

type environmentItemJSON struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Status    string    `json:"status"`
	Primary   bool      `json:"primary"`
	CreatedAt time.Time `json:"createdAt"`
}

func listToJSON(envs []service.Environment) []environmentJSON {
	out := make([]environmentJSON, 0, len(envs))
	for _, e := range envs {
		j := environmentJSON{
			WorkspaceID: e.WorkspaceID, Name: e.Name, WorkspacePath: e.WorkspacePath,
			State: string(e.State), BuildMethod: e.BuildMethod, CreatedAt: e.CreatedAt,
			Containers: []environmentItemJSON{},
		}
		for _, c := range e.Containers {
			j.Containers = append(j.Containers, environmentItemJSON{ID: c.ID, Name: c.Name, Status: c.Status, Primary: c.IsPrimary, CreatedAt: c.CreatedAt})
		}
		out = append(out, j)
	}
	return out
}
