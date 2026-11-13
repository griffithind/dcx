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
