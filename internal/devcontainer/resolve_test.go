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
