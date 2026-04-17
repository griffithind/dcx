package cli

import (
	"context"
	"encoding/json"
	"fmt"

	containerPkg "github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/service"
	dcxssh "github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	sshInfoJSON   bool
	sshInfoDoctor bool
)

var sshInfoCmd = &cobra.Command{
	Use:   "info",
	Short: "Show SSH connection info for the current workspace",
	Long: `Display the SSH endpoint, host-key fingerprint, and diagnostic info for
the devcontainer in the current workspace.

With --doctor, runs live probes (TCP reachable, listener responsive, host
key pinned) and reports PASS/FAIL/WARN for each.`,
	RunE: runSSHInfo,
}

func init() {
	sshInfoCmd.Flags().BoolVar(&sshInfoJSON, "json", false, "Emit machine-readable JSON")
	sshInfoCmd.Flags().BoolVar(&sshInfoDoctor, "doctor", false, "Run live diagnostics")
	sshCmd.AddCommand(sshInfoCmd)
}

// sshInfo is the structured output dcx ssh info [--json] produces.
type sshInfo struct {
	Workspace   string   `json:"workspace"`
	Host        string   `json:"host"`        // the ~/.ssh/config Host alias (workspace.dcx)
	BindAddress string   `json:"bind_address"`
	Port        int      `json:"port"`
	HostKey     string   `json:"host_key_fingerprint"`
	HostKeyFile string   `json:"host_key_file"`
	AllowedIPs  []string `json:"allowed_ips"`
	KnownPinned bool     `json:"known_hosts_pinned"`
}

func runSSHInfo(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	svc := service.NewDevContainerService(workspacePath, configPath, verbose)
	defer svc.Close()

	ids, err := svc.GetIdentifiers()
	if err != nil {
		return fmt.Errorf("identifiers: %w", err)
	}

	hostKeyPath, signer, err := dcxssh.EnsureHostKey(ids.WorkspaceID)
	if err != nil {
		return fmt.Errorf("host key: %w", err)
	}

	pinned, _ := dcxssh.HasHost(ids.WorkspaceID)

	info := sshInfo{
		Workspace:   ids.WorkspaceID,
		Host:        ids.SSHHost,
		HostKey:     dcxssh.Fingerprint(signer),
		HostKeyFile: hostKeyPath,
		KnownPinned: pinned,
		AllowedIPs:  []string{"127.0.0.0/8", "::1/128"},
	}

	// Best-effort: look up the actual mapped port from Docker.
	if docker, derr := containerPkg.DockerClient(); derr == nil {
		if port, perr := docker.PortMapping(ctx, resolveContainerName(ctx, svc), 48022, "tcp"); perr == nil {
			info.Port = port
			info.BindAddress = "127.0.0.1"
		}
	}

	if sshInfoDoctor {
		return runSSHDoctor(ctx, info)
	}

	if sshInfoJSON {
		data, _ := json.MarshalIndent(info, "", "  ")
		fmt.Println(string(data))
		return nil
	}

	ui.Printf("Workspace:  %s", info.Workspace)
	if info.Port > 0 {
		ui.Printf("SSH:        ssh %s   (%s:%d)", info.Host, info.BindAddress, info.Port)
	} else {
		ui.Printf("SSH:        ssh %s   (listener not reachable — run 'dcx ssh info --doctor')", info.Host)
	}
	ui.Printf("Fingerprint: %s", info.HostKey)
	ui.Printf("Host key:   %s", info.HostKeyFile)
	if !info.KnownPinned {
		ui.Warning("known_hosts not yet pinned for this workspace")
	}
	return nil
}

// runSSHDoctor performs live probes and prints PASS/FAIL/WARN lines.
func runSSHDoctor(ctx context.Context, info sshInfo) error {
	ui.Printf("Running diagnostics for workspace %s…", info.Workspace)

	pass := ui.Bold("PASS")
	fail := ui.Bold("FAIL")
	warn := ui.Bold("WARN")

	// 1. Port resolvable?
	if info.Port == 0 {
		ui.Printf("  [%s] Listener not bound to any host port", fail)
		return fmt.Errorf("listener is not publishing port 48022")
	}
	ui.Printf("  [%s] Listener published on %s:%d", pass, info.BindAddress, info.Port)

	// 2. TCP reachable?
	if err := tcpReachable(ctx, info.BindAddress, info.Port); err != nil {
		ui.Printf("  [%s] TCP reachability: %v", fail, err)
		return fmt.Errorf("port %d not reachable", info.Port)
	}
	ui.Printf("  [%s] TCP reachability", pass)

	// 3. known_hosts pinned?
	if info.KnownPinned {
		ui.Printf("  [%s] known_hosts pinned", pass)
	} else {
		ui.Printf("  [%s] known_hosts not pinned — run 'dcx up' to re-pin", warn)
	}

	ui.Printf("Overall: listener healthy, fingerprint %s", info.HostKey)
	return nil
}

func resolveContainerName(ctx context.Context, svc *service.DevContainerService) string {
	plan, err := svc.Plan(ctx, service.PlanOptions{})
	if err != nil || plan == nil || plan.ContainerInfo == nil {
		return ""
	}
	return plan.ContainerInfo.Name
}
