package docker

import "github.com/griffithind/dcx/internal/util"

// Label constants for dcx-managed containers.
// All labels use the io.github.dcx namespace.
const (
	// LabelPrefix is the namespace prefix for all dcx labels.
	LabelPrefix = "io.github.dcx."

	// LabelManaged indicates this container is managed by dcx.
	LabelManaged = LabelPrefix + "managed"

	// LabelEnvKey is the stable identifier derived from the workspace path.
	LabelEnvKey = LabelPrefix + "env_key"

	// LabelWorkspaceRootHash is the full hash of the workspace path.
	LabelWorkspaceRootHash = LabelPrefix + "workspace_root_hash"

	// LabelWorkspacePath is the absolute path to the workspace.
	LabelWorkspacePath = LabelPrefix + "workspace_path"

	// LabelConfigHash is the hash of the devcontainer configuration.
	LabelConfigHash = LabelPrefix + "config_hash"

	// LabelPlan indicates the execution plan type (compose or single).
	LabelPlan = LabelPrefix + "plan"

	// LabelVersion is the dcx label schema version.
	LabelVersion = LabelPrefix + "version"

	// LabelPrimary indicates this is the primary devcontainer.
	LabelPrimary = LabelPrefix + "primary"

	// LabelComposeProject is the compose project name for compose plans.
	LabelComposeProject = LabelPrefix + "compose_project"

	// LabelPrimaryService is the primary service name for compose plans.
	LabelPrimaryService = LabelPrefix + "primary_service"

	// LabelImageTag is the deterministic image tag for single plans.
	LabelImageTag = LabelPrefix + "image_tag"

	// LabelProjectName is the user-defined project name from dcx.json.
	LabelProjectName = LabelPrefix + "project_name"
)

// Plan types
const (
	PlanCompose = "compose"
	PlanSingle  = "single"
)

// LabelSchemaVersion is the current version of the label schema.
const LabelSchemaVersion = "1"

// Labels represents the set of labels applied to dcx-managed containers.
type Labels struct {
	Managed           bool
	EnvKey            string
	WorkspaceRootHash string
	WorkspacePath     string
	ConfigHash        string
	Plan              string
	Version           string
	Primary           bool
	ComposeProject    string
	PrimaryService    string
	ImageTag          string
	ProjectName       string
}

// ToMap converts Labels to a map of string key-value pairs.
func (l Labels) ToMap() map[string]string {
	m := map[string]string{
		LabelManaged: util.BoolToString(l.Managed),
		LabelEnvKey:  l.EnvKey,
		LabelVersion: l.Version,
	}

	if l.WorkspaceRootHash != "" {
		m[LabelWorkspaceRootHash] = l.WorkspaceRootHash
	}
	if l.WorkspacePath != "" {
		m[LabelWorkspacePath] = l.WorkspacePath
	}
	if l.ConfigHash != "" {
		m[LabelConfigHash] = l.ConfigHash
	}
	if l.Plan != "" {
		m[LabelPlan] = l.Plan
	}
	if l.Primary {
		m[LabelPrimary] = "true"
	}
	if l.ComposeProject != "" {
		m[LabelComposeProject] = l.ComposeProject
	}
	if l.PrimaryService != "" {
		m[LabelPrimaryService] = l.PrimaryService
	}
	if l.ImageTag != "" {
		m[LabelImageTag] = l.ImageTag
	}
	if l.ProjectName != "" {
		m[LabelProjectName] = l.ProjectName
	}

	return m
}

// LabelsFromMap creates Labels from a map.
func LabelsFromMap(m map[string]string) Labels {
	return Labels{
		Managed:           m[LabelManaged] == "true",
		EnvKey:            m[LabelEnvKey],
		WorkspaceRootHash: m[LabelWorkspaceRootHash],
		WorkspacePath:     m[LabelWorkspacePath],
		ConfigHash:        m[LabelConfigHash],
		Plan:              m[LabelPlan],
		Version:           m[LabelVersion],
		Primary:           m[LabelPrimary] == "true",
		ComposeProject:    m[LabelComposeProject],
		PrimaryService:    m[LabelPrimaryService],
		ImageTag:          m[LabelImageTag],
		ProjectName:       m[LabelProjectName],
	}
}

