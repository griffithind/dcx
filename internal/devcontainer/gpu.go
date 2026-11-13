package devcontainer

import (
	"encoding/json"
	"fmt"
)

// GPUMode is how a config asks for GPUs.
type GPUMode int

const (
	// GPUNone asks for no GPUs (false, an empty object, or absent).
	GPUNone GPUMode = iota
	// GPURequired fails without a GPU runtime on the Docker host.
	GPURequired
	// GPUOptional uses GPUs only when the Docker host has a GPU runtime.
	GPUOptional
)

// GPURequirement is hostRequirements.gpu: true/false, "optional", or an
// object ({"count", "cores", "memory"}, a requirement unless empty).
type GPURequirement struct {
	Mode   GPUMode
	Count  int
	Cores  int
	Memory string

	raw json.RawMessage // marshalled back as written
}

// UnmarshalJSON accepts the spec's forms (and the string "true").
func (g *GPURequirement) UnmarshalJSON(data []byte) error {
	var v interface{}
	if err := json.Unmarshal(data, &v); err != nil {
		return err
	}
	*g = GPURequirement{raw: append(json.RawMessage(nil), data...)}
	switch t := v.(type) {
	case bool:
		if t {
			g.Mode = GPURequired
		}
	case string:
		switch t {
		case "optional":
			g.Mode = GPUOptional
		case "true":
			g.Mode = GPURequired
		}
	case map[string]interface{}:
		if len(t) == 0 {
			return nil
		}
		g.Mode = GPURequired
		var obj struct {
			Count  int    `json:"count"`
			Cores  int    `json:"cores"`
			Memory string `json:"memory"`
		}
		if err := json.Unmarshal(data, &obj); err != nil {
			return fmt.Errorf("hostRequirements.gpu: %w", err)
		}
		g.Count, g.Cores, g.Memory = obj.Count, obj.Cores, obj.Memory
	case nil:
	default:
		return fmt.Errorf("hostRequirements.gpu: expected a boolean, \"optional\" or an object, got %s", data)
	}
	return nil
}

// MarshalJSON writes the requirement as it was read.
func (g GPURequirement) MarshalJSON() ([]byte, error) {
	if g.raw != nil {
		return g.raw, nil
	}
	switch g.Mode {
	case GPUOptional:
		return json.Marshal("optional")
	case GPURequired:
		if g.Count == 0 && g.Cores == 0 && g.Memory == "" {
			return json.Marshal(true)
		}
		return json.Marshal(map[string]interface{}{"count": g.Count, "cores": g.Cores, "memory": g.Memory})
	}
	return json.Marshal(false)
}

// Wanted reports whether the requirement asks for GPUs at all.
func (g *GPURequirement) Wanted() bool { return g != nil && g.Mode != GPUNone }
