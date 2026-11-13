package cli

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestUpCommandFlags(t *testing.T) {
	// Test that up command has expected flags
	flags := upCmd.Flags()

	// Check flag existence
	recreateFlag := flags.Lookup("recreate")
	assert.NotNil(t, recreateFlag, "recreate flag should exist")
	assert.Equal(t, "false", recreateFlag.DefValue)

	rebuildFlag := flags.Lookup("rebuild")
	assert.NotNil(t, rebuildFlag, "rebuild flag should exist")
	assert.Equal(t, "false", rebuildFlag.DefValue)

	pullFlag := flags.Lookup("pull")
	assert.NotNil(t, pullFlag, "pull flag should exist")
	assert.Equal(t, "false", pullFlag.DefValue)

	// Verify removed flags don't exist
	noAgentFlag := flags.Lookup("no-agent")
	assert.Nil(t, noAgentFlag, "no-agent flag should not exist (SSH agent is always enabled)")

	sshFlag := flags.Lookup("ssh")
	assert.Nil(t, sshFlag, "ssh flag should not exist (SSH is always enabled)")
}

func TestDownCommandFlags(t *testing.T) {
	flags := downCmd.Flags()

	volumesFlag := flags.Lookup("volumes")
	assert.NotNil(t, volumesFlag, "volumes flag should exist")
	assert.Equal(t, "false", volumesFlag.DefValue)

	orphansFlag := flags.Lookup("remove-orphans")
	assert.NotNil(t, orphansFlag, "remove-orphans flag should exist")
	assert.Equal(t, "false", orphansFlag.DefValue)
}

func TestStatusCommandFlags(t *testing.T) {
	flags := statusCmd.Flags()

	detailedFlag := flags.Lookup("detailed")
	assert.NotNil(t, detailedFlag, "detailed flag should exist")
	assert.Equal(t, "false", detailedFlag.DefValue)
	assert.Equal(t, "d", detailedFlag.Shorthand)
}

func TestShellAndExecCommandsAreGone(t *testing.T) {
	// `dcx ssh` replaced them; plain ssh gives the same session.
	for _, name := range []string{"shell", "exec"} {
		cmd, _, err := rootCmd.Find([]string{name})
		assert.True(t, err != nil || cmd == rootCmd, "%s should not be a command", name)
	}
}

func TestSSHRequiresDashBeforeCommand(t *testing.T) {
	cmd, _, err := rootCmd.Find([]string{"ssh"})
	assert.NoError(t, err)
	assert.Equal(t, sshCmd, cmd)
	assert.NoError(t, sshCmd.ParseFlags([]string{"ls"}))
	err = runSSH(sshCmd, []string{"ls"})
	assert.ErrorContains(t, err, "dcx ssh -- ls")
}

func TestRootCommandPersistentFlags(t *testing.T) {
	pFlags := rootCmd.PersistentFlags()

	workspaceFlag := pFlags.Lookup("workspace")
	assert.NotNil(t, workspaceFlag, "workspace flag should exist")
	assert.Equal(t, "w", workspaceFlag.Shorthand)

	configFlag := pFlags.Lookup("config")
	assert.NotNil(t, configFlag, "config flag should exist")
	assert.Equal(t, "c", configFlag.Shorthand)

	verboseFlag := pFlags.Lookup("verbose")
	assert.NotNil(t, verboseFlag, "verbose flag should exist")
	assert.Equal(t, "v", verboseFlag.Shorthand)
}
