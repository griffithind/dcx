package build

import (
	"encoding/json"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
)

// MetadataBuilder constructs devcontainer.metadata label content per spec.
// It merges metadata from base images, features, and local configuration
// following the precedence rules defined in the devcontainer specification.
//
// The order of metadata sources (lowest to highest precedence):
// 1. Base image metadata - from devcontainer.metadata label
// 2. Feature metadata - from each feature's devcontainer-feature.json (in installation order)
// 3. Local devcontainer.json - user's configuration (highest precedence)
type MetadataBuilder struct {
	configs []devcontainer.DevContainerConfig
}

// NewMetadataBuilder creates a new metadata builder.
func NewMetadataBuilder() *MetadataBuilder {
	return &MetadataBuilder{}
}

// WithBaseImage adds metadata from the base image's devcontainer.metadata label.
// This is the lowest precedence source.
func (b *MetadataBuilder) WithBaseImage(labelValue string) error {
	if labelValue == "" {
		return nil
	}
	configs, err := devcontainer.ParseImageMetadata(labelValue)
	if err != nil {
		return err
	}
	b.configs = append(b.configs, configs...)
	return nil
}

// WithFeatures adds metadata from installed features in installation order.
// Feature metadata has higher precedence than base image metadata.
func (b *MetadataBuilder) WithFeatures(feats []*features.Feature) {
	for _, f := range feats {
		if f.Metadata == nil {
			continue
		}
		b.configs = append(b.configs, featureToConfig(f))
	}
}

// WithLocalConfig adds the local devcontainer.json configuration.
// This has the highest precedence.
//
// Per the devcontainer spec and reference implementation (pickConfigProperties),
// the following properties are included in the metadata label from local config:
// - User: remoteUser, containerUser, updateRemoteUserUID, userEnvProbe
// - Environment: containerEnv, remoteEnv
// - Runtime: init, privileged, capAdd, securityOpt, overrideCommand, shutdownAction
// - Mounts: mounts
// - Ports: forwardPorts, portsAttributes, otherPortsAttributes
// - Lifecycle: onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand, postAttachCommand, waitFor
// - Other: hostRequirements, customizations
func (b *MetadataBuilder) WithLocalConfig(cfg *devcontainer.DevContainerConfig) {
	if cfg == nil {
		return
	}
	// Create a copy with only metadata-relevant fields per pickConfigProperties
	metaCfg := devcontainer.DevContainerConfig{
		// User configuration
		RemoteUser:          cfg.RemoteUser,
		ContainerUser:       cfg.ContainerUser,
		UpdateRemoteUserUID: cfg.UpdateRemoteUserUID,
		UserEnvProbe:        cfg.UserEnvProbe,

		// Environment
		ContainerEnv: cfg.ContainerEnv,
		RemoteEnv:    cfg.RemoteEnv,

		// Container runtime
		CapAdd:          cfg.CapAdd,
		SecurityOpt:    cfg.SecurityOpt,
		Privileged:     cfg.Privileged,
		Init:           cfg.Init,
		OverrideCommand: cfg.OverrideCommand,
		ShutdownAction:  cfg.ShutdownAction,

		// Mounts
		Mounts: cfg.Mounts,

		// Ports
		ForwardPorts:         cfg.ForwardPorts,
		PortsAttributes:      cfg.PortsAttributes,
		OtherPortsAttributes: cfg.OtherPortsAttributes,

		// Lifecycle commands
		OnCreateCommand:      cfg.OnCreateCommand,
		UpdateContentCommand: cfg.UpdateContentCommand,
		PostCreateCommand:    cfg.PostCreateCommand,
		PostStartCommand:     cfg.PostStartCommand,
		PostAttachCommand:    cfg.PostAttachCommand,
		WaitFor:              cfg.WaitFor,

		// Host requirements
		HostRequirements: cfg.HostRequirements,

		// Customizations
		Customizations: cfg.Customizations,
	}
	b.configs = append(b.configs, metaCfg)
}

// Build generates the JSON string for the devcontainer.metadata label.
func (b *MetadataBuilder) Build() (string, error) {
	if len(b.configs) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(b.configs)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

// featureToConfig converts feature metadata to DevContainerConfig format.
//
// Per the devcontainer spec and reference implementation (pickFeatureProperties),
// features contribute a SMALLER subset of properties to metadata than local config:
// - Lifecycle: onCreateCommand, updateContentCommand, postCreateCommand, postStartCommand, postAttachCommand
// - Runtime: init, privileged, capAdd, securityOpt
// - Mounts: mounts
// - Other: customizations
//
// NOTE: containerEnv from features is NOT included in metadata - it's baked into
// the image via ENV instructions in the Dockerfile. See features/dockerfile.go.
func featureToConfig(f *features.Feature) devcontainer.DevContainerConfig {
	cfg := devcontainer.DevContainerConfig{}
	if f.Metadata == nil {
		return cfg
	}

	// Container runtime (per pickFeatureProperties)
	cfg.CapAdd = f.Metadata.CapAdd
	cfg.SecurityOpt = f.Metadata.SecurityOpt

	if f.Metadata.Privileged {
		val := true
		cfg.Privileged = &val
	}
	if f.Metadata.Init {
		val := true
		cfg.Init = &val
	}

	// Mounts (per pickFeatureProperties)
	for _, fm := range f.Metadata.Mounts {
		cfg.Mounts = append(cfg.Mounts, devcontainer.Mount{
			Source: fm.Source,
			Target: fm.Target,
			Type:   fm.Type,
		})
	}

	// Lifecycle commands (per pickFeatureProperties)
	cfg.OnCreateCommand = f.Metadata.OnCreateCommand
	cfg.UpdateContentCommand = f.Metadata.UpdateContentCommand
	cfg.PostCreateCommand = f.Metadata.PostCreateCommand
	cfg.PostStartCommand = f.Metadata.PostStartCommand
	cfg.PostAttachCommand = f.Metadata.PostAttachCommand

	// Customizations (per pickFeatureProperties)
	if len(f.Metadata.Customizations) > 0 {
		cfg.Customizations = f.Metadata.Customizations
	}

	return cfg
}

// GenerateMetadataLabel is a convenience function that builds a metadata label
// from base image metadata, features, and local configuration.
func GenerateMetadataLabel(
	baseImageMetadata string,
	feats []*features.Feature,
	localConfig *devcontainer.DevContainerConfig,
) (string, error) {
	builder := NewMetadataBuilder()

	if err := builder.WithBaseImage(baseImageMetadata); err != nil {
		return "", err
	}
	builder.WithFeatures(feats)
	builder.WithLocalConfig(localConfig)

	return builder.Build()
}
