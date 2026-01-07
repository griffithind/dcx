package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var inspectOutputJSON bool

var inspectCmd = &cobra.Command{
	Use:   "inspect",
	Short: "Show detailed environment information",
	Long: `Display comprehensive details about the devcontainer environment.

Shows container information, resolved configuration, labels, mounts,
ports, and other runtime details useful for debugging.`,
	RunE: runInspect,
}

// InspectOutput represents the JSON output structure.
type InspectOutput struct {
	State       string                 `json:"state"`
	EnvKey      string                 `json:"env_key"`
	ConfigHash  string                 `json:"config_hash"`
	ProjectName string                 `json:"project_name,omitempty"`
	PlanType    string                 `json:"plan_type"`
	Container   *ContainerDetails      `json:"container,omitempty"`
	Config      *ConfigSummary         `json:"config,omitempty"`
	Labels      map[string]string      `json:"labels,omitempty"`
}

// ContainerDetails contains container-specific information.
type ContainerDetails struct {
	ID         string            `json:"id"`
	Name       string            `json:"name"`
	Image      string            `json:"image"`
	Status     string            `json:"status"`
	Running    bool              `json:"running"`
	Created    string            `json:"created"`
	Ports      []string          `json:"ports,omitempty"`
	Mounts     []string          `json:"mounts,omitempty"`
	Env        []string          `json:"env,omitempty"`
	WorkingDir string            `json:"working_dir,omitempty"`
	User       string            `json:"user,omitempty"`
}

// ConfigSummary contains key configuration details.
type ConfigSummary struct {
	Image                string            `json:"image,omitempty"`
	Build                *BuildSummary     `json:"build,omitempty"`
	DockerComposeFile    []string          `json:"docker_compose_file,omitempty"`
	Service              string            `json:"service,omitempty"`
	RemoteUser           string            `json:"remote_user,omitempty"`
	ContainerUser        string            `json:"container_user,omitempty"`
	WorkspaceFolder      string            `json:"workspace_folder,omitempty"`
	Features             []string          `json:"features,omitempty"`
	ForwardPorts         []interface{}     `json:"forward_ports,omitempty"`
	OverrideCommand      *bool             `json:"override_command,omitempty"`
	ShutdownAction       string            `json:"shutdown_action,omitempty"`
	UpdateRemoteUserUID  *bool             `json:"update_remote_user_uid,omitempty"`
}

// BuildSummary contains build configuration summary.
type BuildSummary struct {
	Dockerfile string `json:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty"`
	Target     string `json:"target,omitempty"`
}

func runInspect(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load configuration
	cfg, cfgPath, err := config.Load(workspacePath, configPath)
	if err != nil {
		return fmt.Errorf("failed to load configuration: %w", err)
	}

	// Load dcx.json for project name
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
	}

	// Compute identifiers
	envKey := state.ComputeEnvKey(workspacePath)
	configHash, _ := config.ComputeHash(cfg)

	// Get state
	stateMgr := state.NewManager(dockerClient)
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	// Build output structure
	output := InspectOutput{
		State:       string(currentState),
		EnvKey:      envKey,
		ConfigHash:  configHash,
		ProjectName: projectName,
		PlanType:    getPlanType(cfg),
	}

	// Add container details if available
	if containerInfo != nil {
		// Get full container details
		fullContainer, err := dockerClient.InspectContainer(ctx, containerInfo.ID)
		if err == nil {
			output.Container = &ContainerDetails{
				ID:      containerInfo.ID[:12],
				Name:    containerInfo.Name,
				Image:   fullContainer.Image,
				Status:  containerInfo.Status,
				Running: containerInfo.Running,
				Created: fullContainer.Created.Format("2006-01-02 15:04:05"),
			}
		} else {
			output.Container = &ContainerDetails{
				ID:      containerInfo.ID[:12],
				Name:    containerInfo.Name,
				Status:  containerInfo.Status,
				Running: containerInfo.Running,
			}
		}
		output.Labels = containerInfo.Labels.ToMap()
	}

	// Add config summary
	output.Config = buildConfigSummary(cfg, cfgPath)

	if inspectOutputJSON {
		data, err := json.MarshalIndent(output, "", "  ")
		if err != nil {
			return fmt.Errorf("failed to marshal output: %w", err)
		}
		fmt.Println(string(data))
	} else {
		printInspectOutput(output)
	}

	return nil
}

func getPlanType(cfg *config.DevcontainerConfig) string {
	if cfg.IsComposePlan() {
		return "compose"
	}
	return "single"
}

func buildConfigSummary(cfg *config.DevcontainerConfig, cfgPath string) *ConfigSummary {
	summary := &ConfigSummary{
		Image:               cfg.Image,
		Service:             cfg.Service,
		RemoteUser:          cfg.RemoteUser,
		ContainerUser:       cfg.ContainerUser,
		WorkspaceFolder:     cfg.WorkspaceFolder,
		OverrideCommand:     cfg.OverrideCommand,
		ShutdownAction:      cfg.ShutdownAction,
		UpdateRemoteUserUID: cfg.UpdateRemoteUserUID,
	}

	if cfg.Build != nil {
		summary.Build = &BuildSummary{
			Dockerfile: cfg.Build.Dockerfile,
			Context:    cfg.Build.Context,
			Target:     cfg.Build.Target,
		}
	}

	if composeFiles := cfg.GetDockerComposeFiles(); len(composeFiles) > 0 {
		summary.DockerComposeFile = composeFiles
	}

	// List features
	for featureID := range cfg.Features {
		summary.Features = append(summary.Features, featureID)
	}

	// Forward ports
	if len(cfg.ForwardPorts) > 0 {
		summary.ForwardPorts = cfg.ForwardPorts
	}

	return summary
}

func printInspectOutput(output InspectOutput) {
	fmt.Println("Environment Information")
	fmt.Println("=======================")
	fmt.Println()

	fmt.Printf("State:        %s\n", output.State)
	fmt.Printf("Env Key:      %s\n", output.EnvKey)
	fmt.Printf("Config Hash:  %s\n", output.ConfigHash[:12])
	if output.ProjectName != "" {
		fmt.Printf("Project Name: %s\n", output.ProjectName)
	}
	fmt.Printf("Plan Type:    %s\n", output.PlanType)
	fmt.Println()

	if output.Container != nil {
		fmt.Println("Container")
		fmt.Println("---------")
		fmt.Printf("  ID:      %s\n", output.Container.ID)
		fmt.Printf("  Name:    %s\n", output.Container.Name)
		if output.Container.Image != "" {
			fmt.Printf("  Image:   %s\n", output.Container.Image)
		}
		fmt.Printf("  Status:  %s\n", output.Container.Status)
		fmt.Printf("  Running: %t\n", output.Container.Running)
		if output.Container.Created != "" {
			fmt.Printf("  Created: %s\n", output.Container.Created)
		}
		fmt.Println()
	}

	if output.Config != nil {
		fmt.Println("Configuration")
		fmt.Println("-------------")
		if output.Config.Image != "" {
			fmt.Printf("  Image: %s\n", output.Config.Image)
		}
		if output.Config.Build != nil {
			fmt.Printf("  Build:\n")
			if output.Config.Build.Dockerfile != "" {
				fmt.Printf("    Dockerfile: %s\n", output.Config.Build.Dockerfile)
			}
			if output.Config.Build.Context != "" {
				fmt.Printf("    Context: %s\n", output.Config.Build.Context)
			}
		}
		if len(output.Config.DockerComposeFile) > 0 {
			fmt.Printf("  Compose Files: %s\n", strings.Join(output.Config.DockerComposeFile, ", "))
		}
		if output.Config.Service != "" {
			fmt.Printf("  Service: %s\n", output.Config.Service)
		}
		if output.Config.RemoteUser != "" {
			fmt.Printf("  Remote User: %s\n", output.Config.RemoteUser)
		}
		if output.Config.WorkspaceFolder != "" {
			fmt.Printf("  Workspace Folder: %s\n", output.Config.WorkspaceFolder)
		}
		if len(output.Config.Features) > 0 {
			fmt.Printf("  Features: %s\n", strings.Join(output.Config.Features, ", "))
		}
		if output.Config.OverrideCommand != nil {
			fmt.Printf("  Override Command: %t\n", *output.Config.OverrideCommand)
		}
		if output.Config.ShutdownAction != "" {
			fmt.Printf("  Shutdown Action: %s\n", output.Config.ShutdownAction)
		}
		if output.Config.UpdateRemoteUserUID != nil {
			fmt.Printf("  Update Remote User UID: %t\n", *output.Config.UpdateRemoteUserUID)
		}
		fmt.Println()
	}

	if len(output.Labels) > 0 {
		fmt.Println("Labels")
		fmt.Println("------")
		for k, v := range output.Labels {
			// Only show dcx labels
			if strings.HasPrefix(k, "dcx.") {
				fmt.Printf("  %s: %s\n", k, v)
			}
		}
	}
}

func init() {
	inspectCmd.Flags().BoolVar(&inspectOutputJSON, "json", false, "output as JSON")
	rootCmd.AddCommand(inspectCmd)
}
