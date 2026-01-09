package config

import (
	"encoding/json"

	"github.com/griffithind/dcx/internal/util"
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

// MergeMetadata merges image metadata with the local configuration.
// The merging follows spec rules:
// - Booleans: true if any source is true
// - Arrays: Union without duplicates
// - Single values: Last value wins (local config takes precedence)
func MergeMetadata(local *DevContainerConfig, imageConfigs []DevContainerConfig) *DevContainerConfig {
	if len(imageConfigs) == 0 {
		return local
	}

	// Start with a copy of local config
	merged := *local

	// Merge each image config in order (first to last)
	for _, img := range imageConfigs {
		mergeConfig(&merged, &img)
	}

	// Local config takes final precedence for single values
	applyLocalOverrides(&merged, local)

	return &merged
}

// mergeConfig merges source config into target.
func mergeConfig(target, source *DevContainerConfig) {
	// String properties: source wins if target is empty
	if target.Name == "" && source.Name != "" {
		target.Name = source.Name
	}
	if target.Image == "" && source.Image != "" {
		target.Image = source.Image
	}
	if target.WorkspaceFolder == "" && source.WorkspaceFolder != "" {
		target.WorkspaceFolder = source.WorkspaceFolder
	}
	if target.WorkspaceMount == "" && source.WorkspaceMount != "" {
		target.WorkspaceMount = source.WorkspaceMount
	}
	if target.RemoteUser == "" && source.RemoteUser != "" {
		target.RemoteUser = source.RemoteUser
	}
	if target.ContainerUser == "" && source.ContainerUser != "" {
		target.ContainerUser = source.ContainerUser
	}
	if target.WaitFor == "" && source.WaitFor != "" {
		target.WaitFor = source.WaitFor
	}
	if target.UserEnvProbe == "" && source.UserEnvProbe != "" {
		target.UserEnvProbe = source.UserEnvProbe
	}
	if target.ShutdownAction == "" && source.ShutdownAction != "" {
		target.ShutdownAction = source.ShutdownAction
	}

	// Boolean properties: true if any is true
	if source.OverrideCommand != nil && *source.OverrideCommand {
		val := true
		target.OverrideCommand = &val
	}
	if source.Init != nil && *source.Init {
		val := true
		target.Init = &val
	}
	if source.Privileged != nil && *source.Privileged {
		val := true
		target.Privileged = &val
	}
	if source.UpdateRemoteUserUID != nil && *source.UpdateRemoteUserUID {
		val := true
		target.UpdateRemoteUserUID = &val
	}

	// Array properties: union without duplicates
	target.RunArgs = util.UnionStrings(target.RunArgs, source.RunArgs)
	target.CapAdd = util.UnionStrings(target.CapAdd, source.CapAdd)
	target.SecurityOpt = util.UnionStrings(target.SecurityOpt, source.SecurityOpt)

	// ForwardPorts: union without duplicates (handles interface{} types)
	target.ForwardPorts = util.UnionInterfaces(target.ForwardPorts, source.ForwardPorts)

	// Mounts: union
	target.Mounts = unionMounts(target.Mounts, source.Mounts)

	// Map properties: merge with target taking precedence
	if target.ContainerEnv == nil && source.ContainerEnv != nil {
		target.ContainerEnv = make(map[string]string)
	}
	for k, v := range source.ContainerEnv {
		if _, exists := target.ContainerEnv[k]; !exists {
			target.ContainerEnv[k] = v
		}
	}

	if target.RemoteEnv == nil && source.RemoteEnv != nil {
		target.RemoteEnv = make(map[string]string)
	}
	for k, v := range source.RemoteEnv {
		if _, exists := target.RemoteEnv[k]; !exists {
			target.RemoteEnv[k] = v
		}
	}

	// Features: merge with target taking precedence
	if target.Features == nil && source.Features != nil {
		target.Features = make(map[string]interface{})
	}
	for k, v := range source.Features {
		if _, exists := target.Features[k]; !exists {
			target.Features[k] = v
		}
	}

	// Customizations: deep merge
	if target.Customizations == nil && source.Customizations != nil {
		target.Customizations = make(map[string]interface{})
	}
	for tool, sourceConfig := range source.Customizations {
		if targetConfig, exists := target.Customizations[tool]; exists {
			// Deep merge for VS Code customizations
			if tool == "vscode" {
				deepMergeVSCode(targetConfig, sourceConfig)
			}
			// For other tools, target takes precedence (already exists)
		} else {
			target.Customizations[tool] = sourceConfig
		}
	}

	// PortsAttributes: merge with target taking precedence
	if target.PortsAttributes == nil && source.PortsAttributes != nil {
		target.PortsAttributes = make(map[string]interface{})
	}
	for k, v := range source.PortsAttributes {
		if _, exists := target.PortsAttributes[k]; !exists {
			target.PortsAttributes[k] = v
		}
	}
}

// applyLocalOverrides ensures local config values take final precedence.
func applyLocalOverrides(merged, local *DevContainerConfig) {
	// Single values: local wins if set
	if local.Name != "" {
		merged.Name = local.Name
	}
	if local.Image != "" {
		merged.Image = local.Image
	}
	if local.WorkspaceFolder != "" {
		merged.WorkspaceFolder = local.WorkspaceFolder
	}
	if local.WorkspaceMount != "" {
		merged.WorkspaceMount = local.WorkspaceMount
	}
	if local.RemoteUser != "" {
		merged.RemoteUser = local.RemoteUser
	}
	if local.ContainerUser != "" {
		merged.ContainerUser = local.ContainerUser
	}
	if local.WaitFor != "" {
		merged.WaitFor = local.WaitFor
	}
	if local.UserEnvProbe != "" {
		merged.UserEnvProbe = local.UserEnvProbe
	}
	if local.ShutdownAction != "" {
		merged.ShutdownAction = local.ShutdownAction
	}

	// Explicit boolean overrides
	if local.OverrideCommand != nil {
		merged.OverrideCommand = local.OverrideCommand
	}
	if local.Init != nil {
		merged.Init = local.Init
	}
	if local.Privileged != nil {
		merged.Privileged = local.Privileged
	}
	if local.UpdateRemoteUserUID != nil {
		merged.UpdateRemoteUserUID = local.UpdateRemoteUserUID
	}
}

// unionMounts returns a union of mounts, deduplicating by target path.
func unionMounts(a, b []Mount) []Mount {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	var result []Mount

	for _, m := range a {
		if !seen[m.Target] {
			seen[m.Target] = true
			result = append(result, m)
		}
	}
	for _, m := range b {
		if !seen[m.Target] {
			seen[m.Target] = true
			result = append(result, m)
		}
	}

	return result
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
