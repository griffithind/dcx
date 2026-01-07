package cli

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var (
	attachDetachKeys string
	attachNoStdin    bool
	attachSigProxy   bool
)

var attachCmd = &cobra.Command{
	Use:   "attach",
	Short: "Attach to running container",
	Long: `Attach local standard input, output, and error streams to the
running devcontainer's primary process.

This is useful for interacting with the main process started by the
container's entrypoint. Use Ctrl+P, Ctrl+Q to detach without stopping
the container.

For running commands in the container, use 'dcx exec' or 'dcx shell' instead.`,
	RunE: runAttach,
}

func runAttach(cmd *cobra.Command, args []string) error {
	ctx := context.Background()

	// Initialize Docker client
	dockerClient, err := docker.NewClient()
	if err != nil {
		return fmt.Errorf("failed to connect to Docker: %w", err)
	}
	defer dockerClient.Close()

	// Load dcx.json for project name
	dcxCfg, _ := config.LoadDcxConfig(workspacePath)
	var projectName string
	if dcxCfg != nil && dcxCfg.Name != "" {
		projectName = state.SanitizeProjectName(dcxCfg.Name)
	}

	// Get state
	envKey := state.ComputeEnvKey(workspacePath)
	stateMgr := state.NewManager(dockerClient)
	currentState, containerInfo, err := stateMgr.GetStateWithProject(ctx, projectName, envKey)
	if err != nil {
		return fmt.Errorf("failed to get state: %w", err)
	}

	if currentState != state.StateRunning {
		return fmt.Errorf("container is not running (state: %s)", currentState)
	}

	if containerInfo == nil {
		return fmt.Errorf("no container found")
	}

	// Setup terminal
	var stdin io.Reader
	if !attachNoStdin {
		stdin = os.Stdin
	}

	// Check if we have a TTY
	isTTY := term.IsTerminal(int(os.Stdin.Fd()))

	// Make stdin raw if it's a terminal
	var oldState *term.State
	if isTTY && !attachNoStdin {
		oldState, err = term.MakeRaw(int(os.Stdin.Fd()))
		if err != nil {
			return fmt.Errorf("failed to set terminal to raw mode: %w", err)
		}
		defer term.Restore(int(os.Stdin.Fd()), oldState)
	}

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	if attachSigProxy {
		signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
		defer signal.Stop(sigCh)
	}

	// Create a context that can be cancelled
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	// Forward signals to container in background
	if attachSigProxy {
		go func() {
			for sig := range sigCh {
				// Forward signal to container
				_ = dockerClient.KillContainer(ctx, containerInfo.ID, sig.String())
			}
		}()
	}

	fmt.Println("Attaching to container... (use Ctrl+P, Ctrl+Q to detach)")

	// Attach to container
	err = dockerClient.AttachContainer(ctx, containerInfo.ID, docker.AttachOptions{
		Stdin:      stdin,
		Stdout:     os.Stdout,
		Stderr:     os.Stderr,
		TTY:        isTTY,
		DetachKeys: attachDetachKeys,
	})

	if err != nil {
		return fmt.Errorf("attach failed: %w", err)
	}

	return nil
}

func init() {
	attachCmd.Flags().StringVar(&attachDetachKeys, "detach-keys", "ctrl-p,ctrl-q", "override the key sequence for detaching")
	attachCmd.Flags().BoolVar(&attachNoStdin, "no-stdin", false, "do not attach STDIN")
	attachCmd.Flags().BoolVar(&attachSigProxy, "sig-proxy", true, "proxy received signals to the process")
	rootCmd.AddCommand(attachCmd)
}
