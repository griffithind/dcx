package build

import (
	"encoding/json"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
)

func TestMetadataBuilder_Empty(t *testing.T) {
	builder := NewMetadataBuilder()
	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty array, got %s", result)
	}
}

func TestMetadataBuilder_BaseImageOnly(t *testing.T) {
	builder := NewMetadataBuilder()
	err := builder.WithBaseImage(`[{"remoteUser": "vscode"}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if configs[0].RemoteUser != "vscode" {
		t.Errorf("expected remoteUser=vscode, got %s", configs[0].RemoteUser)
	}
}

func TestMetadataBuilder_BaseImageInvalid(t *testing.T) {
	builder := NewMetadataBuilder()
	err := builder.WithBaseImage(`not valid json`)
	if err == nil {
		t.Fatal("expected error for invalid JSON")
	}
}

func TestMetadataBuilder_BaseImageEmpty(t *testing.T) {
	builder := NewMetadataBuilder()
	err := builder.WithBaseImage("")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty array, got %s", result)
	}
}

func TestMetadataBuilder_FeaturesOnly(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "test-feature",
			Metadata: &features.FeatureMetadata{
				ID:     "test-feature",
				CapAdd: []string{"SYS_PTRACE"},
				// Note: containerEnv is intentionally NOT tested here because
				// per the spec (pickFeatureProperties), containerEnv from features
				// goes via ENV instructions in the Dockerfile, NOT metadata.
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if len(configs[0].CapAdd) != 1 || configs[0].CapAdd[0] != "SYS_PTRACE" {
		t.Errorf("expected capAdd=[SYS_PTRACE], got %v", configs[0].CapAdd)
	}
}

func TestMetadataBuilder_FeaturesNilMetadata(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID:       "no-metadata",
			Metadata: nil,
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty array for nil metadata, got %s", result)
	}
}

func TestMetadataBuilder_LocalConfigOnly(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		RemoteUser:    "developer",
		ContainerUser: "root",
		ContainerEnv: map[string]string{
			"HOME": "/home/developer",
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if configs[0].RemoteUser != "developer" {
		t.Errorf("expected remoteUser=developer, got %s", configs[0].RemoteUser)
	}
	if configs[0].ContainerUser != "root" {
		t.Errorf("expected containerUser=root, got %s", configs[0].ContainerUser)
	}
}

func TestMetadataBuilder_LocalConfigNil(t *testing.T) {
	builder := NewMetadataBuilder()
	builder.WithLocalConfig(nil)

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty array, got %s", result)
	}
}

func TestMetadataBuilder_FullStack(t *testing.T) {
	builder := NewMetadataBuilder()

	// Base image metadata (lowest precedence)
	err := builder.WithBaseImage(`[{"remoteUser": "ubuntu", "containerEnv": {"BASE_VAR": "base"}}]`)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	// Feature metadata (middle precedence)
	builder.WithFeatures([]*features.Feature{
		{
			ID: "feature1",
			Metadata: &features.FeatureMetadata{
				ID:     "feature1",
				CapAdd: []string{"SYS_PTRACE"},
			},
		},
		{
			ID: "feature2",
			Metadata: &features.FeatureMetadata{
				ID:          "feature2",
				SecurityOpt: []string{"seccomp=unconfined"},
			},
		},
	})

	// Local config (highest precedence)
	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		RemoteUser: "vscode",
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	// Should have 4 entries: base + 2 features + local
	if len(configs) != 4 {
		t.Errorf("expected 4 configs, got %d", len(configs))
	}

	// Order should be: base, feature1, feature2, local
	if configs[0].RemoteUser != "ubuntu" {
		t.Errorf("expected first config remoteUser=ubuntu, got %s", configs[0].RemoteUser)
	}
	if len(configs[1].CapAdd) == 0 || configs[1].CapAdd[0] != "SYS_PTRACE" {
		t.Errorf("expected second config capAdd=[SYS_PTRACE], got %v", configs[1].CapAdd)
	}
	if len(configs[2].SecurityOpt) == 0 || configs[2].SecurityOpt[0] != "seccomp=unconfined" {
		t.Errorf("expected third config securityOpt=[seccomp=unconfined], got %v", configs[2].SecurityOpt)
	}
	if configs[3].RemoteUser != "vscode" {
		t.Errorf("expected fourth config remoteUser=vscode, got %s", configs[3].RemoteUser)
	}
}

func TestMetadataBuilder_PrivilegedFeature(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "privileged-feature",
			Metadata: &features.FeatureMetadata{
				ID:         "privileged-feature",
				Privileged: true,
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Privileged == nil || !*configs[0].Privileged {
		t.Errorf("expected privileged=true")
	}
}

func TestMetadataBuilder_InitFeature(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "init-feature",
			Metadata: &features.FeatureMetadata{
				ID:   "init-feature",
				Init: true,
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if configs[0].Init == nil || !*configs[0].Init {
		t.Errorf("expected init=true")
	}
}

func TestMetadataBuilder_FeatureMounts(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "mount-feature",
			Metadata: &features.FeatureMetadata{
				ID: "mount-feature",
				Mounts: []features.FeatureMount{
					{
						Source: "/var/run/docker.sock",
						Target: "/var/run/docker.sock",
						Type:   "bind",
					},
				},
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Errorf("expected 1 config, got %d", len(configs))
	}
	if len(configs[0].Mounts) != 1 {
		t.Errorf("expected 1 mount, got %d", len(configs[0].Mounts))
	}
	if configs[0].Mounts[0].Source != "/var/run/docker.sock" {
		t.Errorf("expected mount source=/var/run/docker.sock, got %s", configs[0].Mounts[0].Source)
	}
}

func TestGenerateMetadataLabel(t *testing.T) {
	result, err := GenerateMetadataLabel(
		`[{"remoteUser": "base"}]`,
		[]*features.Feature{
			{
				ID: "feature",
				Metadata: &features.FeatureMetadata{
					ID:     "feature",
					CapAdd: []string{"NET_ADMIN"},
				},
			},
		},
		&devcontainer.DevContainerConfig{
			RemoteUser: "local",
		},
	)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 3 {
		t.Errorf("expected 3 configs, got %d", len(configs))
	}
}

func TestGenerateMetadataLabel_AllNil(t *testing.T) {
	result, err := GenerateMetadataLabel("", nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != "[]" {
		t.Errorf("expected empty array, got %s", result)
	}
}

func TestMetadataBuilder_FeatureLifecycleCommands(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "lifecycle-feature",
			Metadata: &features.FeatureMetadata{
				ID:                "lifecycle-feature",
				OnCreateCommand:   "echo 'onCreate from feature'",
				PostCreateCommand: "echo 'postCreate from feature'",
				PostStartCommand:  []string{"echo", "postStart from feature"},
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	// Check lifecycle commands are preserved
	if configs[0].OnCreateCommand != "echo 'onCreate from feature'" {
		t.Errorf("expected onCreateCommand, got %v", configs[0].OnCreateCommand)
	}
	if configs[0].PostCreateCommand != "echo 'postCreate from feature'" {
		t.Errorf("expected postCreateCommand, got %v", configs[0].PostCreateCommand)
	}
	// PostStartCommand is an array
	arr, ok := configs[0].PostStartCommand.([]interface{})
	if !ok {
		t.Errorf("expected postStartCommand to be array, got %T", configs[0].PostStartCommand)
	} else if len(arr) != 2 {
		t.Errorf("expected postStartCommand array length 2, got %d", len(arr))
	}
}

func TestMetadataBuilder_LocalConfigLifecycleCommands(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		OnCreateCommand:      "npm install",
		UpdateContentCommand: "npm run build",
		PostCreateCommand:    map[string]interface{}{"server": "npm start", "client": "npm run dev"},
		PostStartCommand:     "echo 'started'",
		PostAttachCommand:    "echo 'attached'",
		WaitFor:              "postCreateCommand",
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if configs[0].OnCreateCommand != "npm install" {
		t.Errorf("expected onCreateCommand='npm install', got %v", configs[0].OnCreateCommand)
	}
	if configs[0].UpdateContentCommand != "npm run build" {
		t.Errorf("expected updateContentCommand='npm run build', got %v", configs[0].UpdateContentCommand)
	}
	if configs[0].WaitFor != "postCreateCommand" {
		t.Errorf("expected waitFor='postCreateCommand', got %s", configs[0].WaitFor)
	}
}

func TestMetadataBuilder_LocalConfigPortForwarding(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		ForwardPorts: []interface{}{3000, "8080:80"},
		PortsAttributes: map[string]interface{}{
			"3000": map[string]interface{}{
				"label":         "Frontend",
				"onAutoForward": "notify",
			},
		},
		OtherPortsAttributes: map[string]interface{}{
			"onAutoForward": "silent",
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if len(configs[0].ForwardPorts) != 2 {
		t.Errorf("expected 2 forwardPorts, got %d", len(configs[0].ForwardPorts))
	}
	if configs[0].PortsAttributes == nil {
		t.Error("expected portsAttributes to be set")
	}
	if configs[0].OtherPortsAttributes == nil {
		t.Error("expected otherPortsAttributes to be set")
	}
}

func TestMetadataBuilder_LocalConfigHostRequirements(t *testing.T) {
	builder := NewMetadataBuilder()

	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		HostRequirements: &devcontainer.HostRequirements{
			CPUs:   4,
			Memory: "8gb",
			GPU:    true,
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if configs[0].HostRequirements == nil {
		t.Fatal("expected hostRequirements to be set")
	}
	if configs[0].HostRequirements.CPUs != 4 {
		t.Errorf("expected cpus=4, got %d", configs[0].HostRequirements.CPUs)
	}
	if configs[0].HostRequirements.Memory != "8gb" {
		t.Errorf("expected memory='8gb', got %s", configs[0].HostRequirements.Memory)
	}
}

func TestMetadataBuilder_LocalConfigUserSettings(t *testing.T) {
	builder := NewMetadataBuilder()

	updateUID := true
	overrideCmd := false
	builder.WithLocalConfig(&devcontainer.DevContainerConfig{
		RemoteUser:          "developer",
		ContainerUser:       "root",
		UpdateRemoteUserUID: &updateUID,
		UserEnvProbe:        "loginInteractiveShell",
		OverrideCommand:     &overrideCmd,
		ShutdownAction:      "stopContainer",
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	if configs[0].RemoteUser != "developer" {
		t.Errorf("expected remoteUser='developer', got %s", configs[0].RemoteUser)
	}
	if configs[0].UpdateRemoteUserUID == nil || !*configs[0].UpdateRemoteUserUID {
		t.Error("expected updateRemoteUserUID=true")
	}
	if configs[0].UserEnvProbe != "loginInteractiveShell" {
		t.Errorf("expected userEnvProbe='loginInteractiveShell', got %s", configs[0].UserEnvProbe)
	}
	if configs[0].OverrideCommand == nil || *configs[0].OverrideCommand {
		t.Error("expected overrideCommand=false")
	}
	if configs[0].ShutdownAction != "stopContainer" {
		t.Errorf("expected shutdownAction='stopContainer', got %s", configs[0].ShutdownAction)
	}
}

func TestMetadataBuilder_FeatureContainerEnvNotInMetadata(t *testing.T) {
	// This test verifies the fix: containerEnv from features should NOT appear in metadata
	// because it's baked into the image via ENV instructions in the Dockerfile.
	builder := NewMetadataBuilder()

	builder.WithFeatures([]*features.Feature{
		{
			ID: "env-feature",
			Metadata: &features.FeatureMetadata{
				ID: "env-feature",
				ContainerEnv: map[string]string{
					"FEATURE_VAR": "should_not_appear_in_metadata",
				},
				CapAdd: []string{"NET_ADMIN"}, // This SHOULD appear
			},
		},
	})

	result, err := builder.Build()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var configs []devcontainer.DevContainerConfig
	if err := json.Unmarshal([]byte(result), &configs); err != nil {
		t.Fatalf("failed to parse result: %v", err)
	}

	if len(configs) != 1 {
		t.Fatalf("expected 1 config, got %d", len(configs))
	}

	// containerEnv should NOT be in metadata from features
	if len(configs[0].ContainerEnv) > 0 {
		t.Errorf("containerEnv from features should NOT appear in metadata, got %v", configs[0].ContainerEnv)
	}

	// But capAdd SHOULD be there
	if len(configs[0].CapAdd) != 1 || configs[0].CapAdd[0] != "NET_ADMIN" {
		t.Errorf("expected capAdd=[NET_ADMIN], got %v", configs[0].CapAdd)
	}
}
