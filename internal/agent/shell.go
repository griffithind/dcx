package agent

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/agentproto"
)

//go:embed shell/bash.sh
var bashIntegration string

//go:embed shell/zsh.zsh
var zshIntegration string

//go:embed shell/fish.fish
var fishIntegration string

// shellIntegrationDir is root-owned; `dcx-agent provision` (run as root by
// dcx) writes the scripts, the listener only reads them.
var shellIntegrationDir = agentproto.ShellIntegrationDir

// ShellConfig contains shell-specific configuration for integration.
type ShellConfig struct {
	// Args to add when starting an interactive shell (replaces default -l)
	Args []string
	// Env vars to set
	Env []string
}

// WriteShellIntegration writes the integration scripts for every supported
// shell into dir. It runs at setup time as root, so dir must already be a
// root-owned directory (see runSetupCmd).
func WriteShellIntegration(dir string) error {
	zdotdir := filepath.Join(dir, "zsh")
	fishConf := filepath.Join(dir, "fish", "vendor_conf.d")
	for _, d := range []string{dir, zdotdir, fishConf} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			return err
		}
	}
	zshIntegrationPath := filepath.Join(dir, "zsh.zsh")
	files := map[string]string{
		filepath.Join(dir, "bash-init.sh"): bashIntegration,
		zshIntegrationPath:                 zshIntegration,
		filepath.Join(zdotdir, ".zshenv"): fmt.Sprintf(`# DCX shell integration
source "%s"
# Source user's .zshenv
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zshenv" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zshenv"
`, zshIntegrationPath),
		filepath.Join(zdotdir, ".zshrc"): `# Source user's .zshrc
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zshrc" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zshrc"
`,
		filepath.Join(zdotdir, ".zprofile"): `# Source user's .zprofile
[[ -f "${ZDOTDIR_ORIG:-$HOME}/.zprofile" ]] && source "${ZDOTDIR_ORIG:-$HOME}/.zprofile"
`,
		filepath.Join(fishConf, "dcx.fish"): fishIntegration,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			return err
		}
	}
	return nil
}

// SetupShellIntegration returns how to start an interactive shell so it
// sources dcx's integration (terminal titles showing the typed command).
// It only reads: when the scripts were not installed, or the workspace has
// no project name, the shell starts normally.
func SetupShellIntegration(shell string) ShellConfig {
	config := ShellConfig{}
	if os.Getenv("DCX_PROJECT_NAME") == "" {
		return config
	}
	if _, err := os.Stat(filepath.Join(shellIntegrationDir, "bash-init.sh")); err != nil {
		return config
	}

	switch filepath.Base(shell) {
	case "bash":
		// --rcfile loads our init script, which sources the user's bashrc.
		config.Args = []string{"--rcfile", filepath.Join(shellIntegrationDir, "bash-init.sh")}
	case "zsh":
		if orig := os.Getenv("ZDOTDIR"); orig != "" {
			config.Env = append(config.Env, "ZDOTDIR_ORIG="+orig)
		}
		config.Env = append(config.Env, "ZDOTDIR="+filepath.Join(shellIntegrationDir, "zsh"))
		config.Args = []string{"-l"}
	case "fish":
		xdgData := os.Getenv("XDG_DATA_DIRS")
		if xdgData == "" {
			xdgData = "/usr/local/share:/usr/share"
		}
		// fish reads $XDG_DATA_DIRS/fish/vendor_conf.d.
		config.Env = append(config.Env, "XDG_DATA_DIRS="+shellIntegrationDir+":"+xdgData)
		config.Args = []string{"-l"}
	}
	return config
}
