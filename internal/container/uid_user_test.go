package container

import (
	"testing"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/stretchr/testify/assert"
)

// The UID update applies to remoteUser (or containerUser), else the runArgs
// --user or compose service user, else the image's USER (M2), as
// devcontainers/cli picks it.
func TestUIDUser(t *testing.T) {
	for _, tc := range []struct {
		name                 string
		effective, runArgs   string
		service, image, want string
	}{
		{"remoteUser wins", "vscode", "app", "svc", "node", "vscode"},
		{"runArgs --user", "", "app:app", "", "node", "app"},
		{"compose user", "", "", "svc", "node", "svc"},
		{"image USER", "", "", "", "node", "node"},
	} {
		r := &Provisioner{resolved: &devcontainer.ResolvedDevContainer{EffectiveUser: tc.effective}}
		if tc.runArgs != "" {
			r.resolved.RunArgs = &devcontainer.ParsedRunArgs{User: tc.runArgs}
		}
		assert.Equal(t, tc.want, r.uidUser(tc.service, tc.image), tc.name)
	}
}
