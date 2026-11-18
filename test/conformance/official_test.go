package conformance

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/features"
	"github.com/griffithind/dcx/internal/lifecyclecmd"
	"github.com/griffithind/dcx/internal/mount"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The cases in testdata/official are workspaces together with what the
// official devcontainers/cli made of them (record-official.sh records it;
// it is never run by the tests). TestOfficialCLI resolves each workspace
// with ResolveWorkspace, offline, from the same inputs (the image metadata,
// compose model and registry features the official CLI read) and compares:
// the feature install order, the lockfile, and the merged configuration.
// Identity (${devcontainerId}, names) is dcx's own by design and is not
// compared.

// officialRecord is official.json.
type officialRecord struct {
	CLI          string `json:"cli"`
	FeatureOrder []struct {
		ID      string         `json:"id"`
		Options map[string]any `json:"options"`
		Version string         `json:"version"`
	} `json:"featureOrder"`
	Workspace struct {
		WorkspaceFolder string `json:"workspaceFolder"`
		WorkspaceMount  string `json:"workspaceMount"`
	} `json:"workspace"`
	Merged mergedConfiguration `json:"mergedConfiguration"`
}

// mergedConfiguration is the part of read-configuration's merged
// configuration dcx resolves.
type mergedConfiguration struct {
	RemoteUser     string                     `json:"remoteUser"`
	ContainerUser  string                     `json:"containerUser"`
	Init           bool                       `json:"init"`
	Privileged     bool                       `json:"privileged"`
	CapAdd         []string                   `json:"capAdd"`
	SecurityOpt    []string                   `json:"securityOpt"`
	Entrypoints    []string                   `json:"entrypoints"`
	ContainerEnv   map[string]string          `json:"containerEnv"`
	RemoteEnv      map[string]string          `json:"remoteEnv"`
	Mounts         []mount.Mount              `json:"mounts"`
	OnCreate       []*lifecyclecmd.Command    `json:"onCreateCommands"`
	UpdateContent  []*lifecyclecmd.Command    `json:"updateContentCommands"`
	PostCreate     []*lifecyclecmd.Command    `json:"postCreateCommands"`
	PostStart      []*lifecyclecmd.Command    `json:"postStartCommands"`
	PostAttach     []*lifecyclecmd.Command    `json:"postAttachCommands"`
	PortAttributes map[string]json.RawMessage `json:"portsAttributes"`
}

func TestOfficialCLI(t *testing.T) {
	cases, err := os.ReadDir(filepath.Join("testdata", "official"))
	require.NoError(t, err)
	for _, c := range cases {
		t.Run(c.Name(), func(t *testing.T) {
			testOfficialCase(t, filepath.Join("testdata", "official", c.Name()))
		})
	}
}

func testOfficialCase(t *testing.T, dir string) {
	var want officialRecord
	readJSON(t, filepath.Join(dir, "official.json"), &want)

	// A copy outside dcx's git repository, named like the case (the
	// workspace folder is named after it).
	ws := filepath.Join(t.TempDir(), filepath.Base(dir))
	require.NoError(t, os.CopyFS(ws, os.DirFS(dir)))
	seedFeatures(t, filepath.Join(dir, "features.json"))

	opts := devcontainer.ResolveOptions{
		WorkspacePath: ws,
		Images:        recordedImages(t, filepath.Join(dir, "image-metadata.json")),
		Lockfile:      devcontainer.LockfileUse,
	}
	if data, err := os.ReadFile(filepath.Join(dir, "compose-config.json")); err == nil {
		opts.Compose = composeModel(strings.ReplaceAll(string(data), "<workspace>", ws))
	}
	r, err := devcontainer.ResolveWorkspace(context.Background(), opts)
	require.NoError(t, err)

	t.Run("feature install order", func(t *testing.T) {
		var wantOrder, got []string
		for _, f := range want.FeatureOrder {
			wantOrder = append(wantOrder, featureLine(f.ID, f.Options, f.Version))
		}
		for _, f := range r.Features {
			got = append(got, featureLine(f.ID, f.Options, f.Metadata.Version))
		}
		assert.Equal(t, wantOrder, got)
	})

	t.Run("lockfile", func(t *testing.T) {
		recorded := filepath.Join(dir, ".devcontainer", "devcontainer-lock.json")
		wantLock, err := os.ReadFile(recorded)
		if os.IsNotExist(err) {
			t.Skip("no registry features")
		}
		require.NoError(t, err)
		out := filepath.Join(t.TempDir(), "devcontainer.json")
		require.NoError(t, features.GenerateLockfile(r.Features).Save(out))
		got, err := os.ReadFile(filepath.Join(filepath.Dir(out), "devcontainer-lock.json"))
		require.NoError(t, err)
		assert.Equal(t, string(wantLock), string(got))
	})

	t.Run("workspace", func(t *testing.T) {
		assert.Equal(t, want.Workspace.WorkspaceFolder, r.WorkspaceFolder)
		if want.Workspace.WorkspaceMount != "" {
			m, err := mount.Parse(strings.ReplaceAll(want.Workspace.WorkspaceMount, "<workspace>", ws))
			require.NoError(t, err)
			require.NotNil(t, r.WorkspaceMount)
			assert.Equal(t, mountLine(m, ""), mountLine(*r.WorkspaceMount, ""))
		}
	})

	t.Run("merged configuration", func(t *testing.T) {
		m := want.Merged
		assert.Equal(t, m.RemoteUser, r.RemoteUser, "remoteUser")
		assert.Equal(t, m.ContainerUser, r.ContainerUser, "containerUser")
		assert.Equal(t, m.Init, r.Init, "init")
		assert.Equal(t, m.Privileged, r.Privileged, "privileged")
		assert.ElementsMatch(t, m.CapAdd, r.CapAdd, "capAdd")
		assert.ElementsMatch(t, m.SecurityOpt, r.SecurityOpt, "securityOpt")
		assert.Equal(t, m.Entrypoints, emptyAsNil(r.Entrypoints), "entrypoints")
		assert.Equal(t, emptyMapAsNil(m.ContainerEnv), emptyMapAsNil(r.ContainerEnv), "containerEnv")
		assert.Equal(t, emptyMapAsNil(m.RemoteEnv), emptyMapAsNil(r.RemoteEnv), "remoteEnv")

		// The official CLI substitutes ${devcontainerId} later; dcx's
		// value is its own ID.
		var wantMounts, gotMounts []string
		for _, mt := range m.Mounts {
			wantMounts = append(wantMounts, mountLine(mt, ""))
		}
		for _, mt := range r.Mounts {
			gotMounts = append(gotMounts, mountLine(mt, r.WorkspaceID))
		}
		assert.Equal(t, wantMounts, gotMounts, "mounts")

		for _, phase := range []struct {
			name string
			want []*lifecyclecmd.Command
			got  []devcontainer.Hook
		}{
			{"onCreateCommands", m.OnCreate, r.Hooks.OnCreate},
			{"updateContentCommands", m.UpdateContent, r.Hooks.UpdateContent},
			{"postCreateCommands", m.PostCreate, r.Hooks.PostCreate},
			{"postStartCommands", m.PostStart, r.Hooks.PostStart},
			{"postAttachCommands", m.PostAttach, r.Hooks.PostAttach},
		} {
			var wantCmds, gotCmds []string
			for _, c := range phase.want {
				wantCmds = append(wantCmds, commandJSON(t, c))
			}
			for _, h := range phase.got {
				gotCmds = append(gotCmds, commandJSON(t, h.Command))
			}
			assert.Equal(t, wantCmds, gotCmds, phase.name)
		}
	})
}

// featureLine is a feature as its reference, options and version.
func featureLine(id string, options map[string]any, version string) string {
	if options == nil {
		options = map[string]any{}
	}
	b, _ := json.Marshal(options)
	return fmt.Sprintf("%s %s %s", id, b, version)
}

func mountLine(m mount.Mount, workspaceID string) string {
	source := m.Source
	if workspaceID != "" {
		source = strings.ReplaceAll(source, workspaceID, "${devcontainerId}")
	}
	return fmt.Sprintf("type=%s,source=%s,target=%s,readonly=%t", m.Type, source, m.Target, m.ReadOnly)
}

func commandJSON(t *testing.T, c *lifecyclecmd.Command) string {
	t.Helper()
	b, err := json.Marshal(c)
	require.NoError(t, err)
	return string(b)
}

func emptyAsNil(s []string) []string {
	if len(s) == 0 {
		return nil
	}
	return s
}

func emptyMapAsNil(m map[string]string) map[string]string {
	if len(m) == 0 {
		return nil
	}
	return m
}

func readJSON(t *testing.T, path string, v any) {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	require.NoError(t, json.Unmarshal(data, v), path)
}

// recordedImages serves image-metadata.json: the devcontainer.metadata
// label of each image the official CLI inspected.
type recordedImageSource map[string]json.RawMessage

func recordedImages(t *testing.T, path string) recordedImageSource {
	t.Helper()
	images := recordedImageSource{}
	if _, err := os.Stat(path); err == nil {
		readJSON(t, path, &images)
	}
	return images
}

func (r recordedImageSource) ImageMetadata(_ context.Context, ref string) (string, error) {
	label, ok := r[ref]
	if !ok {
		return "", fmt.Errorf("no recorded metadata for image %s (run test/conformance/record-official.sh)", ref)
	}
	if string(label) == "null" {
		return "", nil
	}
	return string(label), nil
}

// seedFeatures stores the recorded registry features (features.json:
// devcontainer-feature.json by manifest digest) in a new feature cache, so
// resolution pinned by the recorded lockfile never reaches a registry, and
// sends anything that would to a closed port.
func seedFeatures(t *testing.T, path string) {
	t.Helper()
	cache := t.TempDir()
	t.Setenv("XDG_CACHE_HOME", cache)
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	var recorded map[string]json.RawMessage
	if _, err := os.Stat(path); err != nil {
		return
	}
	readJSON(t, path, &recorded)
	for digest, metadata := range recorded {
		hex, ok := strings.CutPrefix(digest, "sha256:")
		require.True(t, ok, digest)
		dir := filepath.Join(cache, "dcx", "features", "sha256", hex)
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer-feature.json"), metadata, 0o644))
		info, err := json.Marshal(features.DigestInfo{ManifestDigest: digest})
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(filepath.Join(dir, ".dcx-integrity"), info, 0o644))
	}
}
