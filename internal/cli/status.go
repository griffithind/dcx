package cli

import (
	"context"
	"fmt"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
)

var statusCmd = &cobra.Command{
	Use:   "status",
	Short: "Show devcontainer status",
	Long: `Show the current state of the devcontainer environment.

This command queries Docker for containers managed by dcx and displays
their current state (ABSENT, CREATED, RUNNING, STALE, or BROKEN).

This is an offline-safe command that does not require network access.`,
	RunE: runStatus,
}

// StatusOutput represents status information for JSON output.
type StatusOutput struct {
	Workspace   string           `json:"workspace"`
	Project     string           `json:"project,omitempty"`
	EnvKey      string           `json:"envKey"`
	State       string           `json:"state"`
	SSH         *SSHStatus       `json:"ssh,omitempty"`
	Shortcuts   int              `json:"shortcuts,omitempty"`
	Container   *ContainerOutput `json:"container,omitempty"`
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
	ConfigHash string `json:"configHash,omitempty"`
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
		projectName = state.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)

	// Compute env key from workspace
	envKey := state.ComputeEnvKey(workspacePath)

	// Try to load config and compute hash for staleness detection
	var currentState state.State
	var containerInfo *state.ContainerInfo

	cfg, _, err := config.Load(workspacePath, configPath)
	if err == nil {
		// Config exists, check for staleness
		configHash, hashErr := config.ComputeHash(cfg)
		if hashErr == nil {
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
		status := StatusOutput{
			Workspace: workspacePath,
			Project:   projectName,
			EnvKey:    envKey,
			State:     string(currentState),
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
				ID:         containerInfo.ID[:12],
				Name:       containerInfo.Name,
				Status:     containerInfo.Status,
				ConfigHash: containerInfo.ConfigHash,
			}
			if len(containerInfo.ConfigHash) > 12 {
				status.Container.ConfigHash = containerInfo.ConfigHash[:12]
			}
		}

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
	}

	return nil
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
