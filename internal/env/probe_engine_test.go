package env

import (
	"context"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeRunsAsUserThroughEngine(t *testing.T) {
	probe := strings.Join(ProbeCommand(ProbeLoginShell), " ")
	fake := &containertest.Fake{Outputs: map[string]string{
		probe: "\n" + probeMarker + "\x00PATH=/usr/local/bin:/usr/bin\x00PWD=/home/dev\x00NVM_DIR=/nvm\x00",
	}}
	env, err := NewProber(fake).Probe(context.Background(), "c1", ProbeLoginShell, "dev")
	require.NoError(t, err)
	assert.Equal(t, map[string]string{"PATH": "/usr/local/bin:/usr/bin", "NVM_DIR": "/nvm"}, env, "PWD is dropped")

	calls := fake.CallsTo("ExecOutputAs")
	require.Len(t, calls, 1)
	assert.Equal(t, append([]string{"c1", "dev"}, ProbeCommand(ProbeLoginShell)...), calls[0].Args)
}

func TestProbeNoneDoesNothing(t *testing.T) {
	fake := &containertest.Fake{}
	env, err := NewProber(fake).Probe(context.Background(), "c1", ProbeNone, "dev")
	require.NoError(t, err)
	assert.Nil(t, env)
	assert.Empty(t, fake.Calls())
}
