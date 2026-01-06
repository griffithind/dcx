package compose

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseComposeFile(t *testing.T) {
	// Create temp compose file
	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")

	content := `
version: '3.8'
services:
  app:
    image: node:18
    volumes:
      - .:/workspace
  db:
    image: postgres:14
    environment:
      POSTGRES_PASSWORD: secret
`
	err := os.WriteFile(composePath, []byte(content), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(composePath)
	require.NoError(t, err)

	assert.Len(t, compose.Services, 2)
	assert.Equal(t, "node:18", compose.Services["app"].Image)
	assert.Equal(t, "postgres:14", compose.Services["db"].Image)
}

func TestGetServiceBaseImage(t *testing.T) {
	compose := &ComposeFile{
		Services: map[string]ServiceConfig{
			"app": {Image: "node:18"},
			"builder": {Build: &ServiceBuild{Context: "."}},
		},
	}

	// Service with image
	image, err := compose.GetServiceBaseImage("app")
	require.NoError(t, err)
	assert.Equal(t, "node:18", image)

	// Service with build (returns empty)
	image, err = compose.GetServiceBaseImage("builder")
	require.NoError(t, err)
	assert.Equal(t, "", image)

	// Non-existent service
	_, err = compose.GetServiceBaseImage("nonexistent")
	assert.Error(t, err)
}

func TestHasBuild(t *testing.T) {
	compose := &ComposeFile{
		Services: map[string]ServiceConfig{
			"app":     {Image: "node:18"},
			"builder": {Build: &ServiceBuild{Context: "."}},
		},
	}

	assert.False(t, compose.HasBuild("app"))
	assert.True(t, compose.HasBuild("builder"))
	assert.False(t, compose.HasBuild("nonexistent"))
}

func TestServiceBuildUnmarshal(t *testing.T) {
	tests := []struct {
		name     string
		yaml     string
		expected ServiceBuild
	}{
		{
			name: "string form",
			yaml: `
services:
  app:
    build: ./context
`,
			expected: ServiceBuild{Context: "./context"},
		},
		{
			name: "struct form",
			yaml: `
services:
  app:
    build:
      context: ./mycontext
      dockerfile: Dockerfile.dev
`,
			expected: ServiceBuild{Context: "./mycontext", Dockerfile: "Dockerfile.dev"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			path := filepath.Join(dir, "docker-compose.yml")
			err := os.WriteFile(path, []byte(tt.yaml), 0644)
			require.NoError(t, err)

			compose, err := ParseComposeFile(path)
			require.NoError(t, err)

			svc := compose.Services["app"]
			require.NotNil(t, svc.Build)
			assert.Equal(t, tt.expected.Context, svc.Build.Context)
			assert.Equal(t, tt.expected.Dockerfile, svc.Build.Dockerfile)
		})
	}
}

// TestOuzoERPStyleCompose tests parsing of a full ouzoerp-style compose.yaml
// with multiple services, builds, volumes, environment, depends_on, etc.
func TestOuzoERPStyleCompose(t *testing.T) {
	composeYAML := `
name: ouzoerp

services:
  app:
    build:
      context: ..
      dockerfile: .devcontainer/Dockerfile-app
    env_file: devcontainer.env
    volumes:
      - app_bkp:/home/user/bkp
      - app_bundle:/usr/local/bundle
      - ../..:/workspaces:cached
    environment:
      COREPACK_ENABLE_DOWNLOAD_PROMPT: 0
    entrypoint: ["/bin/bash", "-c", "trap 'exit 0' SIGINT SIGTERM; while :; do sleep 1; done"]
    ports:
     - 3000:3000
    depends_on:
      - valkey
      - db

  db:
    build:
      context: ..
      dockerfile: .devcontainer/Dockerfile-db
    restart: unless-stopped
    ports:
      - 5432:5432
    volumes:
      - postgresql_data:/var/lib/postgresql/data
    environment:
      POSTGRES_USER: postgres
      POSTGRES_DB: postgres
      POSTGRES_PASSWORD: ouzo

  valkey:
    image: valkey/valkey:8-bookworm
    restart: unless-stopped
    volumes:
      - valkey_data:/data

volumes:
  app_bkp:
    name: ouzoerp_user_bkp
  app_bundle:
    name: ouzoerp_user_bundle
  postgresql_data:
    name: ouzoerp_user_pgdata
  valkey_data:
    name: ouzoerp_user_valkey
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(composeYAML), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	// Check services count
	assert.Len(t, compose.Services, 3)

	// Check app service
	app := compose.Services["app"]
	assert.NotNil(t, app.Build)
	assert.Equal(t, "..", app.Build.Context)
	assert.Equal(t, ".devcontainer/Dockerfile-app", app.Build.Dockerfile)
	assert.Len(t, app.Volumes, 3)
	assert.Contains(t, app.DependsOn, "valkey")
	assert.Contains(t, app.DependsOn, "db")

	// Check db service
	db := compose.Services["db"]
	assert.NotNil(t, db.Build)
	assert.Equal(t, ".devcontainer/Dockerfile-db", db.Build.Dockerfile)

	// Check valkey service (image-based)
	valkey := compose.Services["valkey"]
	assert.Equal(t, "valkey/valkey:8-bookworm", valkey.Image)
	assert.Nil(t, valkey.Build)
}

// TestComposeServiceEnvironment tests parsing of environment variables in compose files.
// Note: Only map form is supported (which is what ouzoerp uses).
func TestComposeServiceEnvironment(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    environment:
      FOO: bar
      BAZ: qux
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	app := compose.Services["app"]
	assert.Equal(t, "bar", app.Environment["FOO"])
	assert.Equal(t, "qux", app.Environment["BAZ"])
}

// TestComposeServiceVolumes tests parsing of various volume formats.
func TestComposeServiceVolumes(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    volumes:
      - named_vol:/data
      - ./local:/app
      - /absolute:/mount
      - cached_vol:/cache:cached
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	app := compose.Services["app"]
	assert.Len(t, app.Volumes, 4)
	assert.Contains(t, app.Volumes, "named_vol:/data")
	assert.Contains(t, app.Volumes, "./local:/app")
	assert.Contains(t, app.Volumes, "/absolute:/mount")
	assert.Contains(t, app.Volumes, "cached_vol:/cache:cached")
}

// TestComposeServiceDependsOn tests parsing of depends_on configuration.
// Note: Only list form is supported (which is what ouzoerp uses).
func TestComposeServiceDependsOn(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    depends_on:
      - db
      - redis
  db:
    image: postgres
  redis:
    image: redis
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	app := compose.Services["app"]
	assert.Contains(t, app.DependsOn, "db")
	assert.Contains(t, app.DependsOn, "redis")
}

// TestComposeServicePorts tests parsing of port configurations.
func TestComposeServicePorts(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    ports:
      - 3000:3000
      - "5432:5432"
      - 8080
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	app := compose.Services["app"]
	assert.GreaterOrEqual(t, len(app.Ports), 2) // At least the explicitly mapped ports
}

// TestComposeWithVariableSubstitution tests that compose files with variables are parsed correctly.
func TestComposeWithVariableSubstitution(t *testing.T) {
	yaml := `
services:
  app:
    image: alpine
    volumes:
      - app_data:/home/${USER}/data
    environment:
      HOME_DIR: /home/${USER}

volumes:
  app_data:
    name: myapp_${USER}_data
`
	dir := t.TempDir()
	path := filepath.Join(dir, "compose.yaml")
	err := os.WriteFile(path, []byte(yaml), 0644)
	require.NoError(t, err)

	// Note: ParseComposeFile doesn't substitute variables, it preserves them
	compose, err := ParseComposeFile(path)
	require.NoError(t, err)

	// Variables should be preserved as-is
	app := compose.Services["app"]
	assert.Contains(t, app.Volumes[0], "${USER}")
}
