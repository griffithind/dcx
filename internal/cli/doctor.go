package cli

import (
	"context"
	"fmt"
	"github.com/griffithind/dcx/internal/report"
	"github.com/griffithind/dcx/internal/service"
	"os"
	"os/exec"
	"runtime"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/selinux"
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
	Long: `Check that all system requirements are met for running dcx.

By default checks both system and configuration. Use flags to check only one:

System checks:
- Docker daemon connectivity
- Docker Compose availability
- SELinux status (Linux only)

Configuration checks (with --config or by default if devcontainer.json exists):
- JSON syntax and schema validity
- Required fields and values
- File references (Dockerfile, compose files)
- Feature references syntax`,
	RunE: runDoctor,
}

func init() {
	// Not --config: that is the global flag selecting the devcontainer.json.
	doctorCmd.Flags().BoolVar(&doctorConfig, "config-only", false, "only check configuration (skip system checks)")
	doctorCmd.Flags().BoolVar(&doctorSystem, "system", false, "only check system requirements (skip config checks)")
}

// CheckResult represents a single check result.
type CheckResult struct {
	Name    string
	OK      bool
	Message string
	Hint    string
}

func runDoctor(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Determine what to check
	checkSystemReqs := !doctorConfig
	checkConfig := !doctorSystem

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

	var systemResults []CheckResult
	var configResults []CheckResult
	allOK := true

	// System checks
	if checkSystemReqs {
		systemResults = append(systemResults, checkDocker(ctx))
		systemResults = append(systemResults, checkCompose())
		if runtime.GOOS == "linux" {
			systemResults = append(systemResults, checkSELinux())
		}

		for _, r := range systemResults {
			if !r.OK {
				allOK = false
			}
		}
	}

	// Configuration checks (if config exists or --config flag)
	if checkConfig {
		if hasConfig {
			configResults = runConfigChecks(ctx)
			for _, r := range configResults {
				if !r.OK {
					allOK = false
				}
			}
		}
	}

	// Text output mode
	if len(systemResults) > 0 {
		ui.Println(ui.Bold("System Requirements"))
		ui.Println(ui.Dim("=================="))
		ui.Println("")

		for _, r := range systemResults {
			var checkResult ui.CheckResult
			if r.OK {
				checkResult = ui.CheckResultPass
			} else {
				checkResult = ui.CheckResultFail
			}
			ui.Println(ui.FormatCheck(checkResult, fmt.Sprintf("%s: %s", r.Name, r.Message)))
			if !r.OK && r.Hint != "" {
				ui.Printf("    %s", ui.Dim(r.Hint))
			}
		}
		ui.Println("")
	}

	if len(configResults) > 0 {
		ui.Println(ui.Bold("Configuration"))
		ui.Println(ui.Dim("============="))
		ui.Println("")

		for _, r := range configResults {
			var checkResult ui.CheckResult
			if r.OK {
				checkResult = ui.CheckResultPass
			} else {
				checkResult = ui.CheckResultFail
			}
			ui.Println(ui.FormatCheck(checkResult, fmt.Sprintf("%s: %s", r.Name, r.Message)))
			if !r.OK && r.Hint != "" {
				ui.Printf("    %s", ui.Dim(r.Hint))
			}
		}
		ui.Println("")
	}

	if allOK {
		ui.Success("All checks passed!")
		return nil
	}

	return fmt.Errorf("some checks failed")
}

// runConfigChecks validates the configuration through the same loading
// and resolution `dcx up` uses, so doctor cannot pass a config up rejects
// (or report values up would not use).
func runConfigChecks(ctx context.Context) []CheckResult {
	var results []CheckResult

	cfg, cfgPath, err := devcontainer.Load(workspacePath, configPath)
	if cfgPath == "" {
		return append(results, CheckResult{
			Name:    "Config File",
			OK:      false,
			Message: fmt.Sprintf("%v", err),
			Hint:    "Create a devcontainer.json file in .devcontainer/ directory",
		})
	}
	results = append(results, CheckResult{Name: "Config File", OK: true, Message: cfgPath})
	if err != nil {
		return append(results, CheckResult{
			Name:    "Config Syntax",
			OK:      false,
			Message: fmt.Sprintf("parse error: %v", err),
		})
	}
	results = append(results, CheckResult{Name: "Config Syntax", OK: true, Message: "valid JSON"})

	svc := service.NewDevContainerService(container.NewDockerCLI(), report.Discard, workspacePath, configPath, verbose)
	resolved, err := svc.Load(ctx)
	if err != nil {
		return append(results, CheckResult{
			Name:    "Config Structure",
			OK:      false,
			Message: fmt.Sprintf("structure error: %v", err),
		})
	}

	planType := ""
	if resolved.Plan != nil {
		planType = string(resolved.Plan.Type())
	}
	results = append(results, CheckResult{
		Name:    "Config Structure",
		OK:      true,
		Message: fmt.Sprintf("plan type: %s", planType),
	})

	// Validate plan-specific requirements
	results = append(results, validatePlanRequirements(resolved)...)

	// Validate file references
	results = append(results, validateFileReferences(resolved)...)

	// Validate features
	results = append(results, validateFeatures(cfg)...)

	return results
}

func validatePlanRequirements(resolved *devcontainer.ResolvedDevContainer) []CheckResult {
	var results []CheckResult

	if resolved.Plan == nil {
		return results
	}

	switch resolved.Plan.Type() {
	case devcontainer.PlanTypeImage:
		if resolved.BaseImage == "" {
			results = append(results, CheckResult{
				Name:    "Image",
				OK:      false,
				Message: "missing 'image' field",
				Hint:    "Add an 'image' field with a valid Docker image reference",
			})
		} else {
			results = append(results, CheckResult{
				Name:    "Image",
				OK:      true,
				Message: resolved.BaseImage,
			})
		}

	case devcontainer.PlanTypeDockerfile:
		dfPlan, ok := resolved.Plan.(*devcontainer.DockerfilePlan)
		if !ok || dfPlan.Dockerfile == "" {
			results = append(results, CheckResult{
				Name:    "Dockerfile",
				OK:      false,
				Message: "missing 'build.dockerfile' field",
			})
		} else {
			results = append(results, CheckResult{
				Name:    "Dockerfile",
				OK:      true,
				Message: dfPlan.Dockerfile,
			})
		}

	case devcontainer.PlanTypeCompose:
		composePlan, ok := resolved.Plan.(*devcontainer.ComposePlan)
		if !ok || len(composePlan.Files) == 0 {
			results = append(results, CheckResult{
				Name:    "Compose Files",
				OK:      false,
				Message: "missing 'dockerComposeFile' field",
			})
		} else {
			results = append(results, CheckResult{
				Name:    "Compose Files",
				OK:      true,
				Message: fmt.Sprintf("%d file(s)", len(composePlan.Files)),
			})
		}

		if composePlan != nil && composePlan.Service == "" {
			results = append(results, CheckResult{
				Name:    "Service",
				OK:      false,
				Message: "missing 'service' field",
				Hint:    "Specify which service is the devcontainer",
			})
		} else if composePlan != nil {
			results = append(results, CheckResult{
				Name:    "Service",
				OK:      true,
				Message: composePlan.Service,
			})
		}
	}

	return results
}

func validateFileReferences(resolved *devcontainer.ResolvedDevContainer) []CheckResult {
	var results []CheckResult

	if resolved.Plan == nil {
		return results
	}

	// Check Dockerfile exists
	if dfPlan, ok := resolved.Plan.(*devcontainer.DockerfilePlan); ok && dfPlan.Dockerfile != "" {
		if _, err := os.Stat(dfPlan.Dockerfile); os.IsNotExist(err) {
			results = append(results, CheckResult{
				Name:    "Dockerfile Exists",
				OK:      false,
				Message: fmt.Sprintf("not found: %s", dfPlan.Dockerfile),
			})
		} else {
			results = append(results, CheckResult{
				Name:    "Dockerfile Exists",
				OK:      true,
				Message: "found",
			})
		}
	}

	// Check compose files exist
	if composePlan, ok := resolved.Plan.(*devcontainer.ComposePlan); ok {
		for _, f := range composePlan.Files {
			if _, err := os.Stat(f); os.IsNotExist(err) {
				results = append(results, CheckResult{
					Name:    "Compose File",
					OK:      false,
					Message: fmt.Sprintf("not found: %s", f),
				})
			}
		}
	}

	return results
}

func validateFeatures(cfg *devcontainer.DevContainerConfig) []CheckResult {
	var results []CheckResult

	if len(cfg.Features) == 0 {
		return results
	}

	validFeatures := 0
	for featureRef := range cfg.Features {
		if featureRef == "" {
			results = append(results, CheckResult{
				Name:    "Feature",
				OK:      false,
				Message: "empty feature reference",
			})
			continue
		}
		validFeatures++
	}

	if validFeatures > 0 {
		results = append(results, CheckResult{
			Name:    "Features",
			OK:      true,
			Message: fmt.Sprintf("%d feature(s) configured", validFeatures),
		})
	}

	return results
}

func checkDocker(ctx context.Context) CheckResult {
	client, err := container.NewDocker()
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      false,
			Message: fmt.Sprintf("failed to connect: %v", err),
			Hint:    "Start Docker Desktop or the Docker daemon",
		}
	}

	if err := client.Ping(ctx); err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      false,
			Message: fmt.Sprintf("daemon not responding: %v", err),
			Hint:    "Ensure Docker daemon is running",
		}
	}

	version, err := client.ServerVersion(ctx)
	if err != nil {
		return CheckResult{
			Name:    "Docker",
			OK:      true,
			Message: "connected (version unknown)",
		}
	}

	return CheckResult{
		Name:    "Docker",
		OK:      true,
		Message: fmt.Sprintf("version %s", version),
	}
}

func checkCompose() CheckResult {
	// Check for docker compose (v2 plugin)
	cmd := exec.Command("docker", "compose", "version", "--short")
	cmdOutput, err := cmd.Output()
	if err == nil {
		return CheckResult{
			Name:    "Docker Compose",
			OK:      true,
			Message: fmt.Sprintf("version %s", string(cmdOutput[:len(cmdOutput)-1])),
		}
	}

	// Check for docker-compose (standalone)
	cmd = exec.Command("docker-compose", "version", "--short")
	cmdOutput, err = cmd.Output()
	if err == nil {
		return CheckResult{
			Name:    "Docker Compose",
			OK:      true,
			Message: fmt.Sprintf("version %s (standalone)", string(cmdOutput[:len(cmdOutput)-1])),
		}
	}

	return CheckResult{
		Name:    "Docker Compose",
		OK:      false,
		Message: "not found",
		Hint:    "Install docker compose plugin or docker-compose",
	}
}

func checkSELinux() CheckResult {
	mode, err := selinux.GetMode()
	if err != nil {
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "not available (expected on non-SELinux systems)",
		}
	}

	switch mode {
	case selinux.ModeEnforcing:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "enforcing (will use :Z bind mount option)",
		}
	case selinux.ModePermissive:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "permissive",
		}
	case selinux.ModeDisabled:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: "disabled",
		}
	default:
		return CheckResult{
			Name:    "SELinux",
			OK:      true,
			Message: fmt.Sprintf("mode: %s", mode),
		}
	}
}
