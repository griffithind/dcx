package agentctl

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// currentAgent is a fake engine whose container already has the embedded
// agent, so Provision goes straight to the provision call.
func currentAgent(t *testing.T) *containertest.Fake {
	t.Helper()
	t.Setenv("HOME", t.TempDir())
	bin := agentBinary(t)
	return &containertest.Fake{Outputs: map[string]string{probeKey: "x86_64\n" + sha256Hex(bin) + "\n"}}
}

func provisionResult(res agentproto.ProvisionResult) func(string, []string, []byte) ([]byte, error) {
	return func(string, []string, []byte) ([]byte, error) {
		data, err := json.Marshal(res)
		return data, err
	}
}

// Everything the agent needs travels in one exec, as root, on stdin.
func TestProvisionSendsOneRequest(t *testing.T) {
	fake := currentAgent(t)
	fake.InputFunc = provisionResult(agentproto.ProvisionResult{Started: true})
	opts := Options{
		ContainerName: "c1", WorkspaceID: "wk_test", User: "dev", DefaultUser: "node",
		WorkDir: "/workspaces/app", AllowedCIDRs: []string{"10.0.0.0/24"},
		Secrets:      []agentproto.Secret{{Name: "token", Value: []byte("t0k")}},
		SecretsOwner: "dev",
		Environment:  map[string]string{"A": "1"},
		Probe:        "loginInteractiveShell",
		Session:      agentproto.SessionConfig{Env: map[string]string{"R": "remote"}},
	}
	require.NoError(t, Provision(context.Background(), fake, nil, opts))

	calls := fake.CallsTo("ExecAsRootWithInput")
	require.Len(t, calls, 1)
	assert.Equal(t, []string{"c1", agentproto.BinaryPath, "provision"}, calls[0].Args)
	for _, c := range fake.Calls() {
		assert.NotContains(t, strings.Join(c.Args, " "), "t0k", "secrets never reach a command line")
	}

	var req agentproto.ProvisionRequest
	require.NoError(t, json.Unmarshal(fake.Inputs[0], &req))
	assert.Contains(t, string(req.HostKey), "PRIVATE KEY")
	assert.Contains(t, string(req.AuthorizedKeys), "ssh-ed25519", "the dcx fallback key is always authorized")
	assert.Equal(t, opts.Secrets, req.Secrets)
	assert.Equal(t, "dev", req.SecretsOwner)
	assert.Equal(t, opts.Environment, req.Environment)
	assert.Equal(t, "loginInteractiveShell", req.Probe)
	assert.Equal(t, "dev", req.ProbeUser)
	assert.Equal(t, opts.Session, req.Session)
	assert.Equal(t, agentproto.ListenConfig{
		Addr: agentproto.ListenAddr("0.0.0.0"), User: "dev", WorkDir: "/workspaces/app", AllowCIDRs: []string{"10.0.0.0/24"},
	}, req.Listen)
	assert.Equal(t, "node", req.DefaultUser)
}

// What the agent reports reaches the user; a failure is an error.
func TestProvisionReportsTheResult(t *testing.T) {
	fake := currentAgent(t)
	fake.InputFunc = provisionResult(agentproto.ProvisionResult{
		Warnings: []string{"Environment probe failed: boom"},
		Skipped:  []string{"QUOTED"},
	})
	rec := &report.Recorder{}
	require.NoError(t, Provision(context.Background(), fake, rec, Options{ContainerName: "c1", WorkspaceID: "wk_test"}))
	var warnings []string
	for _, m := range rec.Messages() {
		if m.Level == report.LevelWarn {
			warnings = append(warnings, m.Text)
		}
	}
	assert.Equal(t, []string{
		"Environment probe failed: boom",
		"Not written to /etc/environment (name or value it cannot express; sessions still get them): QUOTED",
	}, warnings)

	fake = currentAgent(t)
	fake.InputFunc = func(string, []string, []byte) ([]byte, error) {
		return []byte(`{"error":"dcx agent did not start listening within 15s; agent log:\nbind: address in use"}`), errors.New("exit status 1")
	}
	err := Provision(context.Background(), fake, nil, Options{ContainerName: "c1", WorkspaceID: "wk_test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "address in use", "the agent's explanation is kept")

	fake = currentAgent(t)
	fake.InputFunc = func(string, []string, []byte) ([]byte, error) {
		return nil, errors.New("exec failed: no such container")
	}
	err = Provision(context.Background(), fake, nil, Options{ContainerName: "c1", WorkspaceID: "wk_test"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no such container")
}
