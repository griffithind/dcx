package cli

import (
	"fmt"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/service"
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
	cliCtx, err := NewCLIContext()
	if err != nil {
		return err
	}
	defer cliCtx.Close()

	report, err := cliCtx.Service.Status(cliCtx.Ctx, statusDetailed)
	if err != nil {
		return err
	}
	renderStatus(report, statusDetailed)
	return nil
}

// renderStatus prints a status report.
func renderStatus(r *service.StatusReport, detailed bool) {
	label := func(indent, name, value string) { ui.Printf("%s%s", indent, ui.FormatLabel(name, value)) }

	label("", "Workspace", ui.Code(r.WorkspacePath))
	if r.Identity.ResourceName != "" {
		label("", "Project", r.Identity.ResourceName)
	}
	label("", "Workspace ID", r.Identity.WorkspaceID)
	label("", "State", ui.StateColor(string(r.State)))
	switch {
	case r.SSHAlias != "":
		label("", "SSH", ui.Code("ssh "+r.SSHAlias))
	case r.State != state.StateAbsent:
		label("", "SSH", ui.Dim("not configured (run 'dcx up' to configure)"))
	}
	if r.Shortcuts > 0 {
		label("", "Shortcuts", fmt.Sprintf("%d defined (use 'dcx run --list' to view)", r.Shortcuts))
	}

	if c := r.Container; c != nil {
		ui.Println("")
		ui.Println(ui.Bold("Primary Container"))
		label("  ", "ID", shortID(c.ID))
		label("  ", "Name", c.Name)
		label("  ", "Status", c.Status)
		if c.ConfigHash != "" {
			label("  ", "Config", shortID(c.ConfigHash))
		}
		if detailed && r.Image != "" {
			ui.Println("")
			ui.Println(ui.Bold("Container Details"))
			label("  ", "Image", r.Image)
			if r.StartedAt != "" {
				label("  ", "Started", r.StartedAt)
			}
			label("  ", "Running", fmt.Sprintf("%t", c.Running))
		}
	}

	if cfg := r.Config; detailed && cfg != nil {
		ui.Println("")
		ui.Println(ui.Bold("Configuration"))
		optional := func(name, value string) {
			if value != "" {
				label("  ", name, value)
			}
		}
		optional("Image", cfg.Image)
		if cfg.Build != nil {
			optional("Dockerfile", cfg.Build.Dockerfile)
			optional("Build Context", cfg.Build.Context)
		}
		optional("Compose Files", strings.Join(cfg.DockerComposeFile, ", "))
		optional("Service", cfg.Service)
		optional("Remote User", cfg.RemoteUser)
		optional("Workspace Folder", cfg.WorkspaceFolder)
		features := make([]string, 0, len(cfg.Features))
		for f := range cfg.Features {
			features = append(features, f)
		}
		sort.Strings(features)
		optional("Features", strings.Join(features, ", "))
		if r.ConfigHash != "" {
			optional("Config Hash", shortID(r.ConfigHash))
		}
	}

	if detailed && r.Container != nil && r.Container.Labels != nil {
		labels := r.Container.Labels.ToMap()
		keys := make([]string, 0, len(labels))
		for k := range labels {
			if strings.HasPrefix(k, state.Prefix+".") {
				keys = append(keys, k)
			}
		}
		sort.Strings(keys)
		if len(keys) > 0 {
			ui.Println("")
			ui.Println(ui.Bold("Labels"))
			for _, k := range keys {
				ui.Printf("  %s: %s", ui.Dim(k), labels[k])
			}
		}
	}
}

// shortID shortens a container ID or hash for display. Values that are
// already short (legacy or hand-edited labels) are shown as they are.
func shortID(s string) string {
	if len(s) > 12 {
		return s[:12]
	}
	return s
}
