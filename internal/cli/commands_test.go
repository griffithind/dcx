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

	noAgentFlag := flags.Lookup("no-agent")
	assert.NotNil(t, noAgentFlag, "no-agent flag should exist")
	assert.Equal(t, "false", noAgentFlag.DefValue)

	sshFlag := flags.Lookup("ssh")
	assert.NotNil(t, sshFlag, "ssh flag should exist")
	assert.Equal(t, "false", sshFlag.DefValue)
}

func TestUpCommandMetadata(t *testing.T) {
	assert.Equal(t, "up", upCmd.Use)
	assert.NotEmpty(t, upCmd.Short)
	assert.NotEmpty(t, upCmd.Long)
	assert.NotNil(t, upCmd.RunE)
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

func TestDownCommandMetadata(t *testing.T) {
	assert.Equal(t, "down", downCmd.Use)
	assert.NotEmpty(t, downCmd.Short)
	assert.NotEmpty(t, downCmd.Long)
	assert.NotNil(t, downCmd.RunE)
}

func TestStatusCommandFlags(t *testing.T) {
	flags := statusCmd.Flags()

	detailedFlag := flags.Lookup("detailed")
	assert.NotNil(t, detailedFlag, "detailed flag should exist")
	assert.Equal(t, "false", detailedFlag.DefValue)
	assert.Equal(t, "d", detailedFlag.Shorthand)
}

func TestStatusCommandMetadata(t *testing.T) {
	assert.Equal(t, "status", statusCmd.Use)
	assert.NotEmpty(t, statusCmd.Short)
	assert.NotEmpty(t, statusCmd.Long)
	assert.NotNil(t, statusCmd.RunE)
}

func TestExecCommandFlags(t *testing.T) {
	flags := execCmd.Flags()

	noAgentFlag := flags.Lookup("no-agent")
	assert.NotNil(t, noAgentFlag, "no-agent flag should exist")
	assert.Equal(t, "false", noAgentFlag.DefValue)
}

func TestExecCommandMetadata(t *testing.T) {
	assert.Contains(t, execCmd.Use, "exec")
	assert.NotEmpty(t, execCmd.Short)
	assert.NotEmpty(t, execCmd.Long)
	assert.NotNil(t, execCmd.RunE)
}

func TestRootCommandExists(t *testing.T) {
	assert.NotNil(t, rootCmd)
	assert.Equal(t, "dcx", rootCmd.Use)
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
