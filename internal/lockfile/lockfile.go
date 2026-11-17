// Package lockfile provides types and utilities for parsing, validating,
// and working with devcontainer-lock.json files.
package lockfile

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// Lockfile represents the devcontainer-lock.json structure.
// It pins exact feature versions for reproducible builds.
//
// The lockfile format follows the devcontainer specification:
// https://github.com/devcontainers/spec/blob/main/docs/specs/devcontainer-lockfile.md
// and devcontainers/cli writes it: features are keyed by their reference as
// devcontainer.json (or a feature's dependsOn) writes it.
type Lockfile struct {
	// Features maps feature references to their locked versions.
	Features map[string]LockedFeature `json:"features"`
}

// LockedFeature represents a locked feature entry in the lockfile.
type LockedFeature struct {
	// Version is the full semantic version (e.g., "2.5.5").
	Version string `json:"version"`

	// Resolved is the fully qualified reference with digest.
	// For OCI features: "ghcr.io/devcontainers/features/common-utils@sha256:abc123..."
	// For tarball features: the HTTPS URL
	Resolved string `json:"resolved"`

	// Integrity is the OCI manifest digest (the digest the feature is
	// fetched by), or the SHA256 of a tarball. Format: "sha256:hexstring"
	Integrity string `json:"integrity"`

	// DependsOn lists the feature's dependsOn references, in the order
	// the feature declares them. Empty array is omitted.
	DependsOn []string `json:"dependsOn,omitempty"`
}

// Load loads a lockfile from the configuration directory.
// It returns:
//   - lockfile: the parsed lockfile, or nil if not found
//   - initLockfile: true if an empty file exists (marker for initialization)
//   - error: any parsing or I/O error
//
// Per the spec, an empty file signals "initialize lockfile on build completion".
func Load(configPath string) (*Lockfile, bool, error) {
	lockfilePath := Path(configPath)

	data, err := os.ReadFile(lockfilePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, false, nil // No lockfile exists
		}
		return nil, false, fmt.Errorf("failed to read lockfile: %w", err)
	}

	// Empty file is a marker for initialization
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, true, nil
	}

	var lockfile Lockfile
	if err := json.Unmarshal(data, &lockfile); err != nil {
		return nil, false, fmt.Errorf("failed to parse lockfile: %w", err)
	}

	// Ensure Features map is initialized
	if lockfile.Features == nil {
		lockfile.Features = make(map[string]LockedFeature)
	}

	return &lockfile, false, nil
}

// Save writes the lockfile to disk in devcontainers/cli's form: two-space
// indentation, features ordered by CompareIDs, no HTML escaping, and a
// trailing newline, so both tools write the same bytes.
func (l *Lockfile) Save(configPath string) error {
	data, err := l.marshal()
	if err != nil {
		return fmt.Errorf("failed to marshal lockfile: %w", err)
	}
	if err := os.WriteFile(Path(configPath), data, 0644); err != nil {
		return fmt.Errorf("failed to write lockfile: %w", err)
	}
	return nil
}

func (l *Lockfile) marshal() ([]byte, error) {
	ids := make([]string, 0, len(l.Features))
	for id := range l.Features {
		ids = append(ids, id)
	}
	sort.SliceStable(ids, func(i, j int) bool { return CompareIDs(ids[i], ids[j]) < 0 })

	var buf bytes.Buffer
	buf.WriteString("{\n  \"features\": {")
	for i, id := range ids {
		if i > 0 {
			buf.WriteString(",")
		}
		key, err := encode(id, "")
		if err != nil {
			return nil, err
		}
		value, err := encode(l.Features[id], "    ")
		if err != nil {
			return nil, err
		}
		fmt.Fprintf(&buf, "\n    %s: %s", key, value)
	}
	if len(ids) > 0 {
		buf.WriteString("\n  ")
	}
	buf.WriteString("}\n}\n")
	return buf.Bytes(), nil
}

// encode marshals v as JSON.stringify does (no HTML escaping), indented
// by two spaces under prefix.
func encode(v any, prefix string) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent(prefix, "  ")
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimSuffix(buf.Bytes(), []byte("\n")), nil
}

// GetPath returns the lockfile path based on the config file naming.
// Per the spec:
//   - .devcontainer.json → .devcontainer-lock.json
//   - devcontainer.json → devcontainer-lock.json
func Path(configPath string) string {
	dir := filepath.Dir(configPath)
	base := filepath.Base(configPath)

	var lockfileName string
	if strings.HasPrefix(base, ".") {
		lockfileName = ".devcontainer-lock.json"
	} else {
		lockfileName = "devcontainer-lock.json"
	}

	return filepath.Join(dir, lockfileName)
}

// Get retrieves the locked entry of a feature reference, as written.
func (l *Lockfile) Get(featureID string) (LockedFeature, bool) {
	if l == nil || l.Features == nil {
		return LockedFeature{}, false
	}
	locked, ok := l.Features[featureID]
	return locked, ok
}

// Set adds or updates a locked feature.
func (l *Lockfile) Set(featureID string, locked LockedFeature) {
	if l.Features == nil {
		l.Features = make(map[string]LockedFeature)
	}
	l.Features[featureID] = locked
}

// New creates an empty lockfile.
func New() *Lockfile {
	return &Lockfile{
		Features: make(map[string]LockedFeature),
	}
}

// IsEmpty returns true if the lockfile has no features.
func (l *Lockfile) IsEmpty() bool {
	return l == nil || len(l.Features) == 0
}

// Equals compares two lockfiles for equality.
// Used for detecting if lockfile needs to be updated.
func (l *Lockfile) Equals(other *Lockfile) bool {
	if l == nil && other == nil {
		return true
	}
	if l == nil || other == nil {
		return false
	}
	if len(l.Features) != len(other.Features) {
		return false
	}
	for id, locked := range l.Features {
		otherLocked, ok := other.Features[id]
		if !ok {
			return false
		}
		if locked.Version != otherLocked.Version ||
			locked.Resolved != otherLocked.Resolved ||
			locked.Integrity != otherLocked.Integrity {
			return false
		}
		// Compare DependsOn arrays
		if len(locked.DependsOn) != len(otherLocked.DependsOn) {
			return false
		}
		for i, dep := range locked.DependsOn {
			if dep != otherLocked.DependsOn[i] {
				return false
			}
		}
	}
	return true
}

// collationClasses lists ASCII punctuation and symbols in the order the
// Unicode root collation (JavaScript's localeCompare) sorts them, before
// digits and letters.
const collationClasses = " \t\n\r_-,;:!?.'\"()[]{}@*/\\&#%`^+<=>|~$"

// collationWeight is a character's primary weight: punctuation and symbols
// first (collationClasses), then digits, then letters ignoring case.
func collationWeight(c byte) int {
	switch {
	case c >= '0' && c <= '9':
		return 100 + int(c-'0')
	case c >= 'a' && c <= 'z':
		return 200 + int(c-'a')
	case c >= 'A' && c <= 'Z':
		return 200 + int(c-'A')
	}
	if i := strings.IndexByte(collationClasses, c); i >= 0 {
		return i
	}
	return 300 + int(c)
}

// CompareIDs orders feature references as devcontainers/cli does, with
// JavaScript's localeCompare (the Unicode root collation): punctuation
// before digits before letters, case only breaking ties (lowercase first).
// Exact for ASCII, which feature references are.
func CompareIDs(a, b string) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if wa, wb := collationWeight(a[i]), collationWeight(b[i]); wa != wb {
			return wa - wb
		}
	}
	if len(a) != len(b) {
		return len(a) - len(b)
	}
	// Equal but for case: lowercase sorts first.
	for i := 0; i < len(a); i++ {
		if a[i] != b[i] {
			if a[i] >= 'a' && a[i] <= 'z' {
				return -1
			}
			return 1
		}
	}
	return 0
}
