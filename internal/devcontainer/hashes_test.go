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
		cfg.SetRawJSON([]byte(`{"image":"alpine:latest"}`))

		hash1, err := ComputeConfigHash(cfg, "", nil, nil)
		require.NoError(t, err)

		hash2, err := ComputeConfigHash(cfg, "", nil, nil)
		require.NoError(t, err)

		assert.NotEmpty(t, hash1)
		assert.Equal(t, hash1, hash2)
	})

	t.Run("different devcontainer.json produces different hash", func(t *testing.T) {
		cfg1 := &DevContainerConfig{Image: "alpine:latest"}
		cfg1.SetRawJSON([]byte(`{"image":"alpine:latest"}`))

		cfg2 := &DevContainerConfig{Image: "ubuntu:latest"}
		cfg2.SetRawJSON([]byte(`{"image":"ubuntu:latest"}`))

		hash1, err := ComputeConfigHash(cfg1, "", nil, nil)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg2, "", nil, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("Dockerfile change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		df := filepath.Join(dir, "Dockerfile")
		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{"build":{"dockerfile":"Dockerfile"}}`))

		require.NoError(t, os.WriteFile(df, []byte("FROM alpine:latest"), 0644))
		hash1, err := ComputeConfigHash(cfg, df, nil, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(df, []byte("FROM ubuntu:latest"), 0644))
		hash2, err := ComputeConfigHash(cfg, df, nil, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("compose file change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		compose := filepath.Join(dir, "docker-compose.yml")
		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{"dockerComposeFile":"docker-compose.yml"}`))

		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    image: alpine\n"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    image: ubuntu\n"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("compose service Dockerfile change produces different hash", func(t *testing.T) {
		dir := t.TempDir()
		appDir := filepath.Join(dir, "app")
		require.NoError(t, os.MkdirAll(appDir, 0755))

		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte("services:\n  app:\n    build: ./app\n"), 0644))

		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{"dockerComposeFile":"docker-compose.yml","service":"app"}`))

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte("FROM node:18"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile"), []byte("FROM node:20"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
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

		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{}`))

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile.dev"), []byte("FROM node:18"), 0644))
		hash1, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		require.NoError(t, os.WriteFile(filepath.Join(appDir, "Dockerfile.dev"), []byte("FROM node:20"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
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

		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{}`))

		hash1, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		// Change only the worker Dockerfile
		require.NoError(t, os.WriteFile(filepath.Join(workerDir, "Dockerfile"), []byte("FROM python:3.13"), 0644))
		hash2, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2, "changing any service's Dockerfile should change the hash")
	})

	t.Run("feature change produces different hash", func(t *testing.T) {
		cfg := &DevContainerConfig{Image: "alpine:latest"}
		cfg.SetRawJSON([]byte(`{"image":"alpine:latest"}`))

		feats1 := []*features.Feature{
			{ID: "feat1", Options: map[string]interface{}{"version": "1.0"}},
		}
		feats2 := []*features.Feature{
			{ID: "feat1", Options: map[string]interface{}{"version": "2.0"}},
		}

		hash1, err := ComputeConfigHash(cfg, "", nil, feats1)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg, "", nil, feats2)
		require.NoError(t, err)

		assert.NotEqual(t, hash1, hash2)
	})

	t.Run("feature order does not affect hash", func(t *testing.T) {
		cfg := &DevContainerConfig{Image: "alpine:latest"}
		cfg.SetRawJSON([]byte(`{"image":"alpine:latest"}`))

		feats1 := []*features.Feature{
			{ID: "feat-a", Metadata: &features.FeatureMetadata{Version: "1.0"}},
			{ID: "feat-b", Metadata: &features.FeatureMetadata{Version: "2.0"}},
		}
		feats2 := []*features.Feature{
			{ID: "feat-b", Metadata: &features.FeatureMetadata{Version: "2.0"}},
			{ID: "feat-a", Metadata: &features.FeatureMetadata{Version: "1.0"}},
		}

		hash1, err := ComputeConfigHash(cfg, "", nil, feats1)
		require.NoError(t, err)
		hash2, err := ComputeConfigHash(cfg, "", nil, feats2)
		require.NoError(t, err)

		assert.Equal(t, hash1, hash2)
	})

	t.Run("missing compose Dockerfiles are skipped gracefully", func(t *testing.T) {
		dir := t.TempDir()
		compose := filepath.Join(dir, "docker-compose.yml")
		require.NoError(t, os.WriteFile(compose, []byte(
			"services:\n  app:\n    build: ./nonexistent\n",
		), 0644))

		cfg := &DevContainerConfig{}
		cfg.SetRawJSON([]byte(`{}`))

		hash, err := ComputeConfigHash(cfg, "", []string{compose}, nil)
		require.NoError(t, err)
		assert.NotEmpty(t, hash)
	})
}

func TestParseComposeDockerfilePaths(t *testing.T) {
	t.Run("string-form build directive", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build: ./app\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/project/app/Dockerfile", paths[0])
	})

	t.Run("object-form build directive with explicit dockerfile", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build:\n      context: ./src\n      dockerfile: Dockerfile.prod\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/project/src/Dockerfile.prod", paths[0])
	})

	t.Run("object-form without dockerfile defaults to Dockerfile", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build:\n      context: ./src\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/project/src/Dockerfile", paths[0])
	})

	t.Run("multiple services with builds", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build: ./app\n  worker:\n    build:\n      context: ./worker\n      dockerfile: Dockerfile.worker\n  db:\n    image: postgres\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 2)
		assert.Contains(t, paths, "/project/app/Dockerfile")
		assert.Contains(t, paths, "/project/worker/Dockerfile.worker")
	})

	t.Run("image-only service produces no paths", func(t *testing.T) {
		content := []byte("services:\n  db:\n    image: postgres:16\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		assert.Empty(t, paths)
	})

	t.Run("invalid YAML produces no paths", func(t *testing.T) {
		content := []byte("not: valid: yaml: [[[")
		paths := parseComposeDockerfilePaths(content, "/project")

		assert.Empty(t, paths)
	})

	t.Run("absolute context path is preserved", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build:\n      context: /absolute/path\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/absolute/path/Dockerfile", paths[0])
	})

	t.Run("absolute dockerfile path is preserved", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build:\n      context: ./app\n      dockerfile: /absolute/Dockerfile\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/absolute/Dockerfile", paths[0])
	})

	t.Run("build directive with only dockerfile and no context", func(t *testing.T) {
		content := []byte("services:\n  app:\n    build:\n      dockerfile: Dockerfile.custom\n")
		paths := parseComposeDockerfilePaths(content, "/project")

		require.Len(t, paths, 1)
		assert.Equal(t, "/project/Dockerfile.custom", paths[0])
	})
}

func TestCollectComposeDockerfiles(t *testing.T) {
	t.Run("deduplicates paths across compose files", func(t *testing.T) {
		dir := t.TempDir()

		compose1 := filepath.Join(dir, "docker-compose.yml")
		compose2 := filepath.Join(dir, "docker-compose.override.yml")

		require.NoError(t, os.WriteFile(compose1, []byte("services:\n  app:\n    build: ./app\n"), 0644))
		require.NoError(t, os.WriteFile(compose2, []byte("services:\n  app:\n    build: ./app\n"), 0644))

		paths := collectComposeDockerfiles([]string{compose1, compose2})

		assert.Len(t, paths, 1)
	})

	t.Run("collects from multiple compose files", func(t *testing.T) {
		dir := t.TempDir()

		compose1 := filepath.Join(dir, "docker-compose.yml")
		compose2 := filepath.Join(dir, "docker-compose.override.yml")

		require.NoError(t, os.WriteFile(compose1, []byte("services:\n  app:\n    build: ./app\n"), 0644))
		require.NoError(t, os.WriteFile(compose2, []byte("services:\n  worker:\n    build: ./worker\n"), 0644))

		paths := collectComposeDockerfiles([]string{compose1, compose2})

		assert.Len(t, paths, 2)
	})

	t.Run("skips unreadable compose files", func(t *testing.T) {
		paths := collectComposeDockerfiles([]string{"/nonexistent/compose.yml"})
		assert.Empty(t, paths)
	})
}
