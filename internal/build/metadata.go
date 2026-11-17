package build

import (
	"encoding/json"
	"fmt"

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
//
// As devcontainers/cli writes it, the label holds the base image's entries
// verbatim, then the features', then devcontainer.json as written (before
// variable substitution, so nothing host-specific is baked in: whoever
// reads the label substitutes it), with string mounts kept as strings.
type MetadataBuilder struct {
	entries []json.RawMessage
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
	var entries []json.RawMessage
	if err := json.Unmarshal([]byte(labelValue), &entries); err != nil {
		// A single object rather than an array.
		var entry map[string]json.RawMessage
		if err := json.Unmarshal([]byte(labelValue), &entry); err != nil {
			return fmt.Errorf("invalid devcontainer.metadata label: %w", err)
		}
		entries = []json.RawMessage{json.RawMessage(labelValue)}
	}
	b.entries = append(b.entries, entries...)
	return nil
}

// add appends cfg as an entry, keeping string mounts as written (Mount
// marshals as an object, which drops options dcx does not model).
func (b *MetadataBuilder) add(cfg devcontainer.DevContainerConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	if len(cfg.Mounts) > 0 {
		var fields map[string]json.RawMessage
		if err := json.Unmarshal(data, &fields); err != nil {
			return err
		}
		mounts := make([]any, len(cfg.Mounts))
		for i, m := range cfg.Mounts {
			if m.Raw != "" {
				mounts[i] = m.Raw
			} else {
				mounts[i] = m
			}
		}
		if fields["mounts"], err = json.Marshal(mounts); err != nil {
			return err
		}
		if data, err = json.Marshal(fields); err != nil {
			return err
		}
	}
	if string(data) != "{}" {
		b.entries = append(b.entries, data)
	}
	return nil
}

// WithFeatures adds metadata from installed features in installation order.
// Feature metadata has higher precedence than base image metadata.
func (b *MetadataBuilder) WithFeatures(feats []*features.Feature) error {
	for _, f := range feats {
		if f.Metadata == nil {
			continue
		}
		if err := b.add(featureToConfig(f)); err != nil {
			return err
		}
	}
	return nil
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
func (b *MetadataBuilder) WithLocalConfig(cfg *devcontainer.DevContainerConfig) error {
	if cfg == nil {
		return nil
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
		SecurityOpt:     cfg.SecurityOpt,
		Privileged:      cfg.Privileged,
		Init:            cfg.Init,
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
	return b.add(metaCfg)
}

// Build generates the JSON string for the devcontainer.metadata label.
func (b *MetadataBuilder) Build() (string, error) {
	if len(b.entries) == 0 {
		return "[]", nil
	}
	data, err := json.Marshal(b.entries)
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
	// The id (the reference as devcontainer.json writes it, as
	// devcontainers/cli records it) and entrypoint let tools that read the
	// label (dcx included, for a prebuilt image) know which features it has
	// and run their entrypoints.
	cfg.ID = f.ID
	cfg.Entrypoint = f.Metadata.Entrypoint

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
	cfg.Mounts = append(cfg.Mounts, f.Metadata.Mounts...)

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
// from base image metadata, features, and devcontainer.json as written
// (resolved.Raw, unsubstituted).
func GenerateMetadataLabel(
	baseImageMetadata string,
	feats []*features.Feature,
	localConfig *devcontainer.DevContainerConfig,
) (string, error) {
	builder := NewMetadataBuilder()

	if err := builder.WithBaseImage(baseImageMetadata); err != nil {
		return "", err
	}
	if err := builder.WithFeatures(feats); err != nil {
		return "", err
	}
	if err := builder.WithLocalConfig(localConfig); err != nil {
		return "", err
	}
	return builder.Build()
}
