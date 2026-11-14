package cli

import (
	"context"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	doctorConfig bool
	doctorSystem bool
	doctorJSON   bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system requirements and configuration",
	Long: `Check that dcx can run here.

System checks: the Docker daemon (and whether you may use it), the docker
compose plugin, and whether Docker labels containers for SELinux.

Configuration checks (when a devcontainer.json is found, or with --config):
the config resolves exactly as 'dcx up' resolves it, and the files it
references exist. When the workspace's container is running, the SSH
checks probe its agent: the port is recorded, accepts connections, and
known_hosts pins the host key.`,
	RunE: runDoctor,
}

func init() {
	// Not --config: that is the global flag selecting the devcontainer.json.
	doctorCmd.Flags().BoolVar(&doctorConfig, "config-only", false, "only check configuration (skip system checks)")
	doctorCmd.Flags().BoolVar(&doctorSystem, "system", false, "only check system requirements (skip config checks)")
	// The flags select one mode; together they used to cancel out
	// (doctor then checked nothing and reported success).
	doctorCmd.MarkFlagsMutuallyExclusive("config-only", "system")
	doctorCmd.Flags().BoolVar(&doctorJSON, "json", false, "print machine-readable JSON instead of text")
}

// CheckResult represents a single check result.
type CheckResult struct {
	Section string `json:"section"` // "system" or "config"
	Name    string `json:"name"`
	OK      bool   `json:"ok"`
	Message string `json:"message"`
	Hint    string `json:"hint,omitempty"`
}

// doctorReport is the doctor --json output.
type doctorReport struct {
	OK     bool          `json:"ok"`
	Checks []CheckResult `json:"checks"`
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx, stop := commandContext()
	defer stop()

	// Config discovery is the same as `up`'s (including
	// .devcontainer/<folder>/devcontainer.json and --config).
	hasConfig := configPath != ""
	if !hasConfig {
		_, err := devcontainer.Resolve(workspacePath)
		hasConfig = err == nil
	}
	if doctorConfig && !hasConfig {
		return fmt.Errorf("no devcontainer.json found in %s", workspacePath)
	}

	engine := newEngine()
	var system, config []CheckResult
	if !doctorConfig {
		system = inSection("system", systemChecks(ctx, engine))
	}
	var sshChecks []CheckResult
	if !doctorSystem && hasConfig {
		config = inSection("config", configChecks(ctx, engine))
		sshChecks = inSection("ssh", workspaceSSHChecks(ctx, engine))
	}
	report := doctorReport{OK: true, Checks: append(append(append([]CheckResult{}, system...), config...), sshChecks...)}
	for _, c := range report.Checks {
		report.OK = report.OK && c.OK
	}

	if doctorJSON {
		if err := printJSON(report); err != nil {
			return err
		}
	} else {
		printChecks("System Requirements", system)
		printChecks("Configuration", config)
		printChecks("SSH", sshChecks)
		if report.OK {
			ui.Success("All checks passed!")
		}
	}
	if !report.OK {
		return fmt.Errorf("some checks failed")
	}
	return nil
}

// inSection labels results with their section.
func inSection(section string, results []CheckResult) []CheckResult {
	for i := range results {
		results[i].Section = section
	}
	return results
}

// printChecks prints a titled list of results.
func printChecks(title string, results []CheckResult) {
	if len(results) == 0 {
		return
	}
	ui.Println(ui.Bold(title))
	ui.Println(ui.Dim(strings.Repeat("=", len(title))))
	ui.Println("")
	for _, r := range results {
		check := ui.CheckResultPass
		if !r.OK {
			check = ui.CheckResultFail
		}
		ui.Println(ui.FormatCheck(check, fmt.Sprintf("%s: %s", r.Name, r.Message)))
		if !r.OK && r.Hint != "" {
			ui.Printf("    %s", ui.Dim(r.Hint))
		}
	}
	ui.Println("")
}

// systemChecks checks the Docker daemon, the compose plugin and SELinux.
func systemChecks(ctx context.Context, engine container.Engine) []CheckResult {
	info, err := engine.Info(ctx)
	if err != nil {
		hint := "Start Docker Desktop or the Docker daemon"
		if container.IsDaemonPermissionDenied(err.Error()) {
			hint = "The daemon is running; " + container.DaemonPermissionHint
		}
		return []CheckResult{{Name: "Docker", OK: false, Message: err.Error(), Hint: hint}}
	}
	results := []CheckResult{{Name: "Docker", OK: true, Message: "version " + info.ServerVersion}}

	if version, err := engine.ComposeVersion(ctx); err != nil {
		results = append(results, CheckResult{
			Name: "Docker Compose", OK: false, Message: "docker compose plugin not found",
			Hint: "Install the docker compose v2 plugin (dcx runs `docker compose`)",
		})
	} else {
		results = append(results, CheckResult{Name: "Docker Compose", OK: true, Message: "version " + version})
	}

	selinux := "not used by Docker"
	if info.SELinux {
		selinux = "Docker labels containers; dcx relabels the workspace mount (:z)"
	}
	return append(results, CheckResult{Name: "SELinux", OK: true, Message: selinux})
}

// configChecks resolves the configuration exactly as `dcx up` does (so
// doctor cannot pass a config up rejects) and checks the files it names.
func configChecks(ctx context.Context, engine container.Engine) []CheckResult {
	resolved, err := newService(engine, report.Discard).Load(ctx)
	if err != nil {
		return []CheckResult{{
			Name: "Config", OK: false, Message: err.Error(),
			Hint: "Run 'dcx config' to see the configuration as dcx reads it",
		}}
	}
	summary := fmt.Sprintf("%s (%s plan", resolved.ConfigPath, resolved.Plan.Type())
	if resolved.MetadataImage != "" {
		summary += ", base " + resolved.MetadataImage
	}
	if n := len(resolved.Features); n > 0 {
		summary += fmt.Sprintf(", %d feature(s)", n)
	}
	results := []CheckResult{{Name: "Config", OK: true, Message: summary + ")"}}

	var files []string
	switch plan := resolved.Plan.(type) {
	case *devcontainer.DockerfilePlan:
		files = append(files, plan.Dockerfile)
	case *devcontainer.ComposePlan:
		files = append(files, plan.Files...)
	}
	for _, f := range files {
		if _, err := os.Stat(f); err != nil {
			results = append(results, CheckResult{Name: "File", OK: false, Message: "not found: " + f})
		}
	}
	return results
}

// workspaceSSHChecks probes the agent of the workspace's running container
// (none when it isn't running).
func workspaceSSHChecks(ctx context.Context, engine container.Engine) []CheckResult {
	st, err := newService(engine, report.Discard).Status(ctx, false)
	if err != nil || st.Container == nil || !st.Container.Running || st.SSH == nil {
		return nil
	}
	ssh := st.SSH
	if ssh.Port == 0 {
		return []CheckResult{{Name: "Listener", OK: false, Message: "no SSH port recorded for the container", Hint: "Run 'dcx up --recreate'"}}
	}
	endpoint := fmt.Sprintf("%s:%d", ssh.BindAddress, ssh.Port)
	results := []CheckResult{{Name: "Listener", OK: true, Message: "published on " + endpoint}}
	if err := tcpReachable(ctx, "127.0.0.1", ssh.Port); err != nil {
		return append(results, CheckResult{Name: "Reachable", OK: false, Message: err.Error(), Hint: "Run 'dcx up' to restart the agent"})
	}
	results = append(results, CheckResult{Name: "Reachable", OK: true, Message: "the agent accepts connections"})
	if !ssh.KnownHostsPinned {
		return append(results, CheckResult{Name: "Host key", OK: false, Message: "known_hosts does not pin the workspace's host key", Hint: "Run 'dcx up' to pin it"})
	}
	return append(results, CheckResult{Name: "Host key", OK: true, Message: "pinned (" + ssh.HostKeyFingerprint + ")"})
}

// tcpReachable reports whether host:port accepts a TCP connection.
func tcpReachable(ctx context.Context, host string, port int) error {
	dialer := &net.Dialer{Timeout: 2 * time.Second}
	conn, err := dialer.DialContext(ctx, "tcp", net.JoinHostPort(host, strconv.Itoa(port)))
	if err != nil {
		return err
	}
	return conn.Close()
}
