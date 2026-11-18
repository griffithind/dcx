package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/internal/features"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeConfigHash(t *testing.T) {
	t.Run("image plan produces stable hash", func(t *testing.T) {
		cfg := &DevContainerConfig{Image: "alpine:latest"}

		hash1, err := ComputeConfigHash(cfg, "", composeInputs{}, nil)
		require.NoError(t, err)

		hash2, err := ComputeConfigHash(cfg, "", composeInputs{}, nil)
		require.NoError(t, err)

		assert.NotEmpty(t, hash1)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("different devcontainer.json produces different hash", func(t *testing.T) {
		cfg1 := &DevContainerConfig{Image: "alpine:latest"}

		cfg2 := &DevContainerConfig{Image: "ubuntu:latest"}

		hash1, err := ComputeConfigHash(cfg1, "", composeInputs{}, nil)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg2, "", composeInputs{}, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("Dockerfile change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		df := filepath.Join(dir, "Dockerfile")
		cfg := &DevContainerConfig{}

		require.NoError(t, os.WriteFile(df, []byte("FROM alpine:latest"), 0644))
		hash1, err := ComputeConfigHash(cfg, df, composeInputs{}, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(df, []byte("FROM ubuntu:latest"), 0644))
		hash2, err := ComputeConfigHash(cfg, df, composeInputs{}, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("compose file change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		compose := filepath.Join(dir, "docker-compose.yml")
		cfg := &DevContainerConfig{}

		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    image: alpine\n"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", composeInputs{Files: []string{compose}}, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    image: ubuntu\n"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", composeInputs{Files: []string{compose}}, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("compose service Dockerfile change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		appDir := filepath.Join(dir, "app")
		require.NoError(t, os.MkdirAll(appDir, 0755))

		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    build: ./app\n"), 0644))
		in := composeInputs{Files: []string{compose}, Dockerfiles: []string{filepath.Join(appDir, "Dockerfile")}}

		cfg := &DevContainerConfig{}

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte("FROM node:18"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte("FROM node:20"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("compose Dockerfile change with object-form build", func(t *testing.T) {
		dir := t.TempDir()
		appDir := filepath.Join(dir, "app")
		require.NoError(t, os.MkdirAll(appDir, 0755))

		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(
			"services:\n  app:\n    build:\n      context: ./app\n      dockerfile: Dockerfile.dev\n",
		), 0644))
		in := composeInputs{Files: []string{compose}, Dockerfiles: []string{filepath.Join(appDir, "Dockerfile.dev")}}

		cfg := &DevContainerConfig{}

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile.dev"), []byte("FROM node:18"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile.dev"), []byte("FROM node:20"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("multiple compose services with Dockerfiles", func(t *testing.T) {
		dir := t.TempDir()
		appDir := filepath.Join(dir, "app")
		workerDir := filepath.Join(dir, "worker")
		require.NoError(t, os.MkdirAll(appDir, 0755))
		require.NoError(t, os.MkdirAll(workerDir, 0755))

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte("FROM node:18"), 0644))
		require.NoError(t, os.WriteFile(filepath.Join(workerDir, "Dockerfile"), []byte("FROM python:3.12"), 0644))

		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(
			"services:\n  app:\n    build: ./app\n  worker:\n    build: ./worker\n  db:\n    image: postgres:16\n",
		), 0644))
		in := composeInputs{Files: []string{compose}, Dockerfiles: []string{filepath.Join(appDir, "Dockerfile"), filepath.Join(workerDir, "Dockerfile")}}

		cfg := &DevContainerConfig{}

		hash1, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		// Change only the worker Dockerfile
		require.NoError(t, os.WriteFile(filepath.Join(workerDir, "Dockerfile"), []byte("FROM python:3.13"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2, "changing any service's Dockerfile should change the hash")
	})

	t.Run("feature options are part of the file", func(t *testing.T) {
		cfg1 := &DevContainerConfig{Image: "alpine:latest", Features: map[string]interface{}{"feat1": map[string]interface{}{"version": "1.0"}}}
		cfg2 := &DevContainerConfig{Image: "alpine:latest", Features: map[string]interface{}{"feat1": map[string]interface{}{"version": "2.0"}}}
		hash1, err := ComputeConfigHash(cfg1, "", composeInputs{}, nil)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg2, "", composeInputs{}, nil)
		require.NoError(t, err)
		assert.NotEqual(t, hash1, hash2)
	})

	// A tag resolving to a new release changes what would be built, but no
	// file changed: only the inputs hash moves (F4), so dcx reports drift
	// instead of recreating.
	t.Run("a moved feature tag is drift, not staleness", func(t *testing.T) {
		cfg := &DevContainerConfig{Image: "alpine:latest"}
		old := []*features.Feature{{ID: "ghcr.io/x/node:1", ManifestDigest: "sha256:old", Metadata: &features.FeatureMetadata{Version: "1.1.0"}}}
		moved := []*features.Feature{{ID: "ghcr.io/x/node:1", ManifestDigest: "sha256:new", Metadata: &features.FeatureMetadata{Version: "1.2.0"}}}

		c1, err := ComputeConfigHash(cfg, "", composeInputs{}, old)
		require.NoError(t, err)
		c2, err := ComputeConfigHash(cfg, "", composeInputs{}, moved)
		require.NoError(t, err)
		assert.Equal(t, c1, c2, "the staleness key ignores feature digests")

		i1, err := ComputeInputsHash(cfg, "", composeInputs{}, old)
		require.NoError(t, err)
		i2, err := ComputeInputsHash(cfg, "", composeInputs{}, moved)
		require.NoError(t, err)
		assert.NotEqual(t, i1, i2, "the inputs hash sees them")
	})

	t.Run("feature order does not affect hash", func(t *testing.T) {
		cfg := &DevContainerConfig{Image: "alpine:latest"}

		feats1 := []*features.Feature{
			{ID: "feat-a", Metadata: &features.FeatureMetadata{Version: "1.0"}},
			{ID: "feat-b", Metadata: &features.FeatureMetadata{Version: "2.0"}},
		}
		feats2 := []*features.Feature{
			{ID: "feat-b", Metadata: &features.FeatureMetadata{Version: "2.0"}},
			{ID: "feat-a", Metadata: &features.FeatureMetadata{Version: "1.0"}},
		}

		hash1, err := ComputeConfigHash(cfg, "", composeInputs{}, feats1)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg, "", composeInputs{}, feats2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("missing compose Dockerfiles are skipped gracefully", func(t *testing.T) {
		dir := t.TempDir()
		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(
			"services:\n  app:\n    build: ./nonexistent\n",
		), 0644))
		in := composeInputs{Files: []string{compose}, Dockerfiles: []string{filepath.Join(dir, "nonexistent", "Dockerfile")}}

		cfg := &DevContainerConfig{}

		hash, err := ComputeConfigHash(cfg, "", in, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

// Edits that don't affect the container (comments, formatting, editor
// customizations, start/attach hooks) must not mark it stale; edits that do
// must.
func TestConfigHashIsSemantic(t *testing.T) {
	hash := func(t *testing.T, jsonc string) string {
		t.Helper()
		cfg, err := Parse([]byte(jsonc))
		require.NoError(t, err)
		h, err := ComputeConfigHash(cfg, "", composeInputs{}, nil)
		require.NoError(t, err)
		return h
	}
	base := hash(t, `{"image": "alpine:3.20", "containerEnv": {"A": "1"}}`)

	same := []string{
		"{\n  // a comment\n  \"containerEnv\": {\"A\": \"1\"},\n  \"image\": \"alpine:3.20\",\n}",
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "name": "renamed"}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "customizations": {"vscode": {"extensions": ["x"]}}}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "postStartCommand": "echo hi", "postAttachCommand": "echo hi"}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "remoteEnv": {"B": "2"}, "waitFor": "postStartCommand"}`,
	}
	for _, s := range same {
		assert.Equal(t, base, hash(t, s), "should not be stale: %s", s)
	}

	different := []string{
		`{"image": "alpine:3.21", "containerEnv": {"A": "1"}}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "2"}}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "mounts": ["source=v,target=/v,type=volume"]}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "runArgs": ["--memory=1g"]}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "postCreateCommand": "make setup"}`,
		`{"image": "alpine:3.20", "containerEnv": {"A": "1"}, "forwardPorts": [3000]}`,
	}
	for _, s := range different {
		assert.NotEqual(t, base, hash(t, s), "should be stale: %s", s)
	}
}

// Editing a local feature's files must change the key (previously only
// ID, version and options were hashed, so it never rebuilt).
func TestFeaturesKeyCoversLocalFeatureContent(t *testing.T) {
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "install.sh"), []byte("echo v1"), 0o755))
	feat := &features.Feature{
		ID:        "./my-feature",
		Source:    features.FeatureSource{Type: features.SourceTypeLocalPath},
		CachePath: dir,
		Metadata:  &features.FeatureMetadata{ID: "my-feature", Version: "1.0.0"},
	}
	k1 := FeaturesKey([]*features.Feature{feat})
	assert.Equal(t, k1, FeaturesKey([]*features.Feature{feat}), "stable")

	require.NoError(t, os.WriteFile(filepath.Join(dir, "install.sh"), []byte("echo v2"), 0o755))
	assert.NotEqual(t, k1, FeaturesKey([]*features.Feature{feat}))
}

// The key must not depend on the absolute workspace location of compose
// files (the same project checked out elsewhere has the same content).
func TestConfigHashComposeIndependentOfLocation(t *testing.T) {
	write := func(dir string) string {
		p := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(p, []byte("services:\n  app:\n    image: alpine\n"), 0o644))
		return p
	}
	cfg := &DevContainerConfig{DockerComposeFile: StringOrSlice{"docker-compose.yml"}, Service: "app"}
	h1, err := ComputeConfigHash(cfg, "", composeInputs{Files: []string{write(t.TempDir())}}, nil)
	require.NoError(t, err)
	h2, err := ComputeConfigHash(cfg, "", composeInputs{Files: []string{write(t.TempDir())}}, nil)
	require.NoError(t, err)
	assert.Equal(t, h1, h2)
}
