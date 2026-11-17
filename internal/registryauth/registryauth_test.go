package registryauth

import (
	"context"
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// dockerConfigDir points DOCKER_CONFIG at a directory holding config.
func dockerConfigDir(t *testing.T, config string) {
	t.Helper()
	dir := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(dir, "config.json"), []byte(config), 0o600))
	t.Setenv("DOCKER_CONFIG", dir)
	t.Setenv("GITHUB_TOKEN", "")
	t.Setenv("DEVCONTAINERS_OCI_AUTH", "")
}

// fakeHelper installs docker-credential-<name> on PATH, printing out for
// the registry it is asked about and failing ("credentials not found")
// for any other.
func fakeHelper(t *testing.T, name, registry, out string) {
	t.Helper()
	bin := t.TempDir()
	script := "#!/bin/sh\nread reg\nif [ \"$reg\" = \"" + registry + "\" ]; then echo '" + out + "'; else echo 'credentials not found in native keychain'; exit 1; fi\n"
	require.NoError(t, os.WriteFile(filepath.Join(bin, "docker-credential-"+name), []byte(script), 0o755))
	t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
}

func TestLookupInlineAuths(t *testing.T) {
	auth := base64.StdEncoding.EncodeToString([]byte("alice:s3cret"))
	dockerConfigDir(t, `{"auths": {
		"https://registry.example.com/v1/": {"auth": "`+auth+`"},
		"other.example.com:5000": {"username": "bob", "password": "pw"}
	}}`)

	creds, ok, err := Lookup(context.Background(), "registry.example.com")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Credentials{Username: "alice", Password: "s3cret"}, creds)

	creds, ok, err = Lookup(context.Background(), "other.example.com:5000")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "bob", creds.Username)

	_, ok, err = Lookup(context.Background(), "unknown.example.com")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestLookupCredentialHelpers(t *testing.T) {
	fakeHelper(t, "fake", "registry.example.com", `{"Username": "carol", "Secret": "from-helper"}`)
	dockerConfigDir(t, `{"credHelpers": {"registry.example.com": "fake"}, "credsStore": "fake"}`)

	creds, ok, err := Lookup(context.Background(), "registry.example.com")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Credentials{Username: "carol", Password: "from-helper"}, creds)

	// The store has nothing for another registry: no credentials, no error.
	_, ok, err = Lookup(context.Background(), "unknown.example.com")
	require.NoError(t, err)
	assert.False(t, ok)
}

func TestLookupGitHubTokenForGHCR(t *testing.T) {
	dockerConfigDir(t, `{}`)
	t.Setenv("GITHUB_TOKEN", "ghp_x")
	creds, ok, err := Lookup(context.Background(), "ghcr.io")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, "ghp_x", creds.Password)

	_, ok, _ = Lookup(context.Background(), "registry.example.com")
	assert.False(t, ok, "GITHUB_TOKEN is only for ghcr.io")
}

func TestLookupWithoutDockerConfig(t *testing.T) {
	t.Setenv("DOCKER_CONFIG", t.TempDir())
	t.Setenv("GITHUB_TOKEN", "")
	_, ok, err := Lookup(context.Background(), "ghcr.io")
	require.NoError(t, err)
	assert.False(t, ok)
}

// DEVCONTAINERS_OCI_AUTH (registry|user|token, comma-separated) comes
// first, as in devcontainers/cli.
func TestLookupDevcontainersOCIAuth(t *testing.T) {
	dockerConfigDir(t, `{"auths": {"registry.example.com": {"username": "bob", "password": "pw"}}}`)
	t.Setenv("DEVCONTAINERS_OCI_AUTH", "other.example.com|x|y,registry.example.com|alice|tok")
	creds, ok, err := Lookup(context.Background(), "registry.example.com")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Credentials{Username: "alice", Password: "tok"}, creds)
}

// Identity tokens (az acr login) are refresh tokens, from the config or a
// helper's "<token>" user.
func TestLookupIdentityTokens(t *testing.T) {
	dockerConfigDir(t, `{"auths": {"acr.azurecr.io": {"auth": "", "identitytoken": "refresh-1"}}, "credHelpers": {"helped.azurecr.io": "fake"}}`)
	creds, ok, err := Lookup(context.Background(), "acr.azurecr.io")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Credentials{IdentityToken: "refresh-1"}, creds)

	fakeHelper(t, "fake", "helped.azurecr.io", `{"Username": "<token>", "Secret": "refresh-2"}`)
	creds, ok, err = Lookup(context.Background(), "helped.azurecr.io")
	require.NoError(t, err)
	require.True(t, ok)
	assert.Equal(t, Credentials{IdentityToken: "refresh-2"}, creds)
}
