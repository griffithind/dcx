package cli

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// captureStdout returns what run writes to os.Stdout (printJSON's output).
func captureStdout(t *testing.T, run func()) []byte {
	t.Helper()
	r, w, err := os.Pipe()
	require.NoError(t, err)
	old := os.Stdout
	os.Stdout = w
	defer func() { os.Stdout = old }()
	run()
	require.NoError(t, w.Close())
	var buf bytes.Buffer
	_, err = io.Copy(&buf, r)
	require.NoError(t, err)
	return buf.Bytes()
}

func TestDoctorJSON(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, `{"image": "alpine:3"}`)
	doctorJSON = true
	var report doctorReport
	out := captureStdout(t, func() { require.NoError(t, runDoctor(doctorCmd, nil)) })
	require.NoError(t, json.Unmarshal(out, &report), "output: %s", out)
	assert.True(t, report.OK)
	sections := map[string]int{}
	for _, c := range report.Checks {
		sections[c.Section]++
	}
	assert.Equal(t, 3, sections["system"], "docker, compose, selinux")
	assert.Equal(t, 1, sections["config"])
}

func TestLockJSONWithoutFeatures(t *testing.T) {
	withFakeEngine(t, &containertest.Fake{}, `{"image": "alpine:3"}`)
	lockJSON = true
	var got lockResultJSON
	out := captureStdout(t, func() { require.NoError(t, runLock(lockCmd, nil)) })
	require.NoError(t, json.Unmarshal(out, &got), "output: %s", out)
	assert.Equal(t, "no-features", got.Action)
	assert.Equal(t, []string{}, got.Changes)
}

func TestJSONRequested(t *testing.T) {
	cmd := &cobra.Command{Use: "x"}
	assert.False(t, jsonRequested(cmd), "no --json flag")
	var j bool
	cmd.Flags().BoolVar(&j, "json", false, "")
	assert.False(t, jsonRequested(cmd))
	require.NoError(t, cmd.Flags().Set("json", "true"))
	assert.True(t, jsonRequested(cmd))
	assert.False(t, jsonRequested(nil))
}
