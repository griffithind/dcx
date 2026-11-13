package devcontainer

import (
	"encoding/json"
	"fmt"
)

// DevcontainerMetadataLabel is the image label containing devcontainer metadata.
const DevcontainerMetadataLabel = "devcontainer.metadata"

// ParseImageMetadata parses the devcontainer.metadata label value.
// The label contains a JSON array of configuration objects.
func ParseImageMetadata(labelValue string) ([]DevContainerConfig, error) {
	if labelValue == "" {
		return nil, nil
	}

	var configs []DevContainerConfig
	if err := json.Unmarshal([]byte(labelValue), &configs); err != nil {
		return nil, err
	}
	return configs, nil
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
//   - customizations: tool-specific merge, local wins
//
// Properties that only make sense locally (name, image, build, compose,
// lifecycle commands) come from the local config. The inputs are never
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
		merged.ForwardPorts = unionInterfaces(merged.ForwardPorts, l.ForwardPorts)
		for _, m := range l.Mounts {
			merged.Mounts = mergeMountByTarget(merged.Mounts, m)
		}

		merged.ContainerEnv = mergeStringMap(merged.ContainerEnv, l.ContainerEnv)
		merged.RemoteEnv = mergeStringMap(merged.RemoteEnv, l.RemoteEnv)
		merged.PortsAttributes = mergeAnyMap(merged.PortsAttributes, l.PortsAttributes)
		merged.Features = mergeAnyMap(merged.Features, l.Features)
	}

	// Customizations: start from a copy of the local ones (which win) and
	// add what the image layers contribute.
	merged.Customizations = copyAnyMap(local.Customizations)
	for _, img := range imageConfigs {
		mergeCustomizations(&merged, &img)
	}

	return &merged
}

func mergeStringMap(dst, src map[string]string) map[string]string {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]string, len(src))
	}
	for k, v := range src {
		dst[k] = v
	}
	return dst
}

func mergeAnyMap(dst, src map[string]interface{}) map[string]interface{} {
	if len(src) == 0 {
		return dst
	}
	if dst == nil {
		dst = make(map[string]interface{}, len(src))
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

// unionInterfaces is unionStrings for JSON arrays (compared by value).
func unionInterfaces(a, b interface{}) []interface{} {
	seen := make(map[string]bool)
	result := []interface{}{}
	for _, items := range []interface{}{a, b} {
		arr, _ := items.([]interface{})
		for _, item := range arr {
			key := fmt.Sprint(item)
			if !seen[key] {
				seen[key] = true
				result = append(result, item)
			}
		}
	}
	return result
}
