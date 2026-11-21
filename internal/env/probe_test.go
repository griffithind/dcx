package env

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbeCommand(t *testing.T) {
	assert.Nil(t, ProbeCommand(ProbeNone))
	assert.Nil(t, ProbeCommand(""))
	for probe, flags := range map[ProbeType][]string{
		ProbeLoginShell:            {"-l"},
		ProbeLoginInteractiveShell: {"-l", "-i"},
		ProbeInteractiveShell:      {"-i"},
	} {
		cmd := ProbeCommand(probe)
		require.Len(t, cmd, 4+len(flags), probe)
		assert.Equal(t, []string{"sh", "-c", probeScript, "dcx-probe"}, cmd[:4])
		assert.Equal(t, flags, cmd[4:], "the shell flags are positional arguments")
	}
}

func TestParseProbeType(t *testing.T) {
	tests := []struct {
		input    string
		expected ProbeType
	}{
		{"none", ProbeNone},
		{"", ProbeLoginInteractiveShell}, // the spec default
		{"loginShell", ProbeLoginShell},
		{"loginInteractiveShell", ProbeLoginInteractiveShell},
		{"interactiveShell", ProbeInteractiveShell},
		{"invalid", ProbeLoginInteractiveShell},
	}
	for _, tt := range tests {
		assert.Equal(t, tt.expected, ParseProbeType(tt.input), tt.input)
	}
}

func TestParseEnvOutput(t *testing.T) {
	out := "Welcome from .bashrc!\nlast login: today\n" + probeMarker + "\x00" +
		"PATH=/usr/bin:/bin\x00OPTS=--foo=bar\x00EMPTY=\x00" +
		"BASH_FUNC_nvm%%=() {  echo nvm\n}\x00"
	assert.Equal(t, map[string]string{
		"PATH":            "/usr/bin:/bin",
		"OPTS":            "--foo=bar",
		"EMPTY":           "",
		"BASH_FUNC_nvm%%": "() {  echo nvm\n}",
	}, ParseProbeOutput(out), "startup noise is skipped and multi-line values survive")
	assert.Empty(t, ParseProbeOutput(""))
}

// The probe script runs the user's login shell and reports its
// environment, multi-line values included.
func TestProbeScriptRuns(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("reads /proc/self/environ")
	}
	cmd := ProbeCommand(ProbeLoginShell)
	c := exec.Command(cmd[0], cmd[1:]...)
	c.Env = append(os.Environ(), "DCX_PROBE_TEST=line1\nline2")
	out, err := c.Output()
	require.NoError(t, err)
	env := ParseProbeOutput(string(out))
	assert.Equal(t, "line1\nline2", env["DCX_PROBE_TEST"])
	assert.NotEmpty(t, env["PATH"])
}
