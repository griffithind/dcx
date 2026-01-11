// Package common provides shared utilities and constants used across dcx packages.
package common

const (
	// SSHHostSuffix is the suffix appended to workspace IDs for SSH host names.
	// SSH hosts are formatted as "<workspaceID>.dcx" for easy access.
	SSHHostSuffix = ".dcx"

	// HashTruncationLength is the number of characters used when truncating hashes for image tags.
	// This provides a good balance between uniqueness and readability.
	HashTruncationLength = 12

	// ImageTagPrefix is the prefix for dcx-built images.
	// Format: dcx/{workspaceID}:{hash}
	ImageTagPrefix = "dcx/"

	// AgentBinaryPath is the path where dcx-agent is deployed in containers.
	AgentBinaryPath = "/tmp/dcx-agent"

	// SecretsDir is the directory where secrets are mounted in containers.
	SecretsDir = "/run/secrets"
)
