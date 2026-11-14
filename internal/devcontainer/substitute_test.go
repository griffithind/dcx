package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDetermineContainerWorkspaceFolder(t *testing.T) {
	tests := []struct {
		name           string
		cfg            *DevContainerConfig
		localWorkspace string
		want           string
	}{
		{
			name: "explicit workspaceFolder takes precedence",
			cfg: &DevContainerConfig{
				WorkspaceFolder: "/custom/path",
				Image:           "alpine",
			},
			localWorkspace: "/home/user/project",
			want:           "/custom/path",
		},
		{
			name: "image plan defaults to /workspaces/<basename>",
			cfg: &DevContainerConfig{
				Image: "alpine",
			},
			localWorkspace: "/home/user/my-project",
			want:           "/workspaces/my-project",
		},
		{
			name: "dockerfile plan defaults to /workspaces/<basename>",
			cfg: &DevContainerConfig{
				Build: &BuildConfig{
					Dockerfile: "Dockerfile",
				},
			},
			localWorkspace: "/home/user/my-app",
			want:           "/workspaces/my-app",
		},
		{
			name: "compose plan defaults to / (per spec)",
			cfg: &DevContainerConfig{
				DockerComposeFile: []string{"docker-compose.yml"},
				Service:           "app",
			},
			localWorkspace: "/home/user/compose-project",
			want:           "/",
		},
		{
			name: "compose with explicit workspaceFolder",
			cfg: &DevContainerConfig{
				DockerComposeFile: []string{"docker-compose.yml"},
				Service:           "app",
				WorkspaceFolder:   "/workspace",
			},
			localWorkspace: "/home/user/project",
			want:           "/workspace",
		},
		{
			name: "handles workspace with trailing slash",
			cfg: &DevContainerConfig{
				Image: "alpine",
			},
			localWorkspace: "/home/user/project/",
			want:           "/workspaces/project",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := DetermineContainerWorkspaceFolder(tt.cfg, tt.localWorkspace)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestSubstitute(t *testing.T) {
	tests := []struct {
		name  string
		input string
		ctx   *SubstitutionContext
		want  string
	}{
		{
			name:  "no substitution needed",
			input: "plain text",
			ctx:   &SubstitutionContext{},
			want:  "plain text",
		},
		{
			name:  "localWorkspaceFolder",
			input: "${localWorkspaceFolder}/src",
			ctx: &SubstitutionContext{
				LocalWorkspaceFolder: "/home/user/project",
			},
			want: "/home/user/project/src",
		},
		{
			name:  "containerWorkspaceFolder",
			input: "${containerWorkspaceFolder}/build",
			ctx: &SubstitutionContext{
				ContainerWorkspaceFolder: "/workspaces/project",
			},
			want: "/workspaces/project/build",
		},
		{
			name:  "localEnv with value",
			input: "${localEnv:HOME}/test",
			ctx: &SubstitutionContext{
				LookupEnv: func(key string) (string, bool) {
					if key == "HOME" {
						return "/home/testuser", true
					}
					return "", false
				},
			},
			want: "/home/testuser/test",
		},
		{
			name:  "localEnv with default",
			input: "${localEnv:MISSING:defaultvalue}",
			ctx: &SubstitutionContext{
				LookupEnv: func(key string) (string, bool) { return "", false },
			},
			want: "defaultvalue",
		},
		{
			// The default applies only when the variable is unset.
			name:  "localEnv set to empty keeps empty",
			input: "${localEnv:EMPTY:docker.io}/app",
			ctx: &SubstitutionContext{
				LookupEnv: func(key string) (string, bool) { return "", key == "EMPTY" },
			},
			want: "/app",
		},
		{
			name:  "default may contain colons",
			input: "${localEnv:URL:http://localhost:8080}",
			ctx:   &SubstitutionContext{LookupEnv: func(string) (string, bool) { return "", false }},
			want:  "http://localhost:8080",
		},
		{
			// One pass: a value that itself looks like a reference is not
			// expanded again.
			name:  "substituted values are not re-expanded",
			input: "${localEnv:TRICKY}",
			ctx: &SubstitutionContext{
				UserHome:  "/home/me",
				LookupEnv: func(string) (string, bool) { return "${userHome}", true },
			},
			want: "${userHome}",
		},
		{
			name:  "unknown references and a nameless localEnv are kept",
			input: "${nope} ${localEnv}",
			ctx:   &SubstitutionContext{},
			want:  "${nope} ${localEnv}",
		},
		{
			name:  "basename of the compose default / is empty",
			input: "[${containerWorkspaceFolderBasename}]",
			ctx:   &SubstitutionContext{ContainerWorkspaceFolder: "/"},
			want:  "[]",
		},
		{
			// Resolved at exec time (ResolveContainerEnvRefs), not here.
			name:  "containerEnv is left for exec time",
			input: "path=${containerEnv:PATH}",
			ctx:   &SubstitutionContext{},
			want:  "path=${containerEnv:PATH}",
		},
		{
			name:  "multiple substitutions",
			input: "${localWorkspaceFolder}:${containerWorkspaceFolder}",
			ctx: &SubstitutionContext{
				LocalWorkspaceFolder:     "/local",
				ContainerWorkspaceFolder: "/container",
			},
			want: "/local:/container",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Substitute(tt.input, tt.ctx)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestResolveContainerEnvRefs(t *testing.T) {
	got := ResolveContainerEnvRefs(map[string]string{
		"PATH":     "${containerEnv:PATH}:/custom/bin",
		"MISSING":  "${containerEnv:NOPE:fallback}",
		"EMPTY":    "x${containerEnv:NOPE}y",
		"SETEMPTY": "${containerEnv:BLANK:fallback}",
		"PLAIN":    "keep ${SHELL_VAR} as-is",
	}, map[string]string{"PATH": "/usr/local/bin:/usr/bin", "BLANK": ""})

	want := map[string]string{
		"PATH":     "/usr/local/bin:/usr/bin:/custom/bin",
		"MISSING":  "fallback",
		"EMPTY":    "xy",
		"SETEMPTY": "",
		"PLAIN":    "keep ${SHELL_VAR} as-is",
	}
	if len(got) != len(want) {
		t.Fatalf("got %v", got)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s = %q, want %q", k, got[k], v)
		}
	}
}

// Load must substitute identically whether the config is auto-discovered or
// passed explicitly with --config.
func TestLoadSubstitutesWithExplicitConfigPath(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer", "variant")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	cfgJSON := `{"image": "alpine:${localEnv:DCX_TEST_TAG:3.20}", "workspaceFolder": "/src/${localWorkspaceFolderBasename}"}`
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(cfgJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	cfg, _, err := Load(ws, ".devcontainer/variant/devcontainer.json")
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Image != "alpine:3.20" {
		t.Errorf("image = %q", cfg.Image)
	}
	if cfg.WorkspaceFolder != "/src/"+filepath.Base(ws) {
		t.Errorf("workspaceFolder = %q", cfg.WorkspaceFolder)
	}
}

// Every string of the config is substituted once, with the full context:
// feature options (they reach install scripts and the staleness key),
// customizations and the other properties the reference implementation
// substitutes, while numbers and booleans are left alone.
func TestLoadSubstitutesEveryString(t *testing.T) {
	t.Setenv("DCX_TEST_VERSION", "20")
	t.Setenv("DCX_TEST_MEM", "8gb")
	ws := t.TempDir()
	require.NoError(t, os.MkdirAll(filepath.Join(ws, ".devcontainer"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, ".devcontainer", "devcontainer.json"), []byte(`{
		"image": "alpine:3",
		"features": {
			"ghcr.io/devcontainers/features/node:1": {"version": "${localEnv:DCX_TEST_VERSION}", "cache": "/cache/${devcontainerId}", "enabled": true},
			"ghcr.io/devcontainers/features/git:1": true
		},
		"hostRequirements": {"memory": "${localEnv:DCX_TEST_MEM}"},
		"customizations": {"dcx": {"secrets": {"TOKEN": "cat ${localWorkspaceFolder}/token"}}},
		"containerEnv": {"PORT": 3000}
	}`), 0o644))

	loaded, err := LoadConfig(ws, "")
	require.NoError(t, err)
	cfg := loaded.Config
	opts := cfg.Features["ghcr.io/devcontainers/features/node:1"].(map[string]interface{})
	assert.Equal(t, "20", opts["version"])
	assert.Equal(t, "/cache/"+ComputeID(ws), opts["cache"])
	assert.Equal(t, true, opts["enabled"])
	assert.Equal(t, true, cfg.Features["ghcr.io/devcontainers/features/git:1"])
	assert.Equal(t, "8gb", cfg.HostRequirements.Memory)
	assert.Equal(t, SecretConfig("cat "+ws+"/token"), cfg.DcxCustomizations().Secrets["TOKEN"])
	assert.Equal(t, "3000", cfg.ContainerEnv["PORT"])

	assert.Equal(t, "${localEnv:DCX_TEST_MEM}", loaded.Raw.HostRequirements.Memory, "Raw is the file as written")
}
