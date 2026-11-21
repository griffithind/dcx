package lifecycle

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/report"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type memMarkers struct {
	mu   sync.Mutex
	done map[string]bool
}

func (m *memMarkers) Done(_ context.Context, p string) (bool, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.done[p], nil
}

func (m *memMarkers) is(p string) bool {
	done, _ := m.Done(context.Background(), p)
	return done
}

// unreachableMarkers cannot answer (the container did not respond).
type unreachableMarkers struct{}

func (unreachableMarkers) Done(context.Context, string) (bool, error) {
	return false, errors.New("container not responding")
}
func (unreachableMarkers) Mark(context.Context, string) error { return nil }

func (m *memMarkers) Mark(_ context.Context, p string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.done == nil {
		m.done = map[string]bool{}
	}
	m.done[p] = true
	return nil
}

// testRunner records the commands it runs; failOn makes a command fail.
func testRunner(hooks devcontainer.Hooks, failOn string) (*HookRunner, *[]string) {
	var mu sync.Mutex
	var ran []string
	r := NewHookRunner("", report.Discard, "/ws", hooks)
	exec := func(_ context.Context, c lifecyclecmd.Step) error {
		mu.Lock()
		ran = append(ran, c.String())
		mu.Unlock()
		if failOn != "" && strings.Contains(c.String(), failOn) {
			return errors.New("boom")
		}
		return nil
	}
	r.execContainer = exec
	r.execHost = exec
	return r, &ran
}

func cmd(t *testing.T, js string) *lifecyclecmd.Command {
	t.Helper()
	var c lifecyclecmd.Command
	require.NoError(t, json.Unmarshal([]byte(js), &c))
	return &c
}

func local(t *testing.T, js string) devcontainer.Hook {
	return devcontainer.Hook{Source: devcontainer.HookFromConfig, Command: cmd(t, js)}
}

func hookConfig(t *testing.T) devcontainer.Hooks {
	return devcontainer.Hooks{
		OnCreate:      []devcontainer.Hook{local(t, `"on-create"`)},
		UpdateContent: []devcontainer.Hook{local(t, `"update-content"`)},
		PostCreate:    []devcontainer.Hook{local(t, `"post-create"`)},
		PostStart:     []devcontainer.Hook{local(t, `"post-start"`)},
	}
}

// A failure stops at the failing phase; the next run skips completed phases
// and resumes at the failed one instead of rerunning everything (or, as
// before, never running the rest).
func TestRunCreateHooksResumesAfterFailure(t *testing.T) {
	markers := &memMarkers{}

	r, ran := testRunner(hookConfig(t), "post-create")
	err := r.RunCreateHooks(context.Background(), markers)
	require.Error(t, err)
	assert.Equal(t, []string{"on-create", "update-content", "post-create"}, *ran)
	assert.True(t, markers.is(PhaseOnCreate))
	assert.True(t, markers.is(PhaseUpdateContent))
	assert.False(t, markers.is(PhasePostCreate))

	r2, ran2 := testRunner(hookConfig(t), "")
	require.NoError(t, r2.RunCreateHooks(context.Background(), markers))
	assert.Equal(t, []string{"post-create"}, *ran2, "completed phases must not rerun")
	assert.True(t, markers.is(PhasePostCreate))
}

// When markers cannot be read, no once-only hook runs (R16): "can't tell"
// used to count as "not done" and re-ran onCreateCommand.
func TestRunCreateHooksFailsClosedOnUnreadableMarkers(t *testing.T) {
	r, ran := testRunner(hookConfig(t), "")
	err := r.RunCreateHooks(context.Background(), unreachableMarkers{})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot tell whether onCreateCommand already ran")
	assert.Empty(t, *ran)
}

// Each phase runs its hooks in the resolved order (image metadata, then
// features, then devcontainer.json).
func TestHooksRunInResolvedOrder(t *testing.T) {
	hooks := hookConfig(t)
	feature := devcontainer.Hook{Source: devcontainer.HookFromFeature, Feature: "f", Command: cmd(t, `"feature-on-create"`)}
	image := devcontainer.Hook{Source: devcontainer.HookFromImage, Command: cmd(t, `"image-on-create"`)}
	hooks.OnCreate = append([]devcontainer.Hook{image, feature}, hooks.OnCreate...)
	hooks.PostStart = append([]devcontainer.Hook{{Source: devcontainer.HookFromFeature, Feature: "f", Command: cmd(t, `"feature-post-start"`)}}, hooks.PostStart...)

	r, ran := testRunner(hooks, "")
	require.NoError(t, r.RunCreateHooks(context.Background(), nil))
	require.NoError(t, r.RunStartHooks(context.Background()))
	assert.Equal(t, []string{
		"image-on-create", "feature-on-create", "on-create", "update-content", "post-create",
		"feature-post-start", "post-start",
	}, *ran)
}

func TestAttachSteps(t *testing.T) {
	steps := AttachSteps([]devcontainer.Hook{
		{Source: devcontainer.HookFromImage, Command: cmd(t, `"image-post-attach"`)},
		{Source: devcontainer.HookFromFeature, Feature: "f", Command: cmd(t, `"feature-post-attach"`)},
		{Source: devcontainer.HookFromFeature, Feature: "empty", Command: cmd(t, `""`)},
		local(t, `{"b": ["echo", "b c"], "a": "echo a"}`),
	})
	assert.Equal(t, []agentproto.AttachStep{
		{Label: "image metadata", Commands: []agentproto.AttachCommand{{Shell: "image-post-attach"}}},
		{Label: "feature 'f'", Commands: []agentproto.AttachCommand{{Shell: "feature-post-attach"}}},
		{Label: "devcontainer.json", Commands: []agentproto.AttachCommand{
			{Name: "a", Shell: "echo a"},
			{Name: "b", Argv: []string{"echo", "b c"}},
		}},
	}, steps)
	assert.Empty(t, AttachSteps(nil))
}

func TestRunInitializeUsesHost(t *testing.T) {
	r, ran := testRunner(devcontainer.Hooks{Initialize: []devcontainer.Hook{local(t, `["touch", "x y"]`)}}, "")
	require.NoError(t, r.RunInitialize(context.Background()))
	assert.Equal(t, []string{"touch x y"}, *ran)
}

// When one parallel (object-form) command fails, the others' context is
// cancelled.
// A failing parallel command lets the others finish (M7), as in
// devcontainers/cli; the phase fails afterwards, naming the failure.
func TestParallelFailureLetsOthersFinish(t *testing.T) {
	hooks := devcontainer.Hooks{OnCreate: []devcontainer.Hook{local(t, `{"fail": "fail", "slow": "slow"}`)}}
	r := NewHookRunner("", report.Discard, "/ws", hooks)
	var finished atomic.Bool
	r.execContainer = func(ctx context.Context, c lifecyclecmd.Step) error {
		if c.Name == "fail" {
			return errors.New("boom")
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
			finished.Store(true)
			return nil
		}
	}
	err := r.RunCreateHooks(context.Background(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "[fail] boom")
	assert.True(t, finished.Load(), "the slow command ran to completion")
}
