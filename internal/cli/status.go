package cli

import (
	"context"
	"fmt"
	"strings"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
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

// StatusOutput represents status information for JSON output.
type StatusOutput struct {
	Workspace   string            `json:"workspace"`
	Project     string            `json:"project,omitempty"`
	EnvKey      string            `json:"envKey"`
	State       string            `json:"state"`
	ConfigHash  string            `json:"configHash,omitempty"`
	PlanType    string            `json:"planType,omitempty"`
	SSH         *SSHStatus        `json:"ssh,omitempty"`
	Shortcuts   int               `json:"shortcuts,omitempty"`
	Container   *ContainerOutput  `json:"container,omitempty"`
	Config      *ConfigSummary    `json:"config,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
}

// SSHStatus represents SSH configuration status.
type SSHStatus struct {
	Configured bool   `json:"configured"`
	Host       string `json:"host,omitempty"`
}

// ContainerOutput represents container info for JSON output.
type ContainerOutput struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	Status     string `json:"status"`
	Image      string `json:"image,omitempty"`
	Running    bool   `json:"running,omitempty"`
	Created    string `json:"created,omitempty"`
	ConfigHash string `json:"configHash,omitempty"`
}

// ConfigSummary contains key configuration details.
type ConfigSummary struct {
	Image             string        `json:"image,omitempty"`
	Build             *BuildSummary `json:"build,omitempty"`
	DockerComposeFile []string      `json:"dockerComposeFile,omitempty"`
	Service           string        `json:"service,omitempty"`
	RemoteUser        string        `json:"remoteUser,omitempty"`
	WorkspaceFolder   string        `json:"workspaceFolder,omitempty"`
	Features          []string      `json:"features,omitempty"`
}

// BuildSummary contains build configuration summary.
type BuildSummary struct {
	Dockerfile string `json:"dockerfile,omitempty"`
	Context    string `json:"context,omitempty"`
	Target     string `json:"target,omitempty"`
}

func runStatus(cmd *cobra.Command, args []string) error {
	ctx := context.Background()
	out := output.Global()
	c := out.Color()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json configuration (optional)
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)

	// Compute workspace ID
	envKey := workspace.ComputeID(workspacePath)

	// Try to load config and compute hash for staleness detection
	var currentState state.State
	var containerInfo *state.ContainerInfo
	var cfg *config.DevcontainerConfig
	var configHash string

	cfg, _, err = config.Load(workspacePath, configPath)
	if err == nil {
		// Config exists, check for staleness
		// Use simple hash of raw JSON to match workspace builder
		if raw := cfg.GetRawJSON(); len(raw) > 0 {
			configHash = config.ComputeSimpleHash(raw)
		}
		if configHash != "" {
			currentState, containerInfo, err = stateMgr.GetStateWithProjectAndHash(ctx, projectName, envKey, configHash)
		} else {
			currentState, containerInfo, err = stateMgr.GetStateWithProject(ctx, projectName, envKey)
		}
	} else {
		// No config or error loading it, just get basic state
		currentState, containerInfo, err = stateMgr.GetStateWithProject(ctx, projectName, envKey)
	}

	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	// JSON output mode
	if out.IsJSON() {
		status := buildStatusOutput(ctx, dockerClient, workspacePath, projectName, envKey,
			currentState, containerInfo, cfg, configHash, dcxCfg, statusDetailed)
		return out.JSON(status)
	}

	// Text output mode
	kv := output.NewKeyValueTable(out.Writer())
	kv.Add("Workspace", c.Code(workspacePath))
	if projectName != "" {
		kv.Add("Project", projectName)
	}
	kv.Add("Env Key", envKey)
	kv.Add("State", formatState(currentState))

	// Show SSH status
	if containerInfo != nil && ssh.HasSSHConfig(containerInfo.Name) {
		sshHost := envKey
		if projectName != "" {
			sshHost = projectName
		}
		kv.Add("SSH", c.Code(fmt.Sprintf("ssh %s.dcx", sshHost)))
	} else if currentState != state.StateAbsent {
		kv.Add("SSH", c.Dim("not configured (use 'dcx up --ssh' to enable)"))
	}

	// Show shortcuts count
	if dcxCfg != nil && len(dcxCfg.Shortcuts) > 0 {
		kv.Add("Shortcuts", fmt.Sprintf("%d defined (use 'dcx run --list' to view)", len(dcxCfg.Shortcuts)))
	}

	kv.Render()

	// Container details
	if containerInfo != nil {
		out.Println()
		out.Println(c.Header("Primary Container"))

		cKV := output.NewKeyValueTable(out.Writer())
		cKV.Add("  ID", containerInfo.ID[:12])
		cKV.Add("  Name", containerInfo.Name)
		cKV.Add("  Status", containerInfo.Status)
		if containerInfo.ConfigHash != "" {
			cKV.Add("  Config", containerInfo.ConfigHash[:12])
		}
		cKV.Render()

		// Detailed mode: show more container info
		if statusDetailed {
			fullContainer, inspectErr := dockerClient.InspectContainer(ctx, containerInfo.ID)
			if inspectErr == nil {
				out.Println()
				out.Println(c.Header("Container Details"))
				dKV := output.NewKeyValueTable(out.Writer())
				dKV.Add("  Image", fullContainer.Image)
				dKV.Add("  Created", fullContainer.Created.Format("2006-01-02 15:04:05"))
				dKV.Add("  Running", fmt.Sprintf("%t", containerInfo.Running))
				dKV.Render()
			}
		}
	}

	// Detailed mode: show configuration
	if statusDetailed && cfg != nil {
		out.Println()
		out.Println(c.Header("Configuration"))
		cfgKV := output.NewKeyValueTable(out.Writer())
		if cfg.Image != "" {
			cfgKV.Add("  Image", cfg.Image)
		}
		if cfg.Build != nil {
			if cfg.Build.Dockerfile != "" {
				cfgKV.Add("  Dockerfile", cfg.Build.Dockerfile)
			}
			if cfg.Build.Context != "" {
				cfgKV.Add("  Build Context", cfg.Build.Context)
			}
		}
		if composeFiles := cfg.GetDockerComposeFiles(); len(composeFiles) > 0 {
			cfgKV.Add("  Compose Files", strings.Join(composeFiles, ", "))
		}
		if cfg.Service != "" {
			cfgKV.Add("  Service", cfg.Service)
		}
		if cfg.RemoteUser != "" {
			cfgKV.Add("  Remote User", cfg.RemoteUser)
		}
		if cfg.WorkspaceFolder != "" {
			cfgKV.Add("  Workspace Folder", cfg.WorkspaceFolder)
		}
		if len(cfg.Features) > 0 {
			featureList := make([]string, 0, len(cfg.Features))
			for f := range cfg.Features {
				featureList = append(featureList, f)
			}
			cfgKV.Add("  Features", strings.Join(featureList, ", "))
		}
		if configHash != "" {
			cfgKV.Add("  Config Hash", configHash[:12])
		}
		cfgKV.Render()
	}

	// Detailed mode: show labels
	if statusDetailed && containerInfo != nil {
		labelMap := containerInfo.Labels.ToMap()
		dcxLabels := make(map[string]string)
		for k, v := range labelMap {
			if strings.HasPrefix(k, "dcx.") {
				dcxLabels[k] = v
			}
		}
		if len(dcxLabels) > 0 {
			out.Println()
			out.Println(c.Header("Labels"))
			for k, v := range dcxLabels {
				out.Printf("  %s: %s", c.Dim(k), v)
			}
		}
	}

	return nil
}

// buildStatusOutput builds the JSON output structure.
func buildStatusOutput(ctx context.Context, dockerClient *docker.Client,
	workspacePath, projectName, envKey string,
	currentState state.State, containerInfo *state.ContainerInfo,
	cfg *config.DevcontainerConfig, configHash string,
	dcxCfg *config.DcxConfig, detailed bool) StatusOutput {

	status := StatusOutput{
		Workspace: workspacePath,
		Project:   projectName,
		EnvKey:    envKey,
		State:     string(currentState),
	}

	if detailed && configHash != "" {
		status.ConfigHash = configHash
	}

	if detailed && cfg != nil {
		if cfg.IsComposePlan() {
			status.PlanType = "compose"
		} else {
			status.PlanType = "single"
		}
	}

	// SSH status
	if containerInfo != nil && ssh.HasSSHConfig(containerInfo.Name) {
		sshHost := envKey
		if projectName != "" {
			sshHost = projectName
		}
		status.SSH = &SSHStatus{
			Configured: true,
			Host:       sshHost + ".dcx",
		}
	} else if currentState != state.StateAbsent {
		status.SSH = &SSHStatus{
			Configured: false,
		}
	}

	// Shortcuts
	if dcxCfg != nil && len(dcxCfg.Shortcuts) > 0 {
		status.Shortcuts = len(dcxCfg.Shortcuts)
	}

	// Container info
	if containerInfo != nil {
		status.Container = &ContainerOutput{
			ID:     containerInfo.ID[:12],
			Name:   containerInfo.Name,
			Status: containerInfo.Status,
		}
		if len(containerInfo.ConfigHash) > 12 {
			status.Container.ConfigHash = containerInfo.ConfigHash[:12]
		}

		// Detailed mode: more container info
		if detailed {
			status.Container.Running = containerInfo.Running
			fullContainer, err := dockerClient.InspectContainer(ctx, containerInfo.ID)
			if err == nil {
				status.Container.Image = fullContainer.Image
				status.Container.Created = fullContainer.Created.Format("2006-01-02 15:04:05")
			}

			// Labels
			labelMap := containerInfo.Labels.ToMap()
			status.Labels = make(map[string]string)
			for k, v := range labelMap {
				if strings.HasPrefix(k, "dcx.") {
					status.Labels[k] = v
				}
			}
		}
	}

	// Config summary for detailed mode
	if detailed && cfg != nil {
		status.Config = &ConfigSummary{
			Image:           cfg.Image,
			Service:         cfg.Service,
			RemoteUser:      cfg.RemoteUser,
			WorkspaceFolder: cfg.WorkspaceFolder,
		}
		if cfg.Build != nil {
			status.Config.Build = &BuildSummary{
				Dockerfile: cfg.Build.Dockerfile,
				Context:    cfg.Build.Context,
				Target:     cfg.Build.Target,
			}
		}
		if composeFiles := cfg.GetDockerComposeFiles(); len(composeFiles) > 0 {
			status.Config.DockerComposeFile = composeFiles
		}
		for f := range cfg.Features {
			status.Config.Features = append(status.Config.Features, f)
		}
	}

	return status
}

// formatState returns a colored state string.
func formatState(s state.State) string {
	c := output.Color()
	stateStr := string(s)
	switch s {
	case state.StateRunning:
		return c.StateRunning(stateStr)
	case state.StateCreated:
		return c.StateStopped(stateStr)
	case state.StateStale:
		return c.Warning(stateStr)
	case state.StateBroken:
		return c.StateError(stateStr)
	case state.StateAbsent:
		return c.StateUnknown(stateStr)
	default:
		return stateStr
	}
}
