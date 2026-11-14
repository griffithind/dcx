package devcontainer

import (
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// DevcontainerMetadataLabel is the image label containing devcontainer metadata.
const DevcontainerMetadataLabel = "devcontainer.metadata"

// ParseImageMetadata parses the devcontainer.metadata label value: a JSON
// array of configuration objects, or (as the reference implementation also
// accepts) a single object. Properties that do not decode are skipped (see
// parseImageMetadata).
func ParseImageMetadata(labelValue string) ([]DevContainerConfig, error) {
	configs, _, err := parseImageMetadata(labelValue, nil)
	return configs, err
}

// parseImageMetadata parses a devcontainer.metadata label, substituting
// variables in each entry when ctx is set. Like the reference
// implementation, which uses entries loosely, it keeps what it can: an
// entry that is not an object, or a property whose value does not decode
// ("init": "true"), is skipped with a warning instead of dropping the whole
// label.
func parseImageMetadata(labelValue string, ctx *SubstitutionContext) ([]DevContainerConfig, []string, error) {
	if strings.TrimSpace(labelValue) == "" {
		return nil, nil, nil
	}
	tree, err := parseTree([]byte(labelValue))
	if err != nil {
		return nil, nil, err
	}
	var entries []any
	switch v := tree.(type) {
	case []any:
		entries = v
	case map[string]any:
		entries = []any{v}
	default:
		return nil, nil, fmt.Errorf("not a JSON array or object")
	}
	var configs []DevContainerConfig
	var warnings []string
	for i, e := range entries {
		obj, ok := e.(map[string]any)
		if !ok {
			warnings = append(warnings, fmt.Sprintf("entry %d is not an object; ignored", i))
			continue
		}
		for _, w := range dropMiscasedKeys(obj, reflect.TypeOf(DevContainerConfig{}), "") {
			warnings = append(warnings, fmt.Sprintf("entry %d: %s", i, w))
		}
		if ctx != nil {
			obj = substituteTree(obj, ctx).(map[string]any)
		}
		cfg, dropped := decodeTolerant(obj)
		for _, k := range dropped {
			warnings = append(warnings, fmt.Sprintf("entry %d: ignoring %q (unexpected value)", i, k))
		}
		configs = append(configs, cfg)
	}
	return configs, warnings, nil
}

// decodeTolerant decodes obj, leaving out the properties whose values do
// not decode; it returns their names.
func decodeTolerant(obj map[string]any) (DevContainerConfig, []string) {
	var cfg DevContainerConfig
	if decodeTree(obj, &cfg) == nil {
		return cfg, nil
	}
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	kept := map[string]any{}
	var dropped []string
	for _, k := range keys {
		var probe DevContainerConfig
		if decodeTree(map[string]any{k: obj[k]}, &probe) != nil {
			dropped = append(dropped, k)
			continue
		}
		kept[k] = obj[k]
	}
	cfg = DevContainerConfig{}
	_ = decodeTree(kept, &cfg)
	return cfg, dropped
}

// MergeMetadata merges image metadata (devcontainer.metadata label entries,
// in image-layer order) with the local devcontainer.json, which is applied
// as the final layer. Merge rules follow the spec:
//
//   - remoteUser, containerUser, waitFor, userEnvProbe, shutdownAction,
//     overrideCommand, updateRemoteUserUID: last value wins
//   - init, privileged: true if any layer sets true
//   - capAdd, securityOpt, forwardPorts: union
//   - mounts: last wins per target
//   - containerEnv, remoteEnv, portsAttributes: last wins per key
//   - otherPortsAttributes: last wins
//   - hostRequirements: the larger of each
//   - customizations: tool-specific merge, local wins (dcx's own are never
//     taken from an image)
//
// Everything else (name, image, build, compose, workspaceFolder,
// workspaceMount, runArgs, features, ...) comes from the local config only,
// as in the reference implementation, which takes only the properties
// above from image metadata. Lifecycle commands are not merged here: each
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
	merged.PortsAttributes = nil
	merged.CapAdd, merged.SecurityOpt = nil, nil
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
		lastString(&merged.WaitFor, l.WaitFor)
		lastString(&merged.UserEnvProbe, l.UserEnvProbe)
		lastString(&merged.ShutdownAction, l.ShutdownAction)
		lastBool(&merged.OverrideCommand, l.OverrideCommand)
		lastBool(&merged.UpdateRemoteUserUID, l.UpdateRemoteUserUID)
		anyTrue(&merged.Init, l.Init)
		anyTrue(&merged.Privileged, l.Privileged)

		merged.CapAdd = unionStrings(merged.CapAdd, l.CapAdd)
		merged.SecurityOpt = unionStrings(merged.SecurityOpt, l.SecurityOpt)
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
