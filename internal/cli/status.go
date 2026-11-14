package cli

import (
	"fmt"
	"runtime"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/service"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/griffithind/dcx/internal/version"
	"github.com/spf13/cobra"
)

var (
	statusDetailed bool
	statusJSONOut  bool
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show devcontainer status",
	Long: `Show the current state of the devcontainer environment.

This command queries Docker for containers managed by dcx and displays
their current state (ABSENT, CREATED, RUNNING, STALE, or BROKEN).

Use --detailed for container details, the SSH endpoint and host key, the
resolved plan and features, why a container is stale, and its dcx labels; --json prints the same as stable
JSON (--detailed --json replaces the old hidden 'debug' command).

This is an offline-safe command that does not require network access.`,
	RunE: runStatus,
}

func init() {
	statusCmd.Flags().BoolVarP(&statusDetailed, "detailed", "d", false, "show detailed environment information")
	statusCmd.Flags().BoolVar(&statusJSONOut, "json", false, "print machine-readable JSON (stable keys) instead of text")
}

func runStatus(cmd *cobra.Command, args []string) error {
	// No daemon check up front: Status reports a missing or broken config
	// before (or instead of) a Docker error.
	ctx, stop := commandContext()
	defer stop()
	rep := ui.Reporter()
	if statusJSONOut {
		rep = report.Discard
	}
	svc := newService(newEngine(), rep)

	status, err := svc.Status(ctx, statusDetailed || statusJSONOut)
	if err != nil {
		return err
	}
	if statusJSONOut {
		return printJSON(statusToJSON(status, statusDetailed))
	}
	renderStatus(status, statusDetailed)
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
	case r.SSHAlias != "" && r.SSH != nil && r.SSH.Port > 0:
		label("", "SSH", fmt.Sprintf("%s  %s", ui.Code("ssh "+r.SSHAlias), ui.Dim(fmt.Sprintf("(%s:%d)", r.SSH.BindAddress, r.SSH.Port))))
	case r.SSHAlias != "":
		label("", "SSH", ui.Code("ssh "+r.SSHAlias))
	case r.State != state.StateAbsent:
		label("", "SSH", ui.Dim("not configured (run 'dcx up' to configure)"))
	}
	if r.Shortcuts > 0 {
		label("", "Shortcuts", fmt.Sprintf("%d defined (use 'dcx run --list' to view)", r.Shortcuts))
	}
	if r.ConfigError != "" {
		label("", "Config", ui.Dim(r.ConfigError))
	}
	if reason := staleReason(r); reason != "" {
		label("", "Stale", reason)
	}
	if r.Drift {
		label("", "Drift", "inputs changed (substituted values or feature versions); 'dcx up --recreate' applies them")
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

	if ssh := r.SSH; detailed && ssh != nil {
		ui.Println("")
		ui.Println(ui.Bold("SSH"))
		label("  ", "Allowed", strings.Join(ssh.AllowedIPs, ", "))
		if ssh.HostKeyFingerprint != "" {
			label("  ", "Fingerprint", ssh.HostKeyFingerprint)
			label("  ", "Host Key", ssh.HostKeyFile)
		}
		pinned := "yes"
		if !ssh.KnownHostsPinned {
			pinned = "no (run 'dcx up' to pin it)"
		}
		label("  ", "Pinned", pinned)
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
		optional("Plan", string(r.PlanType))
		optional("Features", strings.Join(r.Features, ", "))
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

// staleReason explains a stale state ("" otherwise).
func staleReason(r *service.StatusReport) string {
	if r.State != state.StateStale || r.Container == nil {
		return ""
	}
	return fmt.Sprintf("config changed (container %s, now %s); 'dcx up' recreates it",
		shortID(r.Container.ConfigHash), shortID(r.ConfigHash))
}

// statusJSON is the `dcx status --json` contract: stable, camelCase keys,
// for scripts and tests (the human output may change).
type statusJSON struct {
	WorkspacePath string         `json:"workspacePath"`
	WorkspaceID   string         `json:"workspaceID"`
	Name          string         `json:"name"`
	ResourceName  string         `json:"resourceName"`
	State         string         `json:"state"`
	SSHAlias      string         `json:"sshAlias,omitempty"`
	ConfigHash    string         `json:"configHash,omitempty"`
	Shortcuts     int            `json:"shortcuts"`
	ConfigError   string         `json:"configError,omitempty"`
	Drift         bool           `json:"drift,omitempty"`
	Container     *containerJSON `json:"container"`
	SSH           *sshJSON       `json:"ssh,omitempty"`
	// Detail is set with --detailed (what the old hidden `debug` showed).
	Detail *statusDetailJSON `json:"detail,omitempty"`
}

type statusDetailJSON struct {
	DCXVersion  string            `json:"dcxVersion"`
	Platform    string            `json:"platform"`
	PlanType    string            `json:"planType,omitempty"`
	BaseImage   string            `json:"baseImage,omitempty"`
	Features    []string          `json:"features,omitempty"`
	StaleReason string            `json:"staleReason,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// sshJSON is the workspace's SSH endpoint (status --json).
type sshJSON struct {
	BindAddress        string   `json:"bindAddress,omitempty"`
	Port               int      `json:"port,omitempty"`
	AllowedIPs         []string `json:"allowedIPs"`
	HostKeyFingerprint string   `json:"hostKeyFingerprint,omitempty"`
	HostKeyFile        string   `json:"hostKeyFile,omitempty"`
	KnownHostsPinned   bool     `json:"knownHostsPinned"`
}

type containerJSON struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Running    bool   `json:"running"`
	ConfigHash string `json:"configHash,omitempty"`
	Image      string `json:"image,omitempty"`
	StartedAt  string `json:"startedAt,omitempty"`
}

func statusToJSON(r *service.StatusReport, detailed bool) statusJSON {
	out := statusJSON{
		WorkspacePath: r.WorkspacePath,
		WorkspaceID:   r.Identity.WorkspaceID,
		Name:          r.Identity.Name,
		ResourceName:  r.Identity.ResourceName,
		State:         string(r.State),
		SSHAlias:      r.SSHAlias,
		ConfigHash:    r.ConfigHash,
		Shortcuts:     r.Shortcuts,
		ConfigError:   r.ConfigError,
		Drift:         r.Drift,
	}
	if detailed {
		out.Detail = &statusDetailJSON{
			DCXVersion:  version.Version,
			Platform:    runtime.GOOS + "/" + runtime.GOARCH,
			PlanType:    string(r.PlanType),
			BaseImage:   r.BaseImage,
			Features:    r.Features,
			StaleReason: staleReason(r),
		}
		if r.Container != nil && r.Container.Labels != nil {
			out.Detail.Labels = r.Container.Labels.ToMap()
		}
	}
	if ssh := r.SSH; ssh != nil {
		out.SSH = &sshJSON{
			BindAddress: ssh.BindAddress, Port: ssh.Port, AllowedIPs: ssh.AllowedIPs,
			HostKeyFingerprint: ssh.HostKeyFingerprint, HostKeyFile: ssh.HostKeyFile,
			KnownHostsPinned: ssh.KnownHostsPinned,
		}
	}
	if c := r.Container; c != nil {
		out.Container = &containerJSON{
			ID: c.ID, Name: c.Name, Status: c.Status, Running: c.Running,
			ConfigHash: c.ConfigHash, Image: r.Image, StartedAt: r.StartedAt,
		}
	}
	return out
}
