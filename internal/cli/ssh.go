package cli

import (
	"fmt"
	"os"
	"os/exec"
	"syscall"

	"github.com/griffithind/dcx/internal/shell"
	"github.com/griffithind/dcx/internal/ssh/hostconfig"
	"github.com/griffithind/dcx/internal/state"
	"github.com/spf13/cobra"
	"golang.org/x/term"
)

var sshCmd = &cobra.Command{
	Use:   "ssh [-- command [args...]]",
	Short: "Open a shell or run a command in the container",
	Long: `Open an interactive shell in the workspace's devcontainer, or run a
command in it.

dcx finds the workspace from the current directory, checks that its
container is running, then hands over to OpenSSH using the host alias
'dcx up' configured. The session is exactly what 'ssh <alias>' gives
editors and other tools: the remote user, remoteEnv, the probed user
environment, SSH agent forwarding, and postAttachCommand for interactive
shells. Sessions share one connection per SSH agent, kept open for 10
minutes after the last one ends, so later commands start at once.

Arguments after '--' keep their quoting:

  dcx ssh                          # interactive shell
  dcx ssh -- npm install
  dcx ssh -- bash -c "echo hello"
  dcx ssh -- git clone git@github.com:user/repo.git

'dcx status' shows the alias and endpoint ('--detailed' adds the host key);
'dcx doctor' probes the agent.`,
	Args: cobra.ArbitraryArgs,
	RunE: runSSH,
}

var sshConnect bool

func init() {
	// --connect was needed when `dcx ssh` only printed the command; it is
	// the default now and kept so existing scripts keep working.
	sshCmd.Flags().BoolVar(&sshConnect, "connect", false, "connect (the default; kept for compatibility)")
	_ = sshCmd.Flags().MarkHidden("connect")
	sshCmd.GroupID = "execution"
	rootCmd.AddCommand(sshCmd)
}

func runSSH(cmd *cobra.Command, args []string) error {
	if len(args) > 0 && cmd.ArgsLenAtDash() != 0 {
		return fmt.Errorf("put the command after '--': dcx ssh -- %s", shell.Join(args))
	}

	var remote string
	if len(args) > 0 {
		remote = shell.Join(args)
	}
	return sshInto(remote)
}

// sshInto replaces dcx with an OpenSSH session to the workspace's
// container: an interactive shell when remote is empty, otherwise remote
// (a shell command line, run by the remote user's shell). OpenSSH's exit
// status becomes dcx's.
func sshInto(remote string) error {
	alias, share, err := runningWorkspaceAlias()
	if err != nil {
		return err
	}
	sshPath, err := exec.LookPath("ssh")
	if err != nil {
		return fmt.Errorf("ssh (OpenSSH client) not found in PATH; dcx needs it to connect")
	}

	argv := []string{"ssh"}
	if config := hostconfig.ConfigFileOverride(); config != "" {
		argv = append(argv, "-F", config)
	}
	argv = append(argv, share...)
	if remote != "" {
		// A terminal only when both ends are one, so interactive commands
		// (rails console, vim) work and piped output stays clean.
		if term.IsTerminal(int(os.Stdin.Fd())) && term.IsTerminal(int(os.Stdout.Fd())) {
			argv = append(argv, "-t")
		} else {
			argv = append(argv, "-T")
		}
	}
	argv = append(argv, alias)
	if remote != "" {
		argv = append(argv, "--", remote)
	}
	return syscall.Exec(sshPath, argv, os.Environ())
}

// runningWorkspaceAlias checks that the workspace's container is running
// and returns the SSH alias `dcx up` configured for it, and the options
// that share the connection with dcx's other sessions
// (hostconfig.ShareOptions). One `docker ps` finds a running container;
// only when it finds none does the full state lookup say why.
func runningWorkspaceAlias() (alias string, share []string, err error) {
	cliCtx := NewCLIContext()
	defer cliCtx.Close()

	id := cliCtx.Identity.WorkspaceID
	name := ""
	rows, err := cliCtx.Docker.ListContainerRows(cliCtx.Ctx, map[string]string{
		state.LabelWorkspaceID: id,
		state.LabelIsPrimary:   "true",
	})
	if err == nil && len(rows) == 1 && rows[0].State == "running" {
		name = rows[0].Name
	} else {
		info, err := RequireRunningContainer(cliCtx)
		if err != nil {
			return "", nil, err
		}
		name = info.Name
	}
	aliases := hostconfig.Aliases(name)
	if len(aliases) == 0 {
		return "", nil, fmt.Errorf("SSH is not configured for this devcontainer; run 'dcx up'")
	}
	return aliases[0], hostconfig.ShareOptions(id), nil
}
