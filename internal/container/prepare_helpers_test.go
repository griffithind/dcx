package container_test

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/require"
)

// composePlan is a compose plan for service "app" of /w/compose.yml whose
// model (docker compose config output) is model.
func composePlan(t *testing.T, model string) *devcontainer.ComposePlan {
	t.Helper()
	plan := devcontainer.NewComposePlan([]string{"/w/compose.yml"}, "app", "proj")
	cfg, err := devcontainer.ParseComposeConfig([]byte(model))
	require.NoError(t, err)
	plan.Config = cfg
	return plan
}

// up prepares and creates, as `dcx up` does (with access, or loopback).
func up(t *testing.T, prov *container.Provisioner, opts container.UpOptions, access ...container.SSHAccess) error {
	t.Helper()
	prepared, err := prov.Prepare(context.Background(), opts)
	if err != nil {
		return err
	}
	var a container.SSHAccess
	if len(access) > 0 {
		a = access[0]
	}
	return prov.Create(context.Background(), prepared, a)
}
