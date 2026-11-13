package lifecycle

import (
	"context"
	"errors"
	"github.com/griffithind/dcx/internal/agentproto"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memMarkers struct {
	mu   sync.Mutex
	done map[string]bool
}

func (m *memMarkers) Done(_ context.Context, p string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done[p]
}

func (m *memMarkers) Mark(_ context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done == nil {
		m.done = map[string]bool{}
	}
	m.done[p] = true
	return nil
}

// testRunner records container commands; failOn makes a command fail.
func testRunner(cfg *devcontainer.DevContainerConfig, failOn string) (*HookRunner, *[]string) {
	var mu sync.Mutex
	var ran []string
	r := NewHookRunner(0, report.Discard, "/ws", cfg, "ws")
	exec := func(_ context.Context, c CommandSpec) error {
		mu.Lock()
		ran = append(ran, strings.Join(c.Args, " "))
		mu.Unlock()
		if failOn != "" && strings.Contains(strings.Join(c.Args, " "), failOn) {
			return errors.New("boom")
		}
		return nil
	}
	r.execContainer = exec
	r.execHost = exec
	return r, &ran
}

func hookConfig() *devcontainer.DevContainerConfig {
	return &devcontainer.DevContainerConfig{
		OnCreateCommand:      "on-create",
		UpdateContentCommand: "update-content",
		PostCreateCommand:    "post-create",
		PostStartCommand:     "post-start",
		PostAttachCommand:    "post-attach",
	}
}

// A failure stops at the failing phase; the next run skips completed phases
// and resumes at the failed one instead of rerunning everything (or, as
// before, never running the rest).
func TestRunCreateHooksResumesAfterFailure(t *testing.T) {
	markers := &memMarkers{}

	r, ran := testRunner(hookConfig(), "post-create")
	err := r.RunCreateHooks(context.Background(), markers)
	require.Error(t, err)
	assert.Equal(t, []string{"on-create", "update-content", "post-create"}, *ran)
	assert.True(t, markers.Done(context.Background(), PhaseOnCreate))
	assert.True(t, markers.Done(context.Background(), PhaseUpdateContent))
	assert.False(t, markers.Done(context.Background(), PhasePostCreate))

	r2, ran2 := testRunner(hookConfig(), "")
	require.NoError(t, r2.RunCreateHooks(context.Background(), markers))
	assert.Equal(t, []string{"post-create"}, *ran2, "completed phases must not rerun")
	assert.True(t, markers.Done(context.Background(), PhasePostCreate))
}

func TestFeatureHooksRunBeforeConfigHooks(t *testing.T) {
	r, ran := testRunner(hookConfig(), "")
	r.SetFeatureHooks(
		[]features.FeatureHook{{FeatureName: "f", Command: "feature-on-create"}},
		nil, nil,
		[]features.FeatureHook{{FeatureName: "f", Command: "feature-post-start"}},
	)
	require.NoError(t, r.RunCreateHooks(context.Background(), nil))
	require.NoError(t, r.RunStartHooks(context.Background()))
	assert.Equal(t, []string{
		"feature-on-create", "on-create", "update-content", "post-create",
		"feature-post-start", "post-start",
	}, *ran)
}

func TestAttachSteps(t *testing.T) {
	steps := AttachSteps(
		[]features.FeatureHook{
			{FeatureName: "f", Command: "feature-post-attach"},
			{FeatureName: "empty", Command: ""},
		},
		map[string]interface{}{"b": []interface{}{"echo", "b c"}, "a": "echo a"},
	)
	assert.Equal(t, []agentproto.AttachStep{
		{Label: "feature f", Commands: []agentproto.AttachCommand{{Shell: "feature-post-attach"}}},
		{Label: "devcontainer.json", Commands: []agentproto.AttachCommand{
			{Name: "a", Shell: "echo a"},
			{Name: "b", Argv: []string{"echo", "b c"}},
		}},
	}, steps)
	assert.Empty(t, AttachSteps(nil, nil))
}

func TestRunInitializeUsesHost(t *testing.T) {
	cfg := &devcontainer.DevContainerConfig{InitializeCommand: []interface{}{"touch", "x y"}}
	r, ran := testRunner(cfg, "")
	require.NoError(t, r.RunInitialize(context.Background()))
	assert.Equal(t, []string{"touch x y"}, *ran)
}

// When one parallel (object-form) command fails, the others' context is
// cancelled.
func TestParallelFailureCancelsOthers(t *testing.T) {
	cfg := &devcontainer.DevContainerConfig{OnCreateCommand: map[string]interface{}{"fail": "fail", "slow": "slow"}}
	r := NewHookRunner(0, report.Discard, "/ws", cfg, "ws")
	cancelled := make(chan struct{})
	r.execContainer = func(ctx context.Context, c CommandSpec) error {
		if c.Name == "fail" {
			return errors.New("boom")
		}
		select {
		case <-ctx.Done():
			close(cancelled)
			return ctx.Err()
		case <-time.After(5 * time.Second):
			return nil
		}
	}
	err := r.RunCreateHooks(context.Background(), nil)
	require.Error(t, err)
	select {
	case <-cancelled:
	default:
		t.Fatal("slow command was not cancelled")
	}
}

func TestParseCommandMapIsSorted(t *testing.T) {
	cmds := ParseCommand(map[string]interface{}{"c": "3", "a": "1", "b": []interface{}{"2"}})
	require.Len(t, cmds, 3)
	assert.Equal(t, []string{"a", "b", "c"}, []string{cmds[0].Name, cmds[1].Name, cmds[2].Name})
	assert.True(t, cmds[0].Parallel)
	assert.Nil(t, ParseCommand("   "))
}
