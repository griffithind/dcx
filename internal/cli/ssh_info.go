package cli

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/report"

	containerPkg "github.com/griffithind/dcx/internal/container"
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
	Host        string   `json:"host"` // the ~/.ssh/config Host alias (workspace.dcx)
	BindAddress string   `json:"bindAddress"`
	Port        int      `json:"port"`
	HostKey     string   `json:"hostKeyFingerprint"`
	HostKeyFile string   `json:"hostKeyFile"`
	AllowedIPs  []string `json:"allowedIPs"`
	KnownPinned bool     `json:"knownHostsPinned"`
}

func runSSHInfo(cmd *cobra.Command, args []string) error {
	ctx, stop := commandContext()
	defer stop()

	// Docker is optional here: identifiers and host-key details work without
	// it, and the port lookup below is best-effort.
	docker := newEngine()
	svc := newService(docker, sshInfoReporter())

	ids := svc.Identity()

	hostKeyPath, signer, err := dcxssh.LoadHostKey(ids.WorkspaceID)
	if err != nil {
		return fmt.Errorf("host key: %w", err)
	}

	pinned, _ := dcxssh.HasHost(ids.WorkspaceID)

	info := sshInfo{
		Workspace:   ids.WorkspaceID,
		Host:        svc.SSHAlias(ctx),
		HostKey:     dcxssh.Fingerprint(signer),
		HostKeyFile: hostKeyPath,
		KnownPinned: pinned,
		AllowedIPs:  []string{"127.0.0.0/8", "::1/128"},
	}

	// Best-effort: the port and access recorded on the container.
	if _, ci, serr := svc.StateManager().State(ctx, ids.WorkspaceID); serr == nil && ci != nil {
		if port, perr := containerPkg.SSHHostPort(ctx, docker, ci); perr == nil {
			info.Port = port
			access := containerPkg.RecordedSSHAccess(ci)
			info.BindAddress = access.BindHost()
			info.AllowedIPs = append(info.AllowedIPs, access.AllowedCIDRs...)
		}
	}

	if sshInfoDoctor {
		return runSSHDoctor(ctx, info)
	}

	if sshInfoJSON {
		return printJSON(info)
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
		return fmt.Errorf("no SSH port found for this workspace (is the devcontainer up?)")
	}
	ui.Printf("  [%s] Listener published on %s:%d", pass, info.BindAddress, info.Port)

	// 2. TCP reachable?
	if err := tcpReachable(ctx, "127.0.0.1", info.Port); err != nil {
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

// sshInfoReporter discards progress output in --json mode.
func sshInfoReporter() report.Reporter {
	if sshInfoJSON {
		return report.Discard
	}
	return ui.Reporter()
}
