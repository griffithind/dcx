package workspace

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/config"
	"github.com/griffithind/dcx/internal/docker"
	"github.com/griffithind/dcx/internal/util"
)

func TestComputeID(t *testing.T) {
	id1 := ComputeID("/home/user/project1")
	id2 := ComputeID("/home/user/project2")
	id3 := ComputeID("/home/user/project1")

	if id1 == id2 {
		t.Error("different paths should produce different IDs")
	}
	if id1 != id3 {
		t.Error("same path should produce same ID")
	}
	// ID should be 12 lowercase base32 characters
	if len(id1) != 12 {
		t.Errorf("ID should be 12 base32 chars, got %d", len(id1))
	}
}

func TestComputeName(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		config   *config.DevContainerConfig
		expected string
	}{
		{
			name:     "from config name",
			path:     "/home/user/project",
			config:   &config.DevContainerConfig{Name: "My Project"},
			expected: "My Project",
		},
		{
			name:     "from path",
			path:     "/home/user/my-project",
			config:   &config.DevContainerConfig{},
			expected: "my-project",
		},
		{
			name:     "nil config",
			path:     "/home/user/another-project",
			config:   nil,
			expected: "another-project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := ComputeName(tc.path, tc.config)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestGetPlanType(t *testing.T) {
	tests := []struct {
		name     string
		config   *config.DevContainerConfig
		expected PlanType
	}{
		{
			name:     "image plan",
			config:   &config.DevContainerConfig{Image: "ubuntu:latest"},
			expected: PlanTypeImage,
		},
		{
			name: "dockerfile plan",
			config: &config.DevContainerConfig{
				Build: &config.BuildConfig{Dockerfile: "Dockerfile"},
			},
			expected: PlanTypeDockerfile,
		},
		{
			name: "compose plan",
			config: &config.DevContainerConfig{
				DockerComposeFile: "docker-compose.yml",
				Service:           "app",
			},
			expected: PlanTypeCompose,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := GetPlanType(tc.config)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestBuilderBasicBuild(t *testing.T) {
	builder := NewBuilder(nil)

	cfg := &config.DevContainerConfig{
		Name:            "test-project",
		Image:           "ubuntu:latest",
		WorkspaceFolder: "/workspace",
		ContainerEnv:    map[string]string{"FOO": "bar"},
		RemoteUser:      "vscode",
	}

	ws, err := builder.Build(context.Background(), BuildOptions{
		ConfigPath:    "/home/user/project/.devcontainer/devcontainer.json",
		WorkspaceRoot: "/home/user/project",
		Config:        cfg,
	})

	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if ws.ID == "" {
		t.Error("ID should be set")
	}
	if ws.Name != "test-project" {
		t.Errorf("expected name %q, got %q", "test-project", ws.Name)
	}
	if ws.Resolved.PlanType != PlanTypeImage {
		t.Errorf("expected plan type %v, got %v", PlanTypeImage, ws.Resolved.PlanType)
	}
	if ws.Resolved.Image != "ubuntu:latest" {
		t.Errorf("expected image %q, got %q", "ubuntu:latest", ws.Resolved.Image)
	}
	if ws.Resolved.WorkspaceFolder != "/workspace" {
		t.Errorf("expected workspace folder %q, got %q", "/workspace", ws.Resolved.WorkspaceFolder)
	}
	if ws.Resolved.ContainerEnv["FOO"] != "bar" {
		t.Error("container env not set")
	}
	if ws.Resolved.RemoteUser != "vscode" {
		t.Errorf("expected remote user %q, got %q", "vscode", ws.Resolved.RemoteUser)
	}
	if ws.Hashes.Config == "" {
		t.Error("config hash should be computed")
	}
	if ws.Hashes.Overall == "" {
		t.Error("overall hash should be computed")
	}
}

func TestBuilderDockerfileBuild(t *testing.T) {
	builder := NewBuilder(nil)

	cfg := &config.DevContainerConfig{
		Build: &config.BuildConfig{
			Dockerfile: "Dockerfile",
			Context:    ".",
			Args:       map[string]string{"VERSION": "1.0"},
		},
	}

	ws, err := builder.Build(context.Background(), BuildOptions{
		ConfigPath:    "/home/user/project/.devcontainer/devcontainer.json",
		WorkspaceRoot: "/home/user/project",
		Config:        cfg,
	})

	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if ws.Resolved.PlanType != PlanTypeDockerfile {
		t.Errorf("expected plan type %v, got %v", PlanTypeDockerfile, ws.Resolved.PlanType)
	}
	if ws.Resolved.Dockerfile == nil {
		t.Fatal("dockerfile plan should be set")
	}
	if ws.Resolved.Dockerfile.Args["VERSION"] != "1.0" {
		t.Error("build args not set")
	}
}

func TestBuilderComposeBuild(t *testing.T) {
	builder := NewBuilder(nil)

	cfg := &config.DevContainerConfig{
		DockerComposeFile: []string{"docker-compose.yml"},
		Service:           "app",
		RunServices:       []string{"db", "redis"},
	}

	ws, err := builder.Build(context.Background(), BuildOptions{
		ConfigPath:    "/home/user/project/.devcontainer/devcontainer.json",
		WorkspaceRoot: "/home/user/project",
		Config:        cfg,
	})

	if err != nil {
		t.Fatalf("build failed: %v", err)
	}

	if ws.Resolved.PlanType != PlanTypeCompose {
		t.Errorf("expected plan type %v, got %v", PlanTypeCompose, ws.Resolved.PlanType)
	}
	if ws.Resolved.Compose == nil {
		t.Fatal("compose plan should be set")
	}
	if ws.Resolved.Compose.Service != "app" {
		t.Errorf("expected service %q, got %q", "app", ws.Resolved.Compose.Service)
	}
	if len(ws.Resolved.Compose.RunServices) != 2 {
		t.Errorf("expected 2 run services, got %d", len(ws.Resolved.Compose.RunServices))
	}
}

func TestVariableSubstitution(t *testing.T) {
	ctx := &config.SubstitutionContext{
		LocalWorkspaceFolder:     "/home/user/project",
		ContainerWorkspaceFolder: "/workspace",
		DevcontainerID:           "abc123",
		UserHome:                 "/home/user",
		LocalEnv: func(key string) string {
			if key == "MY_VAR" {
				return "my-value"
			}
			return ""
		},
	}

	tests := []struct {
		input    string
		expected string
	}{
		{"${localWorkspaceFolder}", "/home/user/project"},
		{"${containerWorkspaceFolder}", "/workspace"},
		{"${localWorkspaceFolderBasename}", "project"},
		{"${containerWorkspaceFolderBasename}", "workspace"},
		{"${devcontainerId}", "abc123"},
		{"${userHome}", "/home/user"},
		{"${localEnv:MY_VAR}", "my-value"},
		{"${localEnv:MISSING}", ""},
		{"${localEnv:MISSING:default}", "default"},
		{"${env:MY_VAR}", "my-value"},
		{"prefix-${localWorkspaceFolder}-suffix", "prefix-/home/user/project-suffix"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := config.Substitute(tc.input, ctx)
			if result != tc.expected {
				t.Errorf("input %q: expected %q, got %q", tc.input, tc.expected, result)
			}
		})
	}
}

func TestParseLifecycleHooks(t *testing.T) {
	t.Run("string command", func(t *testing.T) {
		cfg := &config.DevContainerConfig{
			OnCreateCommand: "npm install",
		}
		hooks := parseLifecycleHooks(cfg)
		if len(hooks.OnCreate) != 1 {
			t.Fatalf("expected 1 command, got %d", len(hooks.OnCreate))
		}
		if hooks.OnCreate[0].Command != "npm install" {
			t.Errorf("expected %q, got %q", "npm install", hooks.OnCreate[0].Command)
		}
	})

	t.Run("array command", func(t *testing.T) {
		cfg := &config.DevContainerConfig{
			OnCreateCommand: []interface{}{"npm", "install"},
		}
		hooks := parseLifecycleHooks(cfg)
		if len(hooks.OnCreate) != 1 {
			t.Fatalf("expected 1 command, got %d", len(hooks.OnCreate))
		}
		if len(hooks.OnCreate[0].Args) != 2 {
			t.Errorf("expected 2 args, got %d", len(hooks.OnCreate[0].Args))
		}
	})

	t.Run("map command", func(t *testing.T) {
		cfg := &config.DevContainerConfig{
			OnCreateCommand: map[string]interface{}{
				"install": "npm install",
				"build":   "npm run build",
			},
		}
		hooks := parseLifecycleHooks(cfg)
		if len(hooks.OnCreate) != 2 {
			t.Fatalf("expected 2 commands, got %d", len(hooks.OnCreate))
		}
		for _, cmd := range hooks.OnCreate {
			if !cmd.Parallel {
				t.Error("map commands should be parallel")
			}
		}
	})
}

func TestParsePortForwards(t *testing.T) {
	tests := []struct {
		input    []string
		expected []PortForward
	}{
		{
			input: []string{"8080:8080"},
			expected: []PortForward{{HostPort: 8080, ContainerPort: 8080}},
		},
		{
			input: []string{"3000:80"},
			expected: []PortForward{{HostPort: 3000, ContainerPort: 80}},
		},
		{
			input: []string{"8080"},
			expected: []PortForward{{HostPort: 8080, ContainerPort: 8080}},
		},
	}

	for _, tc := range tests {
		result := parsePortForwards(tc.input)
		if len(result) != len(tc.expected) {
			t.Errorf("expected %d ports, got %d", len(tc.expected), len(result))
			continue
		}
		for i := range result {
			if result[i].HostPort != tc.expected[i].HostPort ||
				result[i].ContainerPort != tc.expected[i].ContainerPort {
				t.Errorf("port %d: expected %+v, got %+v", i, tc.expected[i], result[i])
			}
		}
	}
}

func TestSanitizeProjectName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"my-project", "my-project"},
		{"My Project", "my_project"},
		{"my_project", "my_project"},
		{"My Project 123", "my_project_123"},
		{"Special@#$chars", "specialchars"},
	}

	for _, tc := range tests {
		t.Run(tc.input, func(t *testing.T) {
			result := docker.SanitizeProjectName(tc.input)
			if result != tc.expected {
				t.Errorf("expected %q, got %q", tc.expected, result)
			}
		})
	}
}

func TestUnionStrings(t *testing.T) {
	a := []string{"a", "b", "c"}
	b := []string{"b", "c", "d"}

	result := util.UnionStrings(a, b)

	if len(result) != 4 {
		t.Errorf("expected 4 items, got %d", len(result))
	}

	expected := map[string]bool{"a": true, "b": true, "c": true, "d": true}
	for _, s := range result {
		if !expected[s] {
			t.Errorf("unexpected item: %s", s)
		}
	}
}

func TestDeepMergeCustomizations(t *testing.T) {
	target := map[string]interface{}{
		"vscode": map[string]interface{}{
			"extensions": []interface{}{"ext1"},
			"settings": map[string]interface{}{
				"key1": "value1",
			},
		},
	}

	source := map[string]interface{}{
		"vscode": map[string]interface{}{
			"extensions": []interface{}{"ext2"},
			"settings": map[string]interface{}{
				"key2": "value2",
			},
		},
	}

	deepMergeCustomizations(target, source)

	vscode := target["vscode"].(map[string]interface{})
	extensions := vscode["extensions"].([]interface{})
	settings := vscode["settings"].(map[string]interface{})

	if len(extensions) != 2 {
		t.Errorf("expected 2 extensions, got %d", len(extensions))
	}
	if settings["key1"] != "value1" {
		t.Error("key1 should be preserved")
	}
	if settings["key2"] != "value2" {
		t.Error("key2 should be added")
	}
}

func TestRemoteUserSubstitution_LocalEnvVariable(t *testing.T) {
	// Test that remoteUser and containerUser support variable substitution
	cfg := &config.DevContainerConfig{
		Name:          "test-remoteuser-substitution",
		Image:         "alpine",
		RemoteUser:    "${localEnv:USER}",
		ContainerUser: "${localEnv:CONTAINER_USER}",
	}

	builder := &Builder{}
	ws, err := builder.Build(context.Background(), BuildOptions{
		Config:        cfg,
		ConfigPath:    "/tmp/test/.devcontainer/devcontainer.json",
		WorkspaceRoot: "/tmp/test",
		SubstitutionContext: &config.SubstitutionContext{
			LocalWorkspaceFolder:     "/tmp/test",
			ContainerWorkspaceFolder: "/workspace",
			LocalEnv: func(key string) string {
				switch key {
				case "USER":
					return "testuser"
				case "CONTAINER_USER":
					return "containeruser"
				}
				return ""
			},
		},
	})

	if err != nil {
		t.Fatalf("failed to build workspace: %v", err)
	}

	if ws.Resolved.RemoteUser != "testuser" {
		t.Errorf("expected remoteUser to be 'testuser', got %q", ws.Resolved.RemoteUser)
	}
	if ws.Resolved.ContainerUser != "containeruser" {
		t.Errorf("expected containerUser to be 'containeruser', got %q", ws.Resolved.ContainerUser)
	}
}
