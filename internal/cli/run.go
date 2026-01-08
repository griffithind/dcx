package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sort"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/output"
	"github.com/griffithind/dcx/internal/shortcuts"
	"github.com/griffithind/dcx/internal/ssh"
	"github.com/griffithind/dcx/internal/state"
	"github.com/griffithind/dcx/internal/workspace"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	runNoAgent bool
	runList    bool
)

var runCmd = &cobra.Command{
	Use:   "run [shortcut] [args...]",
	Short: "Run a command shortcut in the container",
	Long: `Run a configured command shortcut in the devcontainer.

Shortcuts are defined in .devcontainer/dcx.json under the "shortcuts" key.

Example dcx.json:
{
  "name": "myproject",
  "shortcuts": {
    "rw": "bin/jobs --skip-recurring",
    "r": {"prefix": "rails", "passArgs": true},
    "test": {"prefix": "rails test", "passArgs": true, "description": "Run tests"}
  }
}

Usage:
  dcx run rw                    # Runs: bin/jobs --skip-recurring
  dcx run r server              # Runs: rails server
  dcx run r console             # Runs: rails console
  dcx run test test/models/     # Runs: rails test test/models/

Use --list to see all available shortcuts.`,
	RunE: runRunCommand,
	Args: cobra.ArbitraryArgs,
}

func init() {
	runCmd.Flags().BoolVar(&runNoAgent, "no-agent", false, "disable SSH agent forwarding")
	runCmd.Flags().BoolVarP(&runList, "list", "l", false, "list available shortcuts")
	// Stop parsing flags after the shortcut name so args like --version pass through
	runCmd.Flags().SetInterspersed(false)
	runCmd.GroupID = "execution"
	rootCmd.AddCommand(runCmd)
}

func runRunCommand(cmd *cobra.Command, args []string) error {
	// Load dcx.json for shortcuts
	dcxCfg, err := config.LoadDcxConfig(workspacePath)
	if err != nil {
		return fmt.Errorf("failed to load dcx.json: %w", err)
	}

	// Handle --list flag
	if runList {
		return listShortcuts(dcxCfg)
	}

	if len(args) == 0 {
		return fmt.Errorf("no shortcut specified; use --list to see available shortcuts")
	}

	// Resolve shortcut
	if dcxCfg == nil || len(dcxCfg.Shortcuts) == 0 {
		return fmt.Errorf("no shortcuts defined in .devcontainer/dcx.json")
	}

	resolved := shortcuts.Resolve(dcxCfg.Shortcuts, args)
	if !resolved.Found {
		return fmt.Errorf("unknown shortcut %q; use --list to see available shortcuts", args[0])
	}

	// Execute the resolved command
	return executeInContainer(resolved.Command)
}

func listShortcuts(dcxCfg *config.DcxConfig) error {
	out := output.Global()
	c := out.Color()

	if dcxCfg == nil || len(dcxCfg.Shortcuts) == 0 {
		out.Println("No shortcuts defined.")
		out.Println()
		out.Println("To define shortcuts, create .devcontainer/dcx.json with a \"shortcuts\" key.")
		return nil
	}

	infos := shortcuts.ListShortcuts(dcxCfg.Shortcuts)

	// Sort alphabetically
	sort.Slice(infos, func(i, j int) bool {
		return infos[i].Name < infos[j].Name
	})

	out.Println(c.Header("Available shortcuts:"))
	out.Println()

	// Calculate column widths
	maxName := 0
	for _, info := range infos {
		if len(info.Name) > maxName {
			maxName = len(info.Name)
		}
	}

	for _, info := range infos {
		if info.Description != "" {
			out.Printf("  %s  %s", c.Code(fmt.Sprintf("%-*s", maxName, info.Name)), info.Description)
			out.Printf("  %-*s  %s", maxName, "", c.Dim("-> "+info.Expansion))
		} else {
			out.Printf("  %s  %s", c.Code(fmt.Sprintf("%-*s", maxName, info.Name)), c.Dim("-> "+info.Expansion))
		}
	}

	return nil
}

func executeInContainer(execArgs []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json for project name
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)

	// Get project name from dcx.json
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = docker.SanitizeProjectName(dcxCfg.Name)
	}

	// Initialize state manager
	stateMgr := state.NewManager(dockerClient)
	envKey := workspace.ComputeID(workspacePath)

	// Check current state (check both project name and env key for migration)
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	switch currentState {
	case state.StateAbsent:
		return fmt.Errorf("no environment found; run 'dcx up' first")
	case state.StateCreated:
		return fmt.Errorf("environment is not running; run 'dcx start' first")
	case state.StateBroken:
		return fmt.Errorf("environment is in broken state; run 'dcx up --recreate'")
	case state.StateStale:
		fmt.Fprintln(os.Stderr, "Warning: environment is stale (config changed)")
	}

	if containerInfo == nil {
		return fmt.Errorf("no primary container found")
	}

	// Load devcontainer config to get user and workspace folder
	cfg, _, _ := config.Load(workspacePath, configPath)

	// Build docker exec command
	dockerArgs := []string{"exec"}

	// Check if we have a TTY
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))
	if isTTY {
		dockerArgs = append(dockerArgs, "-it")
	} else {
		dockerArgs = append(dockerArgs, "-i")
	}

	// Add working directory and user
	var user string
	if cfg != nil {
		workDir := config.DetermineContainerWorkspaceFolder(cfg, workspacePath)
		dockerArgs = append(dockerArgs, "-w", workDir)

		// Add user if specified
		user = cfg.RemoteUser
		if user == "" {
			user = cfg.ContainerUser
		}
		if user != "" {
			user = config.Substitute(user, &config.SubstitutionContext{
				LocalWorkspaceFolder: workspacePath,
			})
			dockerArgs = append(dockerArgs, "-u", user)
			// Set USER and HOME env vars
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("USER=%s", user))
			dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("HOME=/home/%s", user))
		}
	}

	// Setup SSH agent forwarding if enabled
	var agentProxy *ssh.AgentProxy
	if !runNoAgent && ssh.IsAgentAvailable() {
		// Get UID/GID for the container user
		uid, gid := ssh.GetContainerUserIDs(containerInfo.Name, user)

		// Skip deployment since binary is pre-deployed during 'up'
		opts := ssh.AgentProxyOptions{SkipDeploy: true}
		agentProxy, err = ssh.NewAgentProxyWithOptions(containerInfo.ID, containerInfo.Name, uid, gid, opts)
		if err != nil {
			fmt.Fprintf(os.Stderr, "Warning: SSH agent proxy setup failed: %v\n", err)
		} else {
			socketPath, startErr := agentProxy.Start()
			if startErr != nil {
				fmt.Fprintf(os.Stderr, "Warning: SSH agent proxy start failed: %v\n", startErr)
			} else {
				dockerArgs = append(dockerArgs, "-e", fmt.Sprintf("SSH_AUTH_SOCK=%s", socketPath))
			}
		}
	}

	// Add container name and command
	dockerArgs = append(dockerArgs, containerInfo.Name)
	dockerArgs = append(dockerArgs, execArgs...)

	// Run docker exec
	dockerCmd := exec.CommandContext(ctx, "docker", dockerArgs...)
	dockerCmd.Stdin = os.Stdin
	dockerCmd.Stdout = os.Stdout
	dockerCmd.Stderr = os.Stderr

	err = dockerCmd.Run()

	// Clean up SSH agent proxy
	if agentProxy != nil {
		agentProxy.Stop()
	}

	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			os.Exit(exitErr.ExitCode())
		}
		return fmt.Errorf("exec failed: %w", err)
	}

	return nil
}
