package env

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeRunsAsUserThroughEngine(t *testing.T) {
	fake := &containertest.Fake{Outputs: map[string]string{
		"sh -l -c env": "PATH=/usr/local/bin:/usr/bin\nPWD=/home/dev\nNVM_DIR=/nvm\n",
	}}
	env, err := NewProber(fake).Probe(context.Background(), "c1", ProbeLoginShell, "dev")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PATH": "/usr/local/bin:/usr/bin", "NVM_DIR": "/nvm"}, env, "PWD is dropped")

	calls := fake.CallsTo("ExecOutputAs")
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"c1", "dev", "sh", "-l", "-c", "env"}, calls[0].Args)
}

func TestProbeNoneDoesNothing(t *testing.T) {
	fake := &containertest.Fake{}
	env, err := NewProber(fake).Probe(context.Background(), "c1", ProbeNone, "dev")
	require.NoError(t, err)
	assert.Nil(t, env)
	assert.Empty(t, fake.Calls())
}
