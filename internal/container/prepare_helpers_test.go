package container_test

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container"
)

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
