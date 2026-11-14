// Package registryauth finds credentials for an OCI registry the way the
// docker CLI does, so features can come from private registries: the
// docker config ($DOCKER_CONFIG/config.json, default ~/.docker/config.json)
// with its inline "auths", per-registry "credHelpers" and default
// "credsStore", and, for ghcr.io, $GITHUB_TOKEN.
package registryauth

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// Credentials authenticate to a registry.
type Credentials struct {
	Username string
	Password string
}

// helperTimeout bounds a credential helper (some prompt or hang when their
// backing store is locked).
const helperTimeout = 30 * time.Second

// dockerConfig is the part of config.json that names credentials.
type dockerConfig struct {
	Auths map[string]struct {
		Auth     string `json:"auth"`
		Username string `json:"username"`
		Password string `json:"password"`
	} `json:"auths"`
	CredHelpers map[string]string `json:"credHelpers"`
	CredsStore  string            `json:"credsStore"`
}

// Lookup returns the credentials for registry (a host, optionally with a
// port, as in a feature ID), or ok false when none are configured. An
// error means credentials are configured but could not be read.
func Lookup(ctx context.Context, registry string) (Credentials, bool, error) {
	cfg, err := loadDockerConfig()
	if err != nil {
		return Credentials{}, false, err
	}
	if cfg != nil {
		if helper := cfg.CredHelpers[registry]; helper != "" {
			return fromHelper(ctx, helper, registry)
		}
		for key, entry := range cfg.Auths {
			if normalize(key) != registry {
				continue
			}
			if entry.Auth != "" {
				return decodeAuth(entry.Auth, registry)
			}
			if entry.Username != "" {
				return Credentials{Username: entry.Username, Password: entry.Password}, true, nil
			}
			// An empty entry: the secret is in the credsStore.
		}
		if cfg.CredsStore != "" {
			creds, ok, err := fromHelper(ctx, cfg.CredsStore, registry)
			if ok || err != nil {
				return creds, ok, err
			}
		}
	}
	if registry == "ghcr.io" {
		if token := os.Getenv("GITHUB_TOKEN"); token != "" {
			// ghcr.io accepts any username with a token as the password.
			return Credentials{Username: "token", Password: token}, true, nil
		}
	}
	return Credentials{}, false, nil
}

// loadDockerConfig reads the docker CLI's config.json (nil when absent).
func loadDockerConfig() (*dockerConfig, error) {
	dir := os.Getenv("DOCKER_CONFIG")
	if dir == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return nil, nil
		}
		dir = filepath.Join(home, ".docker")
	}
	path := filepath.Join(dir, "config.json")
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", path, err)
	}
	var cfg dockerConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse %s: %w", path, err)
	}
	return &cfg, nil
}

// normalize reduces an "auths" key ("https://ghcr.io/v1/", "ghcr.io") to
// the registry host.
func normalize(key string) string {
	key = strings.TrimPrefix(strings.TrimPrefix(key, "https://"), "http://")
	host, _, _ := strings.Cut(key, "/")
	return host
}

func decodeAuth(auth, registry string) (Credentials, bool, error) {
	raw, err := base64.StdEncoding.DecodeString(auth)
	if err != nil {
		return Credentials{}, false, fmt.Errorf("docker config: invalid auth for %s: %w", registry, err)
	}
	user, pass, found := strings.Cut(string(raw), ":")
	if !found {
		return Credentials{}, false, fmt.Errorf("docker config: invalid auth for %s", registry)
	}
	return Credentials{Username: user, Password: pass}, true, nil
}

// fromHelper asks docker-credential-<helper> for registry's credentials
// (the credential helper protocol's "get"). A helper that has none says
// so with "credentials not found", which is not an error.
func fromHelper(ctx context.Context, helper, registry string) (Credentials, bool, error) {
	ctx, cancel := context.WithTimeout(ctx, helperTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, "docker-credential-"+helper, "get")
	cmd.Stdin = strings.NewReader(registry)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		if strings.Contains(string(out)+stderr.String(), "credentials not found") {
			return Credentials{}, false, nil
		}
		return Credentials{}, false, fmt.Errorf("credential helper docker-credential-%s: %w: %s", helper, err, strings.TrimSpace(stderr.String()+string(out)))
	}
	var resp struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return Credentials{}, false, fmt.Errorf("credential helper docker-credential-%s: %w", helper, err)
	}
	if resp.Secret == "" {
		return Credentials{}, false, nil
	}
	return Credentials{Username: resp.Username, Password: resp.Secret}, true, nil
}
