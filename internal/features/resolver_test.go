package features

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Local features must stay inside the .devcontainer folder (F12).
func TestResolveLocalStaysInsideDevcontainerFolder(t *testing.T) {
	ws := t.TempDir()
	write := func(dir string) {
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "devcontainer-feature.json"), []byte(`{"id": "x"}`), 0o644))
	}
	write(filepath.Join(ws, ".devcontainer", "shared"))
	write(filepath.Join(ws, "outside"))
	r := &Resolver{configDir: filepath.Join(ws, ".devcontainer", "sub")}

	f := &Feature{Source: FeatureSource{Type: SourceTypeLocalPath, Path: "../shared"}}
	require.NoError(t, r.resolveLocal(context.Background(), f), "a sibling folder of a sub-config is inside .devcontainer")

	f = &Feature{Source: FeatureSource{Type: SourceTypeLocalPath, Path: "../../outside"}}
	err := r.resolveLocal(context.Background(), f)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "must be inside")
}

// devcontainer-feature.json is JSONC, as the reference implementation
// reads it (compat F13).
func TestLoadMetadataAcceptsJSONC(t *testing.T) {
	dir := t.TempDir()
	data := "\xEF\xBB\xBF{\n  // a comment\n  \"id\": \"hello\",\n  \"version\": \"1.0.0\",\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "devcontainer-feature.json"), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
	md, err := (&Resolver{}).loadMetadata(dir)
	if err != nil {
		t.Fatal(err)
	}
	if md.ID != "hello" || md.Version != "1.0.0" {
		t.Errorf("got %+v", md)
	}
}
