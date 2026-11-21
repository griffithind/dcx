package container_test

import (
	"context"
	"testing"

	"github.com/griffithind/dcx/internal/container"
	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/griffithind/dcx/internal/secrets"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// argsOf returns the arguments of every call to method, in order.
func argsOf(fake *containertest.Fake, method string) [][]string {
	var out [][]string
	for _, c := range fake.CallsTo(method) {
		out = append(out, c.Args)
	}
	return out
}

func TestMountSecretsWritesOwnedReadOnlyFiles(t *testing.T) {
	fake := &containertest.Fake{}
	list := []secrets.Secret{{Name: "token", Value: []byte("t0k")}, {Name: "npmrc", Value: []byte("//r/:_auth=x")}}
	require.NoError(t, container.MountSecretsToContainer(context.Background(), fake, "c1", list, "dev"))

	assert.Equal(t, []byte("t0k"), fake.Files["c1:/run/secrets/token"])
	assert.Equal(t, []byte("//r/:_auth=x"), fake.Files["c1:/run/secrets/npmrc"])
	// One write each: the tmpfs directory, then the file, owned by the
	// remote user and readable only by it.
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets/token", "dev", "400", "/run/secrets"},
		{"c1", "/run/secrets/npmrc", "dev", "400", "/run/secrets"},
	}, argsOf(fake, "WriteFileInContainer"))
}

func TestMountSecretsDefaultsAndRefusals(t *testing.T) {
	ctx := context.Background()

	fake := &containertest.Fake{}
	require.NoError(t, container.MountSecretsToContainer(ctx, fake, "c1", nil, "dev"))
	assert.Empty(t, fake.Calls(), "no secrets: nothing touched")

	require.NoError(t, container.MountSecretsToContainer(ctx, fake, "c1", []secrets.Secret{{Name: "s", Value: []byte("v")}}, ""))
	assert.Equal(t, "root", fake.CallsTo("WriteFileInContainer")[0].Args[2], "no owner: root")

	for _, name := range []string{"../escape", "a/b", ""} {
		fake := &containertest.Fake{}
		err := container.MountSecretsToContainer(ctx, fake, "c1", []secrets.Secret{{Name: name, Value: []byte("v")}}, "dev")
		require.Error(t, err, "name %q", name)
		assert.Empty(t, fake.Files, "name %q: nothing written", name)
	}

	err := container.MountSecretsToContainer(ctx, &containertest.Fake{}, "", []secrets.Secret{{Name: "s"}}, "dev")
	require.Error(t, err)
}

func TestMountDCXSecretsUsesModesAndOwners(t *testing.T) {
	fake := &containertest.Fake{}
	require.NoError(t, container.MountDCXSecrets(context.Background(), fake, "c1", []container.DCXSecret{
		{Name: "authorized_keys", Value: []byte("ssh-ed25519 AAAA"), Mode: "0444"},
		{Name: "host_key", Value: []byte("PRIVATE")},
		{Name: "user_file", Value: []byte("x"), Mode: "0440", Owner: "dev"},
	}))

	assert.Equal(t, []byte("ssh-ed25519 AAAA"), fake.Files["c1:"+container.DCXSecretPath("authorized_keys")])
	dirs := "/run/secrets,/run/secrets/dcx"
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets/dcx/authorized_keys", "root", "0444", dirs},
		{"c1", "/run/secrets/dcx/host_key", "root", "0400", dirs},
		{"c1", "/run/secrets/dcx/user_file", "dev", "0440", dirs},
	}, argsOf(fake, "WriteFileInContainer"), "the host key defaults to 0400, owner to root")

	err := container.MountDCXSecrets(context.Background(), &containertest.Fake{}, "c1", []container.DCXSecret{{Name: "../x"}})
	require.Error(t, err)
}
