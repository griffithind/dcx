package cli

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/ui"
	"github.com/spf13/cobra"
)

var (
	doctorConfig bool
	doctorSystem bool
)

var doctorCmd = &cobra.Command{
	Use:   "doctor",
	Short: "Check system requirements and configuration",
	Long: `Check that dcx can run here.

System checks: the Docker daemon (and whether you may use it), the docker
compose plugin, and whether Docker labels containers for SELinux.

Configuration checks (when a devcontainer.json is found, or with --config):
the config resolves exactly as 'dcx up' resolves it, and the files it
references exist.`,
	RunE: runDoctor,
}

func init() {
	// Not --config: that is the global flag selecting the devcontainer.json.
	doctorCmd.Flags().BoolVar(&doctorConfig, "config-only", false, "only check configuration (skip system checks)")
	doctorCmd.Flags().BoolVar(&doctorSystem, "system", false, "only check system requirements (skip config checks)")
	// The flags select one mode; together they used to cancel out
	// (doctor then checked nothing and reported success).
	doctorCmd.MarkFlagsMutuallyExclusive("config-only", "system")
}

// CheckResult represents a single check result.
type CheckResult struct {
	Name    string
	OK      bool
	Message string
	Hint    string
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
	allOK := true
	if !doctorConfig {
		allOK = printChecks("System Requirements", systemChecks(ctx, engine)) && allOK
	}
	if !doctorSystem && hasConfig {
		allOK = printChecks("Configuration", configChecks(ctx, engine)) && allOK
	}

	if allOK {
		ui.Success("All checks passed!")
		return nil
	}
	return fmt.Errorf("some checks failed")
}

// printChecks prints a titled list of results and reports whether all of
// them passed.
func printChecks(title string, results []CheckResult) bool {
	if len(results) == 0 {
		return true
	}
	ui.Println(ui.Bold(title))
	ui.Println(ui.Dim(strings.Repeat("=", len(title))))
	ui.Println("")
	allOK := true
	for _, r := range results {
		check := ui.CheckResultPass
		if !r.OK {
			check = ui.CheckResultFail
			allOK = false
		}
		ui.Println(ui.FormatCheck(check, fmt.Sprintf("%s: %s", r.Name, r.Message)))
		if !r.OK && r.Hint != "" {
			ui.Printf("    %s", ui.Dim(r.Hint))
		}
	}
	ui.Println("")
	return allOK
}

// systemChecks checks the Docker daemon, the compose plugin and SELinux.
func systemChecks(ctx context.Context, engine container.Engine) []CheckResult {
	info, err := engine.Info(ctx)
	if err != nil {
		hint := "Start Docker Desktop or the Docker daemon"
		if strings.Contains(err.Error(), "permission denied") {
			hint = "The daemon is running but your user may not use it: add it to the docker group (then log in again), or run dcx under 'sg docker'"
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
