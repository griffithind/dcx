package devcontainer

import (
	"encoding/json"
	"strings"
)

// DevcontainerMetadataLabel is the image label containing devcontainer metadata.
const DevcontainerMetadataLabel = "devcontainer.metadata"

// ParseImageMetadata parses the devcontainer.metadata label value: a JSON
// array of configuration objects, or (as the reference implementation also
// accepts) a single object.
func ParseImageMetadata(labelValue string) ([]DevContainerConfig, error) {
	if strings.TrimSpace(labelValue) == "" {
		return nil, nil
	}
	var configs []DevContainerConfig
	arrErr := json.Unmarshal([]byte(labelValue), &configs)
	if arrErr == nil {
		return configs, nil
	}
	var single DevContainerConfig
	if err := json.Unmarshal([]byte(labelValue), &single); err == nil {
		return []DevContainerConfig{single}, nil
	}
	return nil, arrErr
}

// MergeMetadata merges image metadata (devcontainer.metadata label entries,
// in image-layer order) with the local devcontainer.json, which is applied
// as the final layer. Merge rules follow the spec:
//
//   - remoteUser, containerUser, workspaceFolder, workspaceMount, waitFor,
//     userEnvProbe, shutdownAction, overrideCommand, updateRemoteUserUID:
//     last value wins
//   - init, privileged: true if any layer sets true
//   - capAdd, securityOpt, forwardPorts, runArgs: union
//   - mounts: last wins per target
//   - containerEnv, remoteEnv, portsAttributes, features: last wins per key
//   - otherPortsAttributes: last wins
//   - hostRequirements: the larger of each
//   - customizations: tool-specific merge, local wins (dcx's own are never
//     taken from an image)
//
// Properties that only make sense locally (name, image, build, compose)
// come from the local config. Lifecycle commands are not merged here: each
// layer's hooks run in order (see resolveHooks). The inputs are never
// modified.
func MergeMetadata(local *DevContainerConfig, imageConfigs []DevContainerConfig) *DevContainerConfig {
	if len(imageConfigs) == 0 {
		return local
	}

	merged := *local
	layers := make([]*DevContainerConfig, 0, len(imageConfigs)+1)
	for i := range imageConfigs {
		layers = append(layers, &imageConfigs[i])
	}
	layers = append(layers, local)

	merged.ContainerEnv, merged.RemoteEnv = nil, nil
	merged.PortsAttributes, merged.Features = nil, nil
	merged.CapAdd, merged.SecurityOpt, merged.RunArgs = nil, nil, nil
	merged.ForwardPorts, merged.Mounts = nil, nil
	merged.Init, merged.Privileged = nil, nil
	merged.HostRequirements = nil

	lastString := func(dst *string, v string) {
		if v != "" {
			*dst = v
		}
	}
	lastBool := func(dst **bool, v *bool) {
		if v != nil {
			b := *v
			*dst = &b
		}
	}
	anyTrue := func(dst **bool, v *bool) {
		if v == nil {
			return
		}
		if *dst == nil || *v {
			b := *v
			*dst = &b
		}
	}

	for _, l := range layers {
		lastString(&merged.RemoteUser, l.RemoteUser)
		lastString(&merged.ContainerUser, l.ContainerUser)
		lastString(&merged.WorkspaceFolder, l.WorkspaceFolder)
		lastString(&merged.WorkspaceMount, l.WorkspaceMount)
		lastString(&merged.WaitFor, l.WaitFor)
		lastString(&merged.UserEnvProbe, l.UserEnvProbe)
		lastString(&merged.ShutdownAction, l.ShutdownAction)
		lastBool(&merged.OverrideCommand, l.OverrideCommand)
		lastBool(&merged.UpdateRemoteUserUID, l.UpdateRemoteUserUID)
		anyTrue(&merged.Init, l.Init)
		anyTrue(&merged.Privileged, l.Privileged)

		merged.CapAdd = unionStrings(merged.CapAdd, l.CapAdd)
		merged.SecurityOpt = unionStrings(merged.SecurityOpt, l.SecurityOpt)
		merged.RunArgs = unionStrings(merged.RunArgs, l.RunArgs)
		merged.ForwardPorts = merged.ForwardPorts.union(l.ForwardPorts)
		for _, m := range l.Mounts {
			merged.Mounts = mergeMountByTarget(merged.Mounts, m)
		}

		merged.HostRequirements = maxHostRequirements(merged.HostRequirements, l.HostRequirements)
		merged.ContainerEnv = mergeMap(merged.ContainerEnv, l.ContainerEnv)
		merged.RemoteEnv = mergeMap(merged.RemoteEnv, l.RemoteEnv)
		merged.PortsAttributes = mergeMap(merged.PortsAttributes, l.PortsAttributes)
		if l.OtherPortsAttributes != nil {
			merged.OtherPortsAttributes = l.OtherPortsAttributes
		}
		merged.Features = mergeMap(merged.Features, l.Features)
	}

	// Customizations: start from a copy of the local ones (which win) and
	// add what the image layers contribute.
	merged.Customizations = copyAnyMap(local.Customizations)
	for _, img := range imageConfigs {
		mergeCustomizations(&merged, &img)
	}

	return &merged
}

// maxHostRequirements merges host requirements by taking the larger of
// each (the spec's rule for image metadata): CPUs, memory, storage, and the
// stronger GPU requirement.
func maxHostRequirements(a, b *HostRequirements) *HostRequirements {
	if b == nil {
		return a
	}
	if a == nil {
		c := *b
		return &c
	}
	out := *a
	if b.CPUs > out.CPUs {
		out.CPUs = b.CPUs
	}
	out.Memory = largerSize(out.Memory, b.Memory)
	out.Storage = largerSize(out.Storage, b.Storage)
	if b.GPU != nil && (out.GPU == nil || gpuStrength(b.GPU.Mode) > gpuStrength(out.GPU.Mode)) {
		out.GPU = b.GPU
	}
	return &out
}

func largerSize(a, b string) string {
	if a == "" {
		return b
	}
	av, aerr := parseMemoryString(a)
	bv, berr := parseMemoryString(b)
	if b != "" && aerr == nil && berr == nil && bv > av {
		return b
	}
	return a
}

func gpuStrength(m GPUMode) int {
	switch m {
	case GPURequired:
		return 2
	case GPUOptional:
		return 1
	}
	return 0
}

// mergeMap copies src's entries into dst (allocated when needed).
func mergeMap[V any](dst, src map[string]V) map[string]V {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]V, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

// copyAnyMap deep-copies a JSON-decoded object so merging never mutates
// the caller's config.
func copyAnyMap(m map[string]interface{}) map[string]interface{} {
	if m == nil {
		return nil
	}
	data, err := json.Marshal(m)
	if err != nil {
		return m
	}
	var out map[string]interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return m
	}
	return out
}

// mergeCustomizations adds an image layer's customizations to target,
// keeping values already present (local wins). VS Code extensions are
// unioned and settings merged per key.
func mergeCustomizations(target, source *DevContainerConfig) {
	if target.Customizations == nil && source.Customizations != nil {
		target.Customizations = make(map[string]interface{})
	}
	for tool, sourceConfig := range source.Customizations {
		// dcx's own customizations (secrets and build secrets run commands
		// on the host) are taken from devcontainer.json only: a base image
		// must not be able to run commands on the developer's machine.
		if tool == dcxCustomizationsKey {
			continue
		}
		if targetConfig, exists := target.Customizations[tool]; exists {
			if tool == "vscode" {
				deepMergeVSCode(targetConfig, sourceConfig)
			}
		} else {
			target.Customizations[tool] = sourceConfig
		}
	}
}

// deepMergeVSCode performs deep merging for VS Code customizations per spec:
// - extensions: union arrays
// - settings: merge maps, target (local config) wins for conflicts
func deepMergeVSCode(target, source interface{}) {
	targetMap, targetOk := target.(map[string]interface{})
	sourceMap, sourceOk := source.(map[string]interface{})
	if !targetOk || !sourceOk {
		return
	}

	// Extensions: union arrays (add source extensions not in target)
	if sourceExt, ok := sourceMap["extensions"]; ok {
		sourceExtArr, sourceIsArr := sourceExt.([]interface{})
		if sourceIsArr {
			targetExtArr, _ := targetMap["extensions"].([]interface{})
			targetMap["extensions"] = unionExtensions(targetExtArr, sourceExtArr)
		}
	}

	// Settings: merge maps, target wins for conflicts
	if sourceSettings, ok := sourceMap["settings"].(map[string]interface{}); ok {
		targetSettings, targetHasSettings := targetMap["settings"].(map[string]interface{})
		if !targetHasSettings {
			targetSettings = make(map[string]interface{})
			targetMap["settings"] = targetSettings
		}
		for k, v := range sourceSettings {
			if _, exists := targetSettings[k]; !exists {
				targetSettings[k] = v
			}
		}
	}
}

// unionExtensions returns a union of extension arrays without duplicates.
func unionExtensions(target, source []interface{}) []interface{} {
	seen := make(map[string]bool)
	var result []interface{}

	// Add all target extensions first
	for _, ext := range target {
		if extStr, ok := ext.(string); ok {
			if !seen[extStr] {
				seen[extStr] = true
				result = append(result, ext)
			}
		}
	}

	// Add source extensions that aren't duplicates
	for _, ext := range source {
		if extStr, ok := ext.(string); ok {
			if !seen[extStr] {
				seen[extStr] = true
				result = append(result, ext)
			}
		}
	}

	return result
}

// unionStrings returns a followed by the elements of b not in a, without
// duplicates.
func unionStrings(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(a)+len(b))
	result := make([]string, 0, len(a)+len(b))
	for _, s := range append(append([]string(nil), a...), b...) {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	return result
}
