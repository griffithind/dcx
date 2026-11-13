package devcontainer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// An ambiguous config is rejected by resolution (D7), not resolved by an
// internal precedence (compose over build over image).
func TestResolveWorkspaceRejectsAmbiguousConfig(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	config := `{"image": "alpine:3", "dockerComposeFile": "compose.yml", "service": "app"}`
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(config), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws})
	if err == nil || !strings.Contains(err.Error(), "set only one of image, build or dockerComposeFile (found dockerComposeFile, image)") {
		t.Fatalf("want an ambiguity error, got %v", err)
	}
}

// A base image's devcontainer.metadata must not supply dcx secrets (F3):
// their commands run on the host.
type metadataImages string

func (m metadataImages) ImageMetadata(context.Context, string) (string, error) { return string(m), nil }

func TestImageMetadataCannotSupplyDcxSecrets(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(`{"image": "evil:1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	label := `[{"customizations": {"dcx": {"secrets": {"x": "touch /tmp/pwned"}, "buildSecrets": {"y": "id"}}, "vscode": {"extensions": ["a.b"]}}}]`
	resolved, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws, Images: metadataImages(label)})
	if err != nil {
		t.Fatal(err)
	}
	if len(resolved.RuntimeSecrets)+len(resolved.BuildSecrets) != 0 {
		t.Errorf("image metadata supplied host commands: %+v %+v", resolved.RuntimeSecrets, resolved.BuildSecrets)
	}
	if _, ok := resolved.Config.Customizations["dcx"]; ok {
		t.Error("the image's dcx customizations were merged")
	}
	if _, ok := resolved.Config.Customizations["vscode"]; !ok {
		t.Error("other tools' customizations still merge")
	}
}

// A ${localEnv:…} value that differs between logins (an SSH agent socket)
// changes only the inputs hash; editing the file changes the staleness key
// (F4).
func TestStalenessKeyIgnoresSubstitutedValues(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	write := func(cfg string) {
		if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(cfg), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	resolve := func() *ResolvedDevContainer {
		r, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws})
		if err != nil {
			t.Fatal(err)
		}
		return r
	}
	cfg := `{"image": "alpine:3", "mounts": ["source=${localEnv:DCX_TEST_SOCK},target=/ssh-agent,type=bind"]}`
	write(cfg)
	t.Setenv("DCX_TEST_SOCK", "/tmp/ssh-AAA/agent.1")
	first := resolve()
	t.Setenv("DCX_TEST_SOCK", "/tmp/ssh-BBB/agent.2")
	second := resolve()
	if first.ConfigHash != second.ConfigHash {
		t.Error("a new login's socket path made the container stale")
	}
	if first.InputsHash == second.InputsHash {
		t.Error("the inputs hash must see the substituted value")
	}

	write(`{"image": "alpine:3.21", "mounts": ["source=${localEnv:DCX_TEST_SOCK},target=/ssh-agent,type=bind"]}`)
	if resolve().ConfigHash == second.ConfigHash {
		t.Error("editing devcontainer.json must change the staleness key")
	}
}

// Image metadata is substituted like devcontainer.json, and the entrypoints
// recorded in it run (F6): a prebuilt docker-in-docker image's volume name
// used ${devcontainerId} literally and its dockerd init never ran.
func TestImageMetadataIsSubstitutedAndItsEntrypointsRun(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "devcontainer.json"), []byte(`{"image": "prebuilt:1"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	label := `[{"id": "ghcr.io/devcontainers/features/docker-in-docker:2",
		"entrypoint": "/usr/local/share/docker-init.sh",
		"mounts": [{"source": "dind-var-lib-docker-${devcontainerId}", "target": "/var/lib/docker", "type": "volume"}]}]`
	resolved, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws, Images: metadataImages(label)})
	if err != nil {
		t.Fatal(err)
	}
	var source string
	for _, m := range resolved.Mounts {
		if m.Target == "/var/lib/docker" {
			source = m.Source
		}
	}
	if want := "dind-var-lib-docker-" + resolved.WorkspaceID; source != want {
		t.Errorf("volume source = %q, want %q", source, want)
	}
	if len(resolved.Entrypoints) != 1 || resolved.Entrypoints[0] != "/usr/local/share/docker-init.sh" {
		t.Errorf("Entrypoints = %v", resolved.Entrypoints)
	}
}

// imagesByRef serves one label per image reference.
type imagesByRef map[string]string

func (m imagesByRef) ImageMetadata(_ context.Context, ref string) (string, error) { return m[ref], nil }

// The metadata of the image a Dockerfile builds FROM applies (S1): a
// "FROM …/typescript-node" config used to lose remoteUser: node, so
// sessions ran as root and the UID layer was skipped.
func TestDockerfileBaseImageMetadataApplies(t *testing.T) {
	ws := t.TempDir()
	dir := filepath.Join(ws, ".devcontainer")
	writeFile(t, filepath.Join(dir, "Dockerfile"), "ARG VARIANT=1-20\nFROM mcr.microsoft.com/devcontainers/typescript-node:${VARIANT}\n")
	writeFile(t, filepath.Join(dir, "devcontainer.json"), `{"build": {"dockerfile": "Dockerfile", "args": {"VARIANT": "1-22"}}}`)

	images := imagesByRef{"mcr.microsoft.com/devcontainers/typescript-node:1-22": `[{"remoteUser": "node", "postCreateCommand": "echo base"}]`}
	resolved, err := ResolveWorkspace(context.Background(), ResolveOptions{WorkspacePath: ws, Images: images})
	if err != nil {
		t.Fatal(err)
	}
	if resolved.MetadataImage != "mcr.microsoft.com/devcontainers/typescript-node:1-22" {
		t.Errorf("MetadataImage = %q", resolved.MetadataImage)
	}
	if resolved.RemoteUser != "node" || resolved.EffectiveUser != "node" {
		t.Errorf("remoteUser from the base image's metadata was lost: %q", resolved.RemoteUser)
	}
	if len(resolved.Hooks.PostCreate) != 1 || resolved.Hooks.PostCreate[0].Source != HookFromImage {
		t.Errorf("the base image's hooks were lost: %+v", resolved.Hooks.PostCreate)
	}
}
