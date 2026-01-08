package labels

import (
	"testing"
	"time"
)

func TestNewLabels(t *testing.T) {
	l := NewLabels()

	if l.SchemaVersion != SchemaVersion {
		t.Errorf("expected schema version %q, got %q", SchemaVersion, l.SchemaVersion)
	}
	if !l.Managed {
		t.Error("expected Managed to be true")
	}
	if l.FeaturesInstalled == nil {
		t.Error("expected FeaturesInstalled to be initialized")
	}
	if l.FeaturesConfig == nil {
		t.Error("expected FeaturesConfig to be initialized")
	}
}

func TestLabelsToMapAndBack(t *testing.T) {
	now := time.Now().Truncate(time.Second)

	original := &Labels{
		SchemaVersion:     SchemaVersion,
		Managed:           true,
		WorkspaceID:       "ws-123",
		WorkspaceName:     "my-project",
		WorkspacePath:     "/home/user/project",
		ConfigPath:        ".devcontainer/devcontainer.json",
		HashConfig:        "abc123",
		HashDockerfile:    "def456",
		HashCompose:       "ghi789",
		HashFeatures:      "jkl012",
		HashOverall:       "mno345",
		CreatedAt:         now,
		CreatedBy:         "dcx/1.0.0",
		LastStartedAt:     now,
		LifecycleState:    LifecycleStateReady,
		FeaturesInstalled: []string{"ghcr.io/devcontainers/features/go:1", "ghcr.io/devcontainers/features/node:1"},
		FeaturesConfig: map[string]map[string]interface{}{
			"go":   {"version": "1.21"},
			"node": {"version": "20"},
		},
		BaseImage:       "mcr.microsoft.com/devcontainers/base:ubuntu",
		DerivedImage:    "dcx-derived-abc123",
		BuildMethod:     BuildMethodDockerfile,
		ComposeProject:  "my-project",
		ComposeService:  "devcontainer",
		IsPrimary:       true,
		CacheData: &CacheData{
			ConfigHash:  "abc123",
			LastChecked: now,
		},
		CacheFeatureDigests: map[string]string{
			"go":   "sha256:abc",
			"node": "sha256:def",
		},
	}

	// Convert to map
	m := original.ToMap()

	// Verify required labels are present
	if m[LabelSchemaVersion] != SchemaVersion {
		t.Errorf("expected schema version %q, got %q", SchemaVersion, m[LabelSchemaVersion])
	}
	if m[LabelManaged] != "true" {
		t.Errorf("expected managed=true, got %q", m[LabelManaged])
	}
	if m[LabelWorkspaceID] != "ws-123" {
		t.Errorf("expected workspace ID %q, got %q", "ws-123", m[LabelWorkspaceID])
	}

	// Convert back
	restored := FromMap(m)

	// Verify all fields
	if restored.SchemaVersion != original.SchemaVersion {
		t.Errorf("SchemaVersion: expected %q, got %q", original.SchemaVersion, restored.SchemaVersion)
	}
	if restored.Managed != original.Managed {
		t.Errorf("Managed: expected %v, got %v", original.Managed, restored.Managed)
	}
	if restored.WorkspaceID != original.WorkspaceID {
		t.Errorf("WorkspaceID: expected %q, got %q", original.WorkspaceID, restored.WorkspaceID)
	}
	if restored.WorkspaceName != original.WorkspaceName {
		t.Errorf("WorkspaceName: expected %q, got %q", original.WorkspaceName, restored.WorkspaceName)
	}
	if restored.WorkspacePath != original.WorkspacePath {
		t.Errorf("WorkspacePath: expected %q, got %q", original.WorkspacePath, restored.WorkspacePath)
	}
	if restored.ConfigPath != original.ConfigPath {
		t.Errorf("ConfigPath: expected %q, got %q", original.ConfigPath, restored.ConfigPath)
	}
	if restored.HashConfig != original.HashConfig {
		t.Errorf("HashConfig: expected %q, got %q", original.HashConfig, restored.HashConfig)
	}
	if restored.HashDockerfile != original.HashDockerfile {
		t.Errorf("HashDockerfile: expected %q, got %q", original.HashDockerfile, restored.HashDockerfile)
	}
	if restored.BuildMethod != original.BuildMethod {
		t.Errorf("BuildMethod: expected %q, got %q", original.BuildMethod, restored.BuildMethod)
	}
	if restored.IsPrimary != original.IsPrimary {
		t.Errorf("IsPrimary: expected %v, got %v", original.IsPrimary, restored.IsPrimary)
	}
	if len(restored.FeaturesInstalled) != len(original.FeaturesInstalled) {
		t.Errorf("FeaturesInstalled length: expected %d, got %d", len(original.FeaturesInstalled), len(restored.FeaturesInstalled))
	}
	if restored.CacheData == nil {
		t.Error("CacheData should not be nil")
	} else if restored.CacheData.ConfigHash != original.CacheData.ConfigHash {
		t.Errorf("CacheData.ConfigHash: expected %q, got %q", original.CacheData.ConfigHash, restored.CacheData.ConfigHash)
	}
}

func TestIsLegacy(t *testing.T) {
	tests := []struct {
		name     string
		labels   map[string]string
		expected bool
	}{
		{
			name: "legacy labels only",
			labels: map[string]string{
				"io.github.dcx.managed":        "true",
				"io.github.dcx.env_key":        "abc",
				"io.github.dcx.workspace_path": "/path",
			},
			expected: true,
		},
		{
			name: "new labels only",
			labels: map[string]string{
				LabelManaged:       "true",
				LabelWorkspaceID:   "abc",
				LabelWorkspacePath: "/path",
			},
			expected: false,
		},
		{
			name: "both labels present (migrated)",
			labels: map[string]string{
				"io.github.dcx.managed": "true",
				LabelManaged:            "true",
			},
			expected: false, // New labels take precedence
		},
		{
			name:     "empty labels",
			labels:   map[string]string{},
			expected: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			result := IsLegacy(tc.labels)
			if result != tc.expected {
				t.Errorf("expected %v, got %v", tc.expected, result)
			}
		})
	}
}

func TestMigrateFromLegacy(t *testing.T) {
	legacy := map[string]string{
		"io.github.dcx.managed":        "true",
		"io.github.dcx.env_key":        "ws-abc123",
		"io.github.dcx.workspace_path": "/home/user/project",
		"io.github.dcx.config_hash":    "hash123",
		"io.github.dcx.plan":           "compose",
		"io.github.dcx.primary":        "true",
		"io.github.dcx.compose_project": "my-project",
		"io.github.dcx.primary_service": "devcontainer",
		"io.github.dcx.project_name":    "My Project",
	}

	migrated := MigrateFromLegacy(legacy)

	if !migrated.Managed {
		t.Error("expected Managed to be true")
	}
	if migrated.WorkspaceID != "ws-abc123" {
		t.Errorf("expected WorkspaceID %q, got %q", "ws-abc123", migrated.WorkspaceID)
	}
	if migrated.WorkspacePath != "/home/user/project" {
		t.Errorf("expected WorkspacePath %q, got %q", "/home/user/project", migrated.WorkspacePath)
	}
	if migrated.HashConfig != "hash123" {
		t.Errorf("expected HashConfig %q, got %q", "hash123", migrated.HashConfig)
	}
	if migrated.BuildMethod != BuildMethodCompose {
		t.Errorf("expected BuildMethod %q, got %q", BuildMethodCompose, migrated.BuildMethod)
	}
	if !migrated.IsPrimary {
		t.Error("expected IsPrimary to be true")
	}
	if migrated.ComposeProject != "my-project" {
		t.Errorf("expected ComposeProject %q, got %q", "my-project", migrated.ComposeProject)
	}
	if migrated.ComposeService != "devcontainer" {
		t.Errorf("expected ComposeService %q, got %q", "devcontainer", migrated.ComposeService)
	}
	if migrated.WorkspaceName != "My Project" {
		t.Errorf("expected WorkspaceName %q, got %q", "My Project", migrated.WorkspaceName)
	}
}

func TestFilterFunctions(t *testing.T) {
	selector := FilterSelector()
	expected := "label=com.griffithind.dcx.managed=true"
	if selector != expected {
		t.Errorf("expected %q, got %q", expected, selector)
	}

	wsFilter := FilterByWorkspace("ws-123")
	expected = "label=com.griffithind.dcx.workspace.id=ws-123"
	if wsFilter != expected {
		t.Errorf("expected %q, got %q", expected, wsFilter)
	}

	composeFilter := FilterByComposeProject("my-project")
	expected = "label=com.griffithind.dcx.compose.project=my-project"
	if composeFilter != expected {
		t.Errorf("expected %q, got %q", expected, composeFilter)
	}
}

func TestMergeLabels(t *testing.T) {
	base := map[string]string{
		LabelManaged:     "true",
		LabelWorkspaceID: "ws-123",
	}

	additional := map[string]string{
		"my.custom.label":    "value",
		LabelManaged:         "false", // Should not override
		"another.label":      "another",
	}

	merged := MergeLabels(base, additional)

	// Base labels preserved
	if merged[LabelManaged] != "true" {
		t.Errorf("expected Managed to be preserved as %q, got %q", "true", merged[LabelManaged])
	}
	if merged[LabelWorkspaceID] != "ws-123" {
		t.Errorf("expected WorkspaceID %q, got %q", "ws-123", merged[LabelWorkspaceID])
	}

	// Custom labels added
	if merged["my.custom.label"] != "value" {
		t.Errorf("expected custom label %q, got %q", "value", merged["my.custom.label"])
	}
	if merged["another.label"] != "another" {
		t.Errorf("expected another label %q, got %q", "another", merged["another.label"])
	}
}

func TestLabelConstants(t *testing.T) {
	// Verify prefix is correct
	if Prefix != "com.griffithind.dcx" {
		t.Errorf("expected prefix %q, got %q", "com.griffithind.dcx", Prefix)
	}

	// Verify all labels use the correct prefix
	labels := []string{
		LabelSchemaVersion,
		LabelManaged,
		LabelWorkspaceID,
		LabelWorkspaceName,
		LabelWorkspacePath,
		LabelConfigPath,
		LabelHashConfig,
		LabelHashDockerfile,
		LabelHashCompose,
		LabelHashFeatures,
		LabelHashOverall,
		LabelCreatedAt,
		LabelCreatedBy,
		LabelLastStartedAt,
		LabelLifecycleState,
		LabelFeaturesInstalled,
		LabelFeaturesConfig,
		LabelBaseImage,
		LabelDerivedImage,
		LabelBuildMethod,
		LabelComposeProject,
		LabelComposeService,
		LabelIsPrimary,
		LabelCacheData,
		LabelCacheFeatureDigests,
	}

	for _, label := range labels {
		if len(label) < len(Prefix) || label[:len(Prefix)] != Prefix {
			t.Errorf("label %q does not start with prefix %q", label, Prefix)
		}
	}
}
