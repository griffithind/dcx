package state

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Label prefix and schema version.
const (
	// Prefix is the namespace prefix for all dcx labels.
	Prefix = "com.griffithind.dcx"

	// SchemaVersion is the current version of the label schema.
	SchemaVersion = "2"
)

// Identity labels.
const (
	// LabelSchemaVersion identifies the label schema version.
	LabelSchemaVersion = Prefix + ".schema.version"

	// LabelManaged indicates this container is managed by dcx.
	LabelManaged = Prefix + ".managed"

	// LabelWorkspaceID is the stable identifier for the workspace.
	LabelWorkspaceID = Prefix + ".workspace.id"

	// LabelWorkspaceName is the human-readable workspace name.
	LabelWorkspaceName = Prefix + ".workspace.name"

	// LabelWorkspacePath is the absolute path to the workspace.
	LabelWorkspacePath = Prefix + ".workspace.path"

	// LabelConfigPath is the absolute path to the devcontainer.json.
	LabelConfigPath = Prefix + ".config.path"
)

// Staleness and setup labels.
const (
	// LabelHashConfig is the semantic staleness key of the config the
	// container was created from (see devcontainer.ComputeConfigHash).
	LabelHashConfig = Prefix + ".hash.config"

	// LabelHashInputs is the hash of everything that determined the
	// container (substituted values, feature digests); a difference from
	// the current one is drift, reported but not acted on.
	LabelHashInputs = Prefix + ".hash.inputs"

	// LabelHashVersion identifies the algorithm behind LabelHashConfig.
	// Containers without it were created by a dcx version whose hashes are
	// not comparable with the current ones.
	LabelHashVersion = Prefix + ".hash.version"

	// LabelLifecycleMarkers is set on containers whose lifecycle progress is
	// tracked with marker files (see service markers). Containers without it
	// predate markers and are treated as fully set up.
	LabelLifecycleMarkers = Prefix + ".lifecycle.markers"
)

// Build labels.
const (
	// LabelFeaturesInstalled is a JSON array of installed feature IDs.
	LabelFeaturesInstalled = Prefix + ".features.installed"

	// LabelBaseImage is the original base image reference.
	LabelBaseImage = Prefix + ".build.base.image"

	// LabelDerivedImage is the image after dcx's feature/UID layers.
	LabelDerivedImage = Prefix + ".build.derived.image"

	// LabelBuildMethod is the plan type: "image", "dockerfile" or "compose".
	LabelBuildMethod = Prefix + ".build.method"
)

// Build methods (values of LabelBuildMethod).
const (
	BuildMethodImage      = "image"
	BuildMethodDockerfile = "dockerfile"
	BuildMethodCompose    = "compose"
)

// SSH labels: how the dcx-agent is reached, fixed when the container is
// created (Docker publishes ports only at create time).
const (
	// LabelSSHPort is the host loopback port the agent is reachable on.
	LabelSSHPort = Prefix + ".ssh.port"

	// LabelSSHListen is the agent's listen address inside the container
	// ("0.0.0.0:48022" when the port is published, "127.0.0.1:<port>" for
	// host networking).
	LabelSSHListen = Prefix + ".ssh.listen"

	// LabelSSHAllow is the comma-separated list of CIDRs beyond loopback
	// the agent accepts (`dcx up --hosts`); absent means loopback only.
	LabelSSHAllow = Prefix + ".ssh.allow"
)

// LabelShutdownAction records shutdownAction as resolved at create time
// (image metadata included); devcontainer.json's own value wins.
const LabelShutdownAction = Prefix + ".shutdown.action"

// Compose labels.
const (
	// LabelComposeProject is the compose project name.
	LabelComposeProject = Prefix + ".compose.project"

	// LabelComposeService is the service name within the compose project.
	LabelComposeService = Prefix + ".compose.service"

	// LabelIsPrimary marks the primary devcontainer (the compose service
	// dcx attaches to; always set for single-container plans).
	LabelIsPrimary = Prefix + ".container.primary"

	// Labels docker compose sets on the containers it creates.
	composeLabelConfigFiles = "com.docker.compose.project.config_files"
	composeLabelWorkingDir  = "com.docker.compose.project.working_dir"
)

// ContainerLabels are the dcx labels of a container.
type ContainerLabels struct {
	SchemaVersion string

	// Identity
	Managed       bool
	WorkspaceID   string
	WorkspaceName string
	WorkspacePath string
	ConfigPath    string

	// Staleness
	HashConfig  string
	HashInputs  string
	HashVersion string

	// LifecycleMarkers reports whether lifecycle progress is tracked with
	// marker files inside the container.
	LifecycleMarkers bool

	// Build
	FeaturesInstalled []string
	BaseImage         string
	DerivedImage      string
	BuildMethod       string

	// SSH (see LabelSSHPort). Zero values on containers created by older
	// dcx versions: the port is then looked up from Docker and the agent
	// listens on agentproto.ListenPort.
	SSHPort         int
	SSHListen       string
	SSHAllowedCIDRs []string

	// ShutdownAction is shutdownAction as resolved at create time.
	ShutdownAction string

	// Compose
	ComposeProject string
	ComposeService string
	IsPrimary      bool

	// Recorded by docker compose itself (read-only, never written by dcx):
	// the -f files and project directory the container was created with.
	ComposeConfigFiles []string
	ComposeWorkingDir  string
}

// NewContainerLabels creates ContainerLabels for a new dcx container.
func NewContainerLabels() *ContainerLabels {
	return &ContainerLabels{
		SchemaVersion:     SchemaVersion,
		Managed:           true,
		FeaturesInstalled: []string{},
	}
}

// ToMap converts ContainerLabels to a label map for container creation.
func (l *ContainerLabels) ToMap() map[string]string {
	m := map[string]string{
		LabelSchemaVersion: l.SchemaVersion,
		LabelManaged:       "false",
	}
	if l.Managed {
		m[LabelManaged] = "true"
	}

	setIfNotEmpty(m, LabelWorkspaceID, l.WorkspaceID)
	setIfNotEmpty(m, LabelWorkspaceName, l.WorkspaceName)
	setIfNotEmpty(m, LabelWorkspacePath, l.WorkspacePath)
	setIfNotEmpty(m, LabelConfigPath, l.ConfigPath)

	setIfNotEmpty(m, LabelHashConfig, l.HashConfig)
	setIfNotEmpty(m, LabelHashInputs, l.HashInputs)
	setIfNotEmpty(m, LabelHashVersion, l.HashVersion)
	if l.LifecycleMarkers {
		m[LabelLifecycleMarkers] = "1"
	}

	if len(l.FeaturesInstalled) > 0 {
		if data, err := json.Marshal(l.FeaturesInstalled); err == nil {
			m[LabelFeaturesInstalled] = string(data)
		}
	}
	setIfNotEmpty(m, LabelBaseImage, l.BaseImage)
	setIfNotEmpty(m, LabelDerivedImage, l.DerivedImage)
	setIfNotEmpty(m, LabelBuildMethod, l.BuildMethod)

	if l.SSHPort > 0 {
		m[LabelSSHPort] = strconv.Itoa(l.SSHPort)
	}
	setIfNotEmpty(m, LabelSSHListen, l.SSHListen)
	setIfNotEmpty(m, LabelSSHAllow, strings.Join(l.SSHAllowedCIDRs, ","))

	setIfNotEmpty(m, LabelShutdownAction, l.ShutdownAction)
	setIfNotEmpty(m, LabelComposeProject, l.ComposeProject)
	setIfNotEmpty(m, LabelComposeService, l.ComposeService)
	if l.IsPrimary {
		m[LabelIsPrimary] = "true"
	}
	return m
}

// ContainerLabelsFromMap parses a container's label map.
func ContainerLabelsFromMap(m map[string]string) *ContainerLabels {
	l := NewContainerLabels()
	l.SchemaVersion = m[LabelSchemaVersion]
	l.Managed = m[LabelManaged] == "true"

	l.WorkspaceID = m[LabelWorkspaceID]
	l.WorkspaceName = m[LabelWorkspaceName]
	l.WorkspacePath = m[LabelWorkspacePath]
	l.ConfigPath = m[LabelConfigPath]

	l.HashConfig = m[LabelHashConfig]
	l.HashInputs = m[LabelHashInputs]
	l.HashVersion = m[LabelHashVersion]
	l.LifecycleMarkers = m[LabelLifecycleMarkers] == "1"

	if data := m[LabelFeaturesInstalled]; data != "" {
		_ = json.Unmarshal([]byte(data), &l.FeaturesInstalled)
	}
	l.BaseImage = m[LabelBaseImage]
	l.DerivedImage = m[LabelDerivedImage]
	l.BuildMethod = m[LabelBuildMethod]

	l.SSHPort, _ = strconv.Atoi(m[LabelSSHPort])
	l.SSHListen = m[LabelSSHListen]
	if allow := m[LabelSSHAllow]; allow != "" {
		l.SSHAllowedCIDRs = strings.Split(allow, ",")
	}

	l.ShutdownAction = m[LabelShutdownAction]
	l.ComposeProject = m[LabelComposeProject]
	l.ComposeService = m[LabelComposeService]
	l.IsPrimary = m[LabelIsPrimary] == "true"
	if files := m[composeLabelConfigFiles]; files != "" {
		l.ComposeConfigFiles = strings.Split(files, ",")
	}
	l.ComposeWorkingDir = m[composeLabelWorkingDir]
	return l
}

func setIfNotEmpty(m map[string]string, key, value string) {
	if value != "" {
		m[key] = value
	}
}
