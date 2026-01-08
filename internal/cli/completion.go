package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var completionCmd = &cobra.Command{
	Use:   "completion [bash|zsh|fish|powershell]",
	Short: "Generate shell completion scripts",
	Long: `Generate shell completion scripts for dcx.

To load completions:

Bash:
  $ source <(dcx completion bash)

  # To load completions for each session, execute once:
  # Linux:
  $ dcx completion bash > /etc/bash_completion.d/dcx
  # macOS:
  $ dcx completion bash > $(brew --prefix)/etc/bash_completion.d/dcx

Zsh:
  # If shell completion is not already enabled in your environment,
  # you will need to enable it. You can execute the following once:
  $ echo "autoload -U compinit; compinit" >> ~/.zshrc

  # To load completions for each session, execute once:
  $ dcx completion zsh > "${fpath[1]}/_dcx"

  # You may need to start a new shell for this setup to take effect.

Fish:
  $ dcx completion fish | source

  # To load completions for each session, execute once:
  $ dcx completion fish > ~/.config/fish/completions/dcx.fish

PowerShell:
  PS> dcx completion powershell | Out-String | Invoke-Expression

  # To load completions for every new session, run:
  PS> dcx completion powershell > dcx.ps1
  # and source this file from your PowerShell profile.
`,
	DisableFlagsInUseLine: true,
	ValidArgs:             []string{"bash", "zsh", "fish", "powershell"},
	Args:                  cobra.MatchAll(cobra.ExactArgs(1), cobra.OnlyValidArgs),
	RunE: func(cmd *cobra.Command, args []string) error {
		switch args[0] {
		case "bash":
			return cmd.Root().GenBashCompletionV2(os.Stdout, true)
		case "zsh":
			return cmd.Root().GenZshCompletion(os.Stdout)
		case "fish":
			return cmd.Root().GenFishCompletion(os.Stdout, true)
		case "powershell":
			return cmd.Root().GenPowerShellCompletionWithDesc(os.Stdout)
		}
		return nil
	},
}

func init() {
	completionCmd.GroupID = "utilities"
	rootCmd.AddCommand(completionCmd)
}
