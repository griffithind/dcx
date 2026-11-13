// Package mount holds the mount type shared by devcontainer.json, image
// metadata and feature metadata, and its only string parser.
package mount

import (
	"encoding/json"
	"fmt"
	"strings"
)

// Mount is a mount specification, written as a string (Docker --mount or
// -v syntax) or an object, in devcontainer.json, image metadata and
// feature metadata alike.
type Mount struct {
	Source      string `json:"source,omitempty"`
	Target      string `json:"target,omitempty"`
	Type        string `json:"type,omitempty"` // bind, volume, tmpfs
	ReadOnly    bool   `json:"readonly,omitempty"`
	Consistency string `json:"consistency,omitempty"` // cached, delegated, consistent (macOS)
	// Raw holds the original string if mount was specified as a string
	Raw string `json:"-"`
	// Options are --mount options dcx does not model (bind-propagation,
	// volume-subpath, volume-nocopy, volume-opt, tmpfs-size, tmpfs-mode),
	// as key=value, passed on rather than dropped.
	Options []string `json:"-"`
	// Relabel asks Docker to relabel a bind mount's source for SELinux
	// (":z"); dcx sets it on the workspace mount when the daemon labels
	// containers. Never read from a config.
	Relabel bool `json:"-"`
}

// UnmarshalJSON handles both string and object forms of mount specifications.
func (m *Mount) UnmarshalJSON(data []byte) error {
	// Try string first
	var s string
	if err := json.Unmarshal(data, &s); err == nil {
		// Parsed now so metadata merging can key on Target; values with
		// variables are re-parsed after substitution.
		if parsed, err := Parse(s); err == nil {
			*m = parsed
		}
		m.Raw = s
		return nil
	}

	// Try object form
	type mountAlias Mount
	var obj mountAlias
	if err := json.Unmarshal(data, &obj); err != nil {
		return err
	}
	*m = Mount(obj)
	return nil
}

// String returns the mount as a docker-style string.
func (m Mount) String() string {
	if m.Raw != "" {
		return m.Raw
	}
	if m.Type == "" {
		m.Type = "bind"
	}
	result := fmt.Sprintf("type=%s,source=%s,target=%s", m.Type, m.Source, m.Target)
	if m.ReadOnly {
		result += ",readonly"
	}
	return result
}

// Parse parses a mount given as a string. It is the only mount string
// parser in dcx (user, image-metadata and feature mounts all use it).
//
// Two forms are accepted:
//   - Docker --mount syntax: "type=bind,source=/a,target=/b,readonly"
//     (src/dst/destination aliases, readonly or readonly=true, consistency)
//   - Docker -v short syntax: "/host:/container[:ro]"
//
// The form is chosen by the presence of '=', so a value containing ':'
// (e.g. an expanded path) can never be mistaken for the short form.
func Parse(spec string) (Mount, error) {
	spec = strings.TrimSpace(spec)
	if spec == "" {
		return Mount{}, fmt.Errorf("empty mount")
	}
	if strings.Contains(spec, "=") {
		return parseKeyValueMount(spec)
	}
	return parseShortMount(spec)
}

func parseShortMount(spec string) (Mount, error) {
	parts := strings.SplitN(spec, ":", 3)
	if len(parts) < 2 || parts[0] == "" || parts[1] == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: expected source:target[:options]", spec)
	}
	m := Mount{Source: parts[0], Target: parts[1], Type: "bind"}
	if !strings.HasPrefix(m.Source, "/") && !strings.HasPrefix(m.Source, ".") && !strings.HasPrefix(m.Source, "~") {
		m.Type = "volume"
	}
	if len(parts) == 3 {
		for _, opt := range strings.Split(parts[2], ",") {
			switch opt {
			case "ro", "readonly":
				m.ReadOnly = true
			case "cached", "delegated", "consistent":
				m.Consistency = opt
			}
		}
	}
	return m, nil
}

func parseKeyValueMount(spec string) (Mount, error) {
	m := Mount{Type: "bind"}
	for _, part := range strings.Split(spec, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		key, value, hasValue := strings.Cut(part, "=")
		key = strings.TrimSpace(key)
		value = strings.TrimSpace(value)
		switch key {
		case "source", "src":
			m.Source = value
		case "target", "dst", "destination":
			m.Target = value
		case "type":
			m.Type = value
		case "readonly", "ro":
			m.ReadOnly = !hasValue || value == "true" || value == "1"
		case "consistency":
			m.Consistency = value
		default:
			m.Options = append(m.Options, part)
		}
	}
	if m.Target == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: missing target", spec)
	}
	if m.Type != "tmpfs" && m.Source == "" {
		return Mount{}, fmt.Errorf("invalid mount %q: missing source", spec)
	}
	return m, nil
}

// Option returns the value of an unmodelled --mount option ("" when absent).
func (m Mount) Option(key string) string {
	for _, o := range m.Options {
		if k, v, _ := strings.Cut(o, "="); k == key {
			return v
		}
	}
	return ""
}
