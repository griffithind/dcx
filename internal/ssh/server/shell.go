package server

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
)

//go:embed shell/bash.sh
var bashIntegration string

//go:embed shell/zsh.zsh
var zshIntegration string

//go:embed shell/fish.fish
var fishIntegration string

const shellIntegrationDir = "/tmp/.dcx-shell"

// ShellConfig contains shell-specific configuration for integration.
type ShellConfig struct {
	// Args to add when starting an interactive shell (replaces default -l)
	Args []string
	// Env vars to set
	Env []string
}

// SetupShellIntegration creates integration files and returns shell configuration.
// This enables terminal title updates showing the typed command (like Kitty/Ghostty).
func SetupShellIntegration(shell string) ShellConfig {
	shellBase := filepath.Base(shell)
	config := ShellConfig{}

	// Skip if no project name
	if os.Getenv("DCX_PROJECT_NAME") == "" {
		return config
	}

	_ = os.MkdirAll(shellIntegrationDir, 0755)

	switch shellBase {
	case "bash":
		// Write our init script (sources user's bashrc at the end)
		initPath := filepath.Join(shellIntegrationDir, "bash-init.sh")
		_ = os.WriteFile(initPath, []byte(bashIntegration), 0644)
		// Use --rcfile to load our init script instead of default -l
		config.Args = []string{"--rcfile", initPath}

	case "zsh":
		// Write integration script
		integrationPath := filepath.Join(shellIntegrationDir, "zsh.zsh")
		_ = os.WriteFile(integrationPath, []byte(zshIntegration), 0644)

		// Create ZDOTDIR with startup files that source our integration then user's
		zdotdir := filepath.Join(shellIntegrationDir, "zsh")
		_ = os.MkdirAll(zdotdir, 0755)

		// .zshenv - always sourced first
		zshenv := fmt.Sprintf(`# DCX shell integration
source "%s"
# Source user's .zshenv
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zshenv" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zshenv"
`, integrationPath)
		_ = os.WriteFile(filepath.Join(zdotdir, ".zshenv"), []byte(zshenv), 0644)

		// .zshrc - for interactive shells
		zshrc := `# Source user's .zshrc
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zshrc" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zshrc"
`
		_ = os.WriteFile(filepath.Join(zdotdir, ".zshrc"), []byte(zshrc), 0644)

		// .zprofile - for login shells
		zprofile := `# Source user's .zprofile
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zprofile" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zprofile"
`
		_ = os.WriteFile(filepath.Join(zdotdir, ".zprofile"), []byte(zprofile), 0644)

		// Preserve original ZDOTDIR and set ours
		if orig := os.Getenv("ZDOTDIR"); orig != "" {
			config.Env = append(config.Env, "ZDOTDIR_ORIG="+orig)
		}
		config.Env = append(config.Env, "ZDOTDIR="+zdotdir)
		// Still use -l for login shell behavior
		config.Args = []string{"-l"}

	case "fish":
		// Fish uses vendor_conf.d for additional config
		confDir := filepath.Join(shellIntegrationDir, "fish/vendor_conf.d")
		_ = os.MkdirAll(confDir, 0755)
		_ = os.WriteFile(filepath.Join(confDir, "dcx.fish"), []byte(fishIntegration), 0644)

		// Set XDG_DATA_DIRS to include our config
		xdgData := os.Getenv("XDG_DATA_DIRS")
		if xdgData == "" {
			xdgData = "/usr/local/share:/usr/share"
		}
		config.Env = append(config.Env, "XDG_DATA_DIRS="+shellIntegrationDir+":"+xdgData)
		config.Args = []string{"-l"}
	}

	return config
}
