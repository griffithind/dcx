package devcontainer

import (
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecyclecmd"
)

// HookSource says where a lifecycle hook comes from.
type HookSource string

const (
	// HookFromImage is a devcontainer.metadata entry of the base image.
	HookFromImage HookSource = "image"
	// HookFromFeature is a feature's devcontainer-feature.json.
	HookFromFeature HookSource = "feature"
	// HookFromConfig is devcontainer.json.
	HookFromConfig HookSource = "devcontainer.json"
)

// Hook is one lifecycle command and where it comes from.
type Hook struct {
	Source HookSource
	// Feature is the feature's display name (HookFromFeature).
	Feature string
	Command *lifecyclecmd.Command
}

// Origin describes the hook in progress messages: "", "feature 'Node.js'"
// or "image metadata".
func (h Hook) Origin() string {
	switch h.Source {
	case HookFromFeature:
		return "feature '" + h.Feature + "'"
	case HookFromImage:
		return "image metadata"
	}
	return ""
}

// Hooks are the lifecycle commands of every phase, in the order the spec
// runs them: base image metadata entries, then features in install order,
// then devcontainer.json. initializeCommand only comes from
// devcontainer.json (it runs on the host).
type Hooks struct {
	Initialize    []Hook
	OnCreate      []Hook
	UpdateContent []Hook
	PostCreate    []Hook
	PostStart     []Hook
	PostAttach    []Hook
}

// lifecycleCommands is the per-phase view of a config or feature.
type lifecycleCommands struct {
	onCreate, updateContent, postCreate, postStart, postAttach *lifecyclecmd.Command
}

func (h *Hooks) add(source HookSource, feature string, c lifecycleCommands) {
	add := func(dst *[]Hook, cmd *lifecyclecmd.Command) {
		if cmd != nil && len(cmd.Steps()) > 0 {
			*dst = append(*dst, Hook{Source: source, Feature: feature, Command: cmd})
		}
	}
	add(&h.OnCreate, c.onCreate)
	add(&h.UpdateContent, c.updateContent)
	add(&h.PostCreate, c.postCreate)
	add(&h.PostStart, c.postStart)
	add(&h.PostAttach, c.postAttach)
}

func configCommands(c *DevContainerConfig) lifecycleCommands {
	return lifecycleCommands{c.OnCreateCommand, c.UpdateContentCommand, c.PostCreateCommand, c.PostStartCommand, c.PostAttachCommand}
}

// resolveHooks collects the hooks of image metadata, features and the
// local config.
func resolveHooks(local *DevContainerConfig, imageConfigs []DevContainerConfig, feats []*features.Feature) Hooks {
	var h Hooks
	for i := range imageConfigs {
		h.add(HookFromImage, "", configCommands(&imageConfigs[i]))
	}
	for _, f := range feats {
		m := f.Metadata
		if m == nil {
			continue
		}
		name := m.Name
		if name == "" {
			name = m.ID
		}
		h.add(HookFromFeature, name, lifecycleCommands{m.OnCreateCommand, m.UpdateContentCommand, m.PostCreateCommand, m.PostStartCommand, m.PostAttachCommand})
	}
	h.add(HookFromConfig, "", configCommands(local))
	h.Initialize = InitializeHooks(local)
	return h
}

// InitializeHooks returns the initializeCommand of devcontainer.json, the
// only place it comes from (it runs on the host, before resolution).
func InitializeHooks(local *DevContainerConfig) []Hook {
	if cmd := local.InitializeCommand; cmd != nil && len(cmd.Steps()) > 0 {
		return []Hook{{Source: HookFromConfig, Command: cmd}}
	}
	return nil
}
