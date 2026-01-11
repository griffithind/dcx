//go:build e2e

package e2e

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/griffithind/dcx/test/helpers"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRuntimeSecrets_Image tests runtime secrets with image-based devcontainer.
func TestRuntimeSecrets_Image(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"MY_SECRET": "echo secret-value-123"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up the container
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "Mounting secrets")

	// Verify secret is mounted at /run/secrets/MY_SECRET
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/MY_SECRET")
	require.NoError(t, err)
	assert.Equal(t, "secret-value-123", helpers.StripANSI(stdout)[:len("secret-value-123")])
}

// TestRuntimeSecrets_Dockerfile tests runtime secrets with Dockerfile-based devcontainer.
func TestRuntimeSecrets_Dockerfile(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile
	dockerfile := `FROM alpine:latest
RUN echo "built from dockerfile" > /built-marker
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create devcontainer.json with runtime secrets
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"DB_PASSWORD": "echo db-pass-456"
				}
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Bring up
	stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "up")
	assert.Contains(t, stdout, "Mounting secrets")

	// Verify Dockerfile was executed
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/built-marker")
	require.NoError(t, err)
	assert.Contains(t, stdout, "built from dockerfile")

	// Verify secret is mounted
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/run/secrets/DB_PASSWORD")
	require.NoError(t, err)
	assert.Contains(t, stdout, "db-pass-456")
}

// TestRuntimeSecrets_Compose tests runtime secrets with compose-based devcontainer.
func TestRuntimeSecrets_Compose(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"API_KEY": "echo api-key-789"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: alpine:latest
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	stdout := helpers.RunDCXInDirSuccess(t, workspace, "up")
	assert.Contains(t, stdout, "Mounting secrets")

	// Verify secret is mounted
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/API_KEY")
	require.NoError(t, err)
	assert.Contains(t, stdout, "api-key-789")
}

// TestRuntimeSecrets_MultipleSecrets tests multiple runtime secrets.
func TestRuntimeSecrets_MultipleSecrets(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"SECRET_ONE": "echo value-one",
					"SECRET_TWO": "echo value-two",
					"SECRET_THREE": "echo value-three"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify all secrets are mounted
	for _, tc := range []struct {
		name  string
		value string
	}{
		{"SECRET_ONE", "value-one"},
		{"SECRET_TWO", "value-two"},
		{"SECRET_THREE", "value-three"},
	} {
		stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/"+tc.name)
		require.NoError(t, err, "failed to read secret %s", tc.name)
		assert.Contains(t, stdout, tc.value, "secret %s has wrong value", tc.name)
	}
}

// TestRuntimeSecrets_Tmpfs verifies that /run/secrets is mounted as tmpfs (in-memory).
func TestRuntimeSecrets_Tmpfs(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"TMPFS_TEST": "echo tmpfs-test-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify /run/secrets is tmpfs
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "df", "-T", "/run/secrets")
	require.NoError(t, err)
	assert.Contains(t, stdout, "tmpfs", "expected /run/secrets to be tmpfs")
}

// TestRuntimeSecrets_Permissions tests that secrets have correct permissions (0400).
func TestRuntimeSecrets_Permissions(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"PERM_TEST_SECRET": "echo test-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Check permissions using stat
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "stat", "-c", "%a", "/run/secrets/PERM_TEST_SECRET")
	require.NoError(t, err)
	// Should be 400 (read-only by owner)
	assert.Contains(t, stdout, "400")
}

// TestBuildSecrets_Dockerfile tests build secrets with Dockerfile.
func TestBuildSecrets_Dockerfile(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile that uses build secret
	dockerfile := `FROM alpine:latest
# Use BuildKit secret mount to access the secret during build
RUN --mount=type=secret,id=BUILD_TOKEN \
    cat /run/secrets/BUILD_TOKEN > /build-secret-result
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create devcontainer.json with build secrets
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"buildSecrets": {
					"BUILD_TOKEN": "echo build-secret-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Bring up - this should build with secrets
	stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "up")
	assert.Contains(t, stdout, "Fetching build secrets")

	// Verify the secret was available during build
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/build-secret-result")
	require.NoError(t, err)
	assert.Contains(t, stdout, "build-secret-value")
}

// TestBuildSecrets_NotInImage verifies build secrets don't persist in image layers.
func TestBuildSecrets_NotInImage(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile that uses build secret but doesn't persist it
	dockerfile := `FROM alpine:latest
# Access secret during build but don't save it to a file
RUN --mount=type=secret,id=SENSITIVE_TOKEN \
    echo "secret was accessed" > /build-status
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"buildSecrets": {
					"SENSITIVE_TOKEN": "echo super-secret-do-not-persist"
				}
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, tmpDir, "up")

	// Verify build completed
	stdout, _, err := helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/build-status")
	require.NoError(t, err)
	assert.Contains(t, stdout, "secret was accessed")

	// Verify secret is NOT at /run/secrets (it was only available during build)
	_, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "test", "-f", "/run/secrets/SENSITIVE_TOKEN")
	assert.Error(t, err, "secret should not exist in running container")
}

// TestBuildSecrets_Compose tests build secrets with compose-based devcontainer.
func TestBuildSecrets_Compose(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile for compose to build
	dockerfile := `FROM alpine:latest
# Use BuildKit secret mount to access the secret during build
RUN --mount=type=secret,id=COMPOSE_BUILD_SECRET \
    cat /run/secrets/COMPOSE_BUILD_SECRET > /compose-build-secret-result
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	// Create docker-compose.yml that builds from Dockerfile
	dockerComposeYAML := `version: '3.8'
services:
  app:
    build:
      context: .
      dockerfile: Dockerfile
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "docker-compose.yml"), []byte(dockerComposeYAML), 0644)
	require.NoError(t, err)

	// Create devcontainer.json with build secrets
	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"buildSecrets": {
					"COMPOSE_BUILD_SECRET": "echo compose-secret-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Bring up - this should build with secrets
	stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "up")
	assert.Contains(t, stdout, "Fetching build secrets")

	// Verify the secret was available during build
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/compose-build-secret-result")
	require.NoError(t, err)
	assert.Contains(t, stdout, "compose-secret-value")
}

// TestRuntimeSecrets_NonRootUser tests secrets are accessible by non-root remoteUser.
func TestRuntimeSecrets_NonRootUser(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "node:20-slim",
		"remoteUser": "node",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"USER_SECRET": "echo non-root-secret-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify we're running as node user (non-root)
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "whoami")
	require.NoError(t, err)
	assert.Contains(t, stdout, "node")

	// Verify secret is readable by non-root user
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/USER_SECRET")
	require.NoError(t, err)
	assert.Contains(t, stdout, "non-root-secret-value")

	// Verify secret is owned by the remoteUser
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "stat", "-c", "%U", "/run/secrets/USER_SECRET")
	require.NoError(t, err)
	assert.Contains(t, stdout, "node")
}

// TestRuntimeSecrets_RootUser tests secrets work when running as root.
func TestRuntimeSecrets_RootUser(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"image": "alpine:latest",
		"remoteUser": "root",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"ROOT_SECRET": "echo root-secret-value"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	workspace := helpers.CreateTempWorkspace(t, devcontainerJSON)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify we're running as root
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "whoami")
	require.NoError(t, err)
	assert.Contains(t, stdout, "root")

	// Verify secret is readable
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/ROOT_SECRET")
	require.NoError(t, err)
	assert.Contains(t, stdout, "root-secret-value")
}

// TestRuntimeSecrets_ComposeNonRootUser tests secrets with compose and non-root user.
func TestRuntimeSecrets_ComposeNonRootUser(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)
	helpers.RequireComposeAvailable(t)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"dockerComposeFile": "docker-compose.yml",
		"service": "app",
		"remoteUser": "node",
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"COMPOSE_USER_SECRET": "echo compose-user-secret"
				}
			}
		}
	}`, helpers.UniqueTestName(t))

	dockerComposeYAML := `version: '3.8'
services:
  app:
    image: node:20-slim
    command: sleep infinity
    volumes:
      - ..:/workspace:cached
`

	workspace := helpers.CreateTempComposeWorkspace(t, devcontainerJSON, dockerComposeYAML)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, workspace, "down")
	})

	// Bring up
	helpers.RunDCXInDirSuccess(t, workspace, "up")

	// Verify we're running as node user
	stdout, _, err := helpers.RunDCXInDir(t, workspace, "exec", "--", "whoami")
	require.NoError(t, err)
	assert.Contains(t, stdout, "node")

	// Verify secret is readable
	stdout, _, err = helpers.RunDCXInDir(t, workspace, "exec", "--", "cat", "/run/secrets/COMPOSE_USER_SECRET")
	require.NoError(t, err)
	assert.Contains(t, stdout, "compose-user-secret")
}

// TestSecrets_BothRuntimeAndBuild tests using both runtime and build secrets together.
func TestSecrets_BothRuntimeAndBuild(t *testing.T) {
	t.Parallel()
	helpers.RequireDockerAvailable(t)

	tmpDir := t.TempDir()
	devcontainerDir := filepath.Join(tmpDir, ".devcontainer")
	err := os.MkdirAll(devcontainerDir, 0755)
	require.NoError(t, err)

	// Create Dockerfile that uses build secret
	dockerfile := `FROM alpine:latest
RUN --mount=type=secret,id=BUILD_ONLY \
    cat /run/secrets/BUILD_ONLY > /build-time-value
`
	err = os.WriteFile(filepath.Join(devcontainerDir, "Dockerfile"), []byte(dockerfile), 0644)
	require.NoError(t, err)

	devcontainerJSON := fmt.Sprintf(`{
		"name": %q,
		"build": {"dockerfile": "Dockerfile"},
		"workspaceFolder": "/workspace",
		"customizations": {
			"dcx": {
				"secrets": {
					"RUNTIME_SECRET": "echo runtime-value"
				},
				"buildSecrets": {
					"BUILD_ONLY": "echo build-time-only"
				}
			}
		}
	}`, helpers.UniqueTestName(t))
	err = os.WriteFile(filepath.Join(devcontainerDir, "devcontainer.json"), []byte(devcontainerJSON), 0644)
	require.NoError(t, err)

	t.Cleanup(func() {
		helpers.RunDCXInDir(t, tmpDir, "down")
	})

	// Bring up
	stdout := helpers.RunDCXInDirSuccess(t, tmpDir, "up")
	assert.Contains(t, stdout, "Fetching build secrets")
	assert.Contains(t, stdout, "Mounting secrets")

	// Verify build secret was used during build
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/build-time-value")
	require.NoError(t, err)
	assert.Contains(t, stdout, "build-time-only")

	// Verify runtime secret is mounted
	stdout, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "cat", "/run/secrets/RUNTIME_SECRET")
	require.NoError(t, err)
	assert.Contains(t, stdout, "runtime-value")

	// Verify build secret is NOT available at runtime
	_, _, err = helpers.RunDCXInDir(t, tmpDir, "exec", "--", "test", "-f", "/run/secrets/BUILD_ONLY")
	assert.Error(t, err, "build secret should not exist at runtime")
}
