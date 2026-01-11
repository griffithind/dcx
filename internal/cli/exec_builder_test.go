package cli

import (
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/state"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewExecBuilder(t *testing.T) {
	containerInfo := &state.ContainerInfo{
		ID:   "test-container-id",
		Name: "test-container",
	}
	cfg := &devcontainer.DevContainerConfig{
		RemoteUser: "vscode",
	}
	workspacePath := "/test/workspace"

	builder := NewExecBuilder(containerInfo, cfg, workspacePath)
	require.NotNil(t, builder)
	assert.Equal(t, containerInfo, builder.containerInfo)
	assert.Equal(t, cfg, builder.cfg)
	assert.Equal(t, workspacePath, builder.workspacePath)
}

func TestExecBuilderBuildArgs(t *testing.T) {
	tests := []struct {
		name         string
		containerInfo *state.ContainerInfo
		cfg          *devcontainer.DevContainerConfig
		workspacePath string
		opts         ExecFlags
		wantContains []string
		wantNotContains []string
		wantUser     string
	}{
		{
			name: "basic command no TTY",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg:          nil,
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"echo", "hello"},
				TTY:     boolPtr(false),
			},
			wantContains:    []string{"exec", "-i"},
			wantNotContains: []string{"-it"},
			wantUser:        "",
		},
		{
			name: "with TTY",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg:          nil,
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"bash"},
				TTY:     boolPtr(true),
			},
			wantContains: []string{"exec", "-it"},
			wantUser:     "",
		},
		{
			name: "with user from opts",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg:          nil,
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"bash"},
				User:    "root",
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-u", "root", "USER=root"},
			wantUser:     "root",
		},
		{
			name: "with user from config RemoteUser",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg: &devcontainer.DevContainerConfig{
				RemoteUser: "vscode",
			},
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"bash"},
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-u", "vscode", "USER=vscode"},
			wantUser:     "vscode",
		},
		{
			name: "with user from config ContainerUser (fallback)",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg: &devcontainer.DevContainerConfig{
				ContainerUser: "developer",
			},
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"bash"},
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-u", "developer"},
			wantUser:     "developer",
		},
		{
			name: "opts user overrides config",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg: &devcontainer.DevContainerConfig{
				RemoteUser: "vscode",
			},
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"bash"},
				User:    "admin",
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-u", "admin"},
			wantUser:     "admin",
		},
		{
			name: "with custom workdir",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg:          nil,
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"ls"},
				WorkDir: "/custom/dir",
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-w", "/custom/dir"},
			wantUser:     "",
		},
		{
			name: "with environment variables",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg:          nil,
			workspacePath: "/workspace",
			opts: ExecFlags{
				Command: []string{"printenv"},
				Env:     []string{"FOO=bar", "BAZ=qux"},
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-e", "FOO=bar", "-e", "BAZ=qux"},
			wantUser:     "",
		},
		{
			name: "with workspace folder from config",
			containerInfo: &state.ContainerInfo{
				ID:   "container-123",
				Name: "my-container",
			},
			cfg: &devcontainer.DevContainerConfig{
				WorkspaceFolder: "/home/user/project",
			},
			workspacePath: "/local/project",
			opts: ExecFlags{
				Command: []string{"pwd"},
				TTY:     boolPtr(false),
			},
			wantContains: []string{"-w", "/home/user/project"},
			wantUser:     "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			builder := NewExecBuilder(tt.containerInfo, tt.cfg, tt.workspacePath)
			args, user := builder.BuildArgs(tt.opts)

			// Check expected args are present
			argsStr := strings.Join(args, " ")
			for _, want := range tt.wantContains {
				assert.Contains(t, argsStr, want, "args should contain %q", want)
			}

			// Check unwanted args are not present
			for _, notWant := range tt.wantNotContains {
				assert.NotContains(t, argsStr, notWant, "args should not contain %q", notWant)
			}

			// Check user
			assert.Equal(t, tt.wantUser, user)
		})
	}
}

func TestExecFlags(t *testing.T) {
	tests := []struct {
		name   string
		flags  ExecFlags
		wantLen int
	}{
		{
			name:    "empty flags",
			flags:   ExecFlags{},
			wantLen: 0,
		},
		{
			name: "with command",
			flags: ExecFlags{
				Command: []string{"bash", "-c", "echo hello"},
			},
			wantLen: 3,
		},
		{
			name: "with all options",
			flags: ExecFlags{
				Command: []string{"ls", "-la"},
				User:    "vscode",
				WorkDir: "/workspace",
				Env:     []string{"FOO=bar"},
				TTY:     boolPtr(true),
			},
			wantLen: 2,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Len(t, tt.flags.Command, tt.wantLen)
		})
	}
}

func TestBoolPtr(t *testing.T) {
	truePtr := boolPtr(true)
	assert.NotNil(t, truePtr)
	assert.True(t, *truePtr)

	falsePtr := boolPtr(false)
	assert.NotNil(t, falsePtr)
	assert.False(t, *falsePtr)
}

func TestExecBuilderNilConfig(t *testing.T) {
	containerInfo := &state.ContainerInfo{
		ID:   "test-id",
		Name: "test-container",
	}

	builder := NewExecBuilder(containerInfo, nil, "/workspace")
	args, user := builder.BuildArgs(ExecFlags{
		Command: []string{"echo", "test"},
		TTY:     boolPtr(false),
	})

	assert.Contains(t, args, "exec")
	assert.Contains(t, args, "-i")
	assert.Empty(t, user)
}

func TestExecBuilderRemoteEnv(t *testing.T) {
	tests := []struct {
		name         string
		remoteEnv    map[string]string
		optsEnv      []string
		wantContains []string
	}{
		{
			name: "remoteEnv is included in args",
			remoteEnv: map[string]string{
				"EDITOR":    "vim",
				"TERM":      "xterm-256color",
				"MY_SECRET": "value123",
			},
			wantContains: []string{"EDITOR=vim", "TERM=xterm-256color", "MY_SECRET=value123"},
		},
		{
			name:         "empty remoteEnv produces no extra args",
			remoteEnv:    map[string]string{},
			wantContains: []string{},
		},
		{
			name:         "nil remoteEnv produces no extra args",
			remoteEnv:    nil,
			wantContains: []string{},
		},
		{
			name: "opts.Env comes after remoteEnv (can override)",
			remoteEnv: map[string]string{
				"FOO": "from_remote",
			},
			optsEnv:      []string{"FOO=from_opts"},
			wantContains: []string{"FOO=from_remote", "FOO=from_opts"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerInfo := &state.ContainerInfo{
				ID:   "test-id",
				Name: "test-container",
			}
			cfg := &devcontainer.DevContainerConfig{
				RemoteEnv: tt.remoteEnv,
			}

			builder := NewExecBuilder(containerInfo, cfg, "/workspace")
			args, _ := builder.BuildArgs(ExecFlags{
				Command: []string{"env"},
				Env:     tt.optsEnv,
				TTY:     boolPtr(false),
			})

			argsStr := strings.Join(args, " ")
			for _, want := range tt.wantContains {
				assert.Contains(t, argsStr, want, "args should contain %q", want)
			}
		})
	}
}

func TestExecBuilderTTYEnvironment(t *testing.T) {
	// Test that TERM and locale vars are passed when TTY is enabled
	// This aligns with OpenSSH SendEnv defaults (TERM, LANG, LC_*)
	containerInfo := &state.ContainerInfo{
		ID:   "test-id",
		Name: "test-container",
	}

	// Set test environment variables (t.Setenv automatically restores on test completion)
	t.Setenv("TERM", "xterm-256color")
	t.Setenv("LANG", "en_US.UTF-8")

	builder := NewExecBuilder(containerInfo, nil, "/workspace")

	t.Run("TTY passes TERM and LANG", func(t *testing.T) {
		args, _ := builder.BuildArgs(ExecFlags{
			Command: []string{"bash"},
			TTY:     boolPtr(true),
		})
		argsStr := strings.Join(args, " ")
		assert.Contains(t, argsStr, "TERM=xterm-256color")
		assert.Contains(t, argsStr, "LANG=en_US.UTF-8")
	})

	t.Run("no TTY does not pass TERM", func(t *testing.T) {
		args, _ := builder.BuildArgs(ExecFlags{
			Command: []string{"echo", "test"},
			TTY:     boolPtr(false),
		})
		argsStr := strings.Join(args, " ")
		assert.NotContains(t, argsStr, "TERM=")
		assert.NotContains(t, argsStr, "LANG=")
	})
}

func TestExecBuilderUserResolution(t *testing.T) {
	tests := []struct {
		name       string
		optsUser   string
		remoteUser string
		containerUser string
		wantUser   string
	}{
		{
			name:       "opts user takes precedence",
			optsUser:   "admin",
			remoteUser: "vscode",
			containerUser: "developer",
			wantUser:   "admin",
		},
		{
			name:       "remote user over container user",
			optsUser:   "",
			remoteUser: "vscode",
			containerUser: "developer",
			wantUser:   "vscode",
		},
		{
			name:       "container user as fallback",
			optsUser:   "",
			remoteUser: "",
			containerUser: "developer",
			wantUser:   "developer",
		},
		{
			name:       "no user",
			optsUser:   "",
			remoteUser: "",
			containerUser: "",
			wantUser:   "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			containerInfo := &state.ContainerInfo{
				ID:   "test-id",
				Name: "test-container",
			}
			cfg := &devcontainer.DevContainerConfig{
				RemoteUser:    tt.remoteUser,
				ContainerUser: tt.containerUser,
			}

			builder := NewExecBuilder(containerInfo, cfg, "/workspace")
			_, user := builder.BuildArgs(ExecFlags{
				User:    tt.optsUser,
				Command: []string{"test"},
				TTY:     boolPtr(false),
			})

			assert.Equal(t, tt.wantUser, user)
		})
	}
}
