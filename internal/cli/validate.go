package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/spf13/cobra"
)

var (
	validateOutputJSON bool
)

var validateCmd = &cobra.Command{
	Use:   "validate",
	Short: "Validate devcontainer configuration",
	Long: `Validate the devcontainer.json configuration without building or starting.

This command checks:
- JSON syntax and schema validity
- Required fields and values
- File references (Dockerfile, compose files)
- Feature references (syntax only, not network validation)
- Potential configuration issues

Exit codes:
  0 - Configuration is valid
  1 - Configuration has errors
  2 - Configuration has warnings (with --strict)

Examples:
  dcx validate              # Validate current directory
  dcx validate --json       # Output results as JSON
  dcx validate -p /path     # Validate specific workspace`,
	RunE: runValidate,
}

func init() {
	validateCmd.Flags().BoolVar(&validateOutputJSON, "json", false, "output results as JSON")
	rootCmd.AddCommand(validateCmd)
}

// ValidationResult contains the validation results.
type ValidationResult struct {
	Valid    bool               `json:"valid"`
	Errors   []ValidationIssue  `json:"errors,omitempty"`
	Warnings []ValidationIssue  `json:"warnings,omitempty"`
	Info     *ValidationInfo    `json:"info,omitempty"`
}

// ValidationIssue represents a validation error or warning.
type ValidationIssue struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Path    string `json:"path,omitempty"`
	Hint    string `json:"hint,omitempty"`
}

// ValidationInfo contains information about the validated configuration.
type ValidationInfo struct {
	ConfigPath     string   `json:"config_path"`
	PlanType       string   `json:"plan_type"`
	Image          string   `json:"image,omitempty"`
	Dockerfile     string   `json:"dockerfile,omitempty"`
	ComposeFiles   []string `json:"compose_files,omitempty"`
	Service        string   `json:"service,omitempty"`
	FeatureCount   int      `json:"feature_count"`
	LifecycleHooks []string `json:"lifecycle_hooks,omitempty"`
}

func runValidate(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	result := &ValidationResult{
		Valid:    true,
		Errors:   []ValidationIssue{},
		Warnings: []ValidationIssue{},
	}

	// Find configuration
	cfgPath := configPath
	if cfgPath == "" {
		cfgPath = findConfigPath(workspacePath)
		if cfgPath == "" {
			result.Valid = false
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "CONFIG_NOT_FOUND",
				Message: "devcontainer.json not found",
				Hint:    "Create a devcontainer.json file in .devcontainer/ directory",
			})
			return outputValidationResult(result)
		}
	}

	// Parse configuration
	cfg, err := config.ParseFile(cfgPath)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "PARSE_ERROR",
			Message: fmt.Sprintf("Failed to parse configuration: %v", err),
			Path:    cfgPath,
		})
		return outputValidationResult(result)
	}

	// Build workspace model (validates structure)
	builder := workspace.NewBuilder(nil)
	ws, err := builder.Build(ctx, workspace.BuildOptions{
		ConfigPath:    cfgPath,
		WorkspaceRoot: workspacePath,
		Config:        cfg,
	})
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, ValidationIssue{
			Code:    "BUILD_ERROR",
			Message: fmt.Sprintf("Configuration error: %v", err),
			Path:    cfgPath,
		})
		return outputValidationResult(result)
	}

	// Populate info
	result.Info = buildValidationInfo(ws)

	// Validate plan-specific requirements
	validatePlanRequirements(ws, result)

	// Validate file references
	validateFileReferences(ws, result)

	// Validate features syntax
	validateFeatures(cfg, result)

	// Validate lifecycle hooks
	validateLifecycleHooks(cfg, result)

	// Validate Docker connectivity (warning only)
	validateDockerConnectivity(result)

	// Update valid status based on errors
	result.Valid = len(result.Errors) == 0

	return outputValidationResult(result)
}

func findConfigPath(wsPath string) string {
	paths := []string{
		filepath.Join(wsPath, ".devcontainer", "devcontainer.json"),
		filepath.Join(wsPath, ".devcontainer.json"),
	}
	for _, p := range paths {
		if _, err := os.Stat(p); err == nil {
			return p
		}
	}
	return ""
}

func buildValidationInfo(ws *workspace.Workspace) *ValidationInfo {
	info := &ValidationInfo{
		ConfigPath:   ws.ConfigPath,
		PlanType:     string(ws.Resolved.PlanType),
		FeatureCount: len(ws.Resolved.Features),
	}

	switch ws.Resolved.PlanType {
	case workspace.PlanTypeImage:
		info.Image = ws.Resolved.Image
	case workspace.PlanTypeDockerfile:
		if ws.Resolved.Dockerfile != nil {
			info.Dockerfile = ws.Resolved.Dockerfile.Path
		}
	case workspace.PlanTypeCompose:
		if ws.Resolved.Compose != nil {
			info.ComposeFiles = ws.Resolved.Compose.Files
			info.Service = ws.Resolved.Compose.Service
		}
	}

	// Lifecycle hooks
	if ws.Resolved.Hooks != nil {
		hooks := ws.Resolved.Hooks
		if len(hooks.Initialize) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "initializeCommand")
		}
		if len(hooks.OnCreate) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "onCreateCommand")
		}
		if len(hooks.UpdateContent) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "updateContentCommand")
		}
		if len(hooks.PostCreate) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "postCreateCommand")
		}
		if len(hooks.PostStart) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "postStartCommand")
		}
		if len(hooks.PostAttach) > 0 {
			info.LifecycleHooks = append(info.LifecycleHooks, "postAttachCommand")
		}
	}

	return info
}

func validatePlanRequirements(ws *workspace.Workspace, result *ValidationResult) {
	switch ws.Resolved.PlanType {
	case workspace.PlanTypeImage:
		if ws.Resolved.Image == "" {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "MISSING_IMAGE",
				Message: "image-based configuration requires 'image' field",
				Hint:    "Add an 'image' field with a valid Docker image reference",
			})
		}

	case workspace.PlanTypeDockerfile:
		if ws.Resolved.Dockerfile == nil || ws.Resolved.Dockerfile.Path == "" {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "MISSING_DOCKERFILE",
				Message: "Dockerfile-based configuration requires 'build.dockerfile' field",
				Hint:    "Add a 'build' object with 'dockerfile' field",
			})
		}

	case workspace.PlanTypeCompose:
		if ws.Resolved.Compose == nil || len(ws.Resolved.Compose.Files) == 0 {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "MISSING_COMPOSE_FILE",
				Message: "compose-based configuration requires 'dockerComposeFile' field",
			})
		}
		if ws.Resolved.Compose != nil && ws.Resolved.Compose.Service == "" {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "MISSING_SERVICE",
				Message: "compose-based configuration requires 'service' field",
				Hint:    "Specify which service is the devcontainer",
			})
		}
	}
}

func validateFileReferences(ws *workspace.Workspace, result *ValidationResult) {
	// Check Dockerfile exists
	if ws.Resolved.Dockerfile != nil {
		if _, err := os.Stat(ws.Resolved.Dockerfile.Path); os.IsNotExist(err) {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "DOCKERFILE_NOT_FOUND",
				Message: fmt.Sprintf("Dockerfile not found: %s", ws.Resolved.Dockerfile.Path),
				Path:    ws.Resolved.Dockerfile.Path,
			})
		}
	}

	// Check compose files exist
	if ws.Resolved.Compose != nil {
		for _, f := range ws.Resolved.Compose.Files {
			if _, err := os.Stat(f); os.IsNotExist(err) {
				result.Errors = append(result.Errors, ValidationIssue{
					Code:    "COMPOSE_FILE_NOT_FOUND",
					Message: fmt.Sprintf("docker-compose file not found: %s", f),
					Path:    f,
				})
			}
		}
	}
}

func validateFeatures(cfg *config.DevcontainerConfig, result *ValidationResult) {
	if cfg.Features == nil {
		return
	}

	for featureRef := range cfg.Features {
		// Basic syntax validation
		if featureRef == "" {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "EMPTY_FEATURE_REF",
				Message: "Feature reference cannot be empty",
			})
			continue
		}

		// Check for common issues
		if featureRef[0] == '/' {
			result.Warnings = append(result.Warnings, ValidationIssue{
				Code:    "LOCAL_FEATURE_PATH",
				Message: fmt.Sprintf("Local feature path used: %s", featureRef),
				Hint:    "Local features may not be portable across machines",
			})
		}
	}
}

func validateLifecycleHooks(cfg *config.DevcontainerConfig, result *ValidationResult) {
	// Validate waitFor value
	if cfg.WaitFor != "" {
		validWaitFor := map[string]bool{
			"initializeCommand":    true,
			"onCreateCommand":      true,
			"updateContentCommand": true,
			"postCreateCommand":    true,
			"postStartCommand":     true,
		}
		if !validWaitFor[cfg.WaitFor] {
			result.Errors = append(result.Errors, ValidationIssue{
				Code:    "INVALID_WAIT_FOR",
				Message: fmt.Sprintf("Invalid waitFor value: %s", cfg.WaitFor),
				Hint:    "Valid values: initializeCommand, onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand",
			})
		}
	}
}

func validateDockerConnectivity(result *ValidationResult) {
	client, err := docker.NewClient()
	if err != nil {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Code:    "DOCKER_NOT_AVAILABLE",
			Message: "Docker is not available",
			Hint:    "Start Docker to build and run the devcontainer",
		})
		return
	}
	defer client.Close()

	// Try to ping Docker
	ctx := context.Background()
	if _, err := client.ServerVersion(ctx); err != nil {
		result.Warnings = append(result.Warnings, ValidationIssue{
			Code:    "DOCKER_NOT_RESPONDING",
			Message: "Docker daemon is not responding",
			Hint:    "Ensure Docker daemon is running",
		})
	}
}

func outputValidationResult(result *ValidationResult) error {
	if validateOutputJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		return enc.Encode(result)
	}

	// Human-readable output
	if result.Valid {
		fmt.Println("Configuration is valid")
		fmt.Println()
	} else {
		fmt.Println("Configuration has errors")
		fmt.Println()
	}

	// Show info
	if result.Info != nil {
		fmt.Printf("Config: %s\n", result.Info.ConfigPath)
		fmt.Printf("Type:   %s\n", result.Info.PlanType)
		if result.Info.Image != "" {
			fmt.Printf("Image:  %s\n", result.Info.Image)
		}
		if result.Info.Dockerfile != "" {
			fmt.Printf("Dockerfile: %s\n", result.Info.Dockerfile)
		}
		if len(result.Info.ComposeFiles) > 0 {
			fmt.Printf("Compose: %v\n", result.Info.ComposeFiles)
			fmt.Printf("Service: %s\n", result.Info.Service)
		}
		if result.Info.FeatureCount > 0 {
			fmt.Printf("Features: %d\n", result.Info.FeatureCount)
		}
		if len(result.Info.LifecycleHooks) > 0 {
			fmt.Printf("Lifecycle hooks: %v\n", result.Info.LifecycleHooks)
		}
		fmt.Println()
	}

	// Show errors
	if len(result.Errors) > 0 {
		fmt.Println("Errors:")
		for _, e := range result.Errors {
			fmt.Printf("  [%s] %s\n", e.Code, e.Message)
			if e.Path != "" {
				fmt.Printf("         Path: %s\n", e.Path)
			}
			if e.Hint != "" {
				fmt.Printf("         Hint: %s\n", e.Hint)
			}
		}
		fmt.Println()
	}

	// Show warnings
	if len(result.Warnings) > 0 {
		fmt.Println("Warnings:")
		for _, w := range result.Warnings {
			fmt.Printf("  [%s] %s\n", w.Code, w.Message)
			if w.Hint != "" {
				fmt.Printf("         Hint: %s\n", w.Hint)
			}
		}
		fmt.Println()
	}

	// Exit with error code if invalid
	if !result.Valid {
		os.Exit(1)
	}

	return nil
}
