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
	assert.Equal(t, [][]string{{"c1", "/run/secrets/token", "dev"}, {"c1", "/run/secrets/npmrc", "dev"}},
		argsOf(fake, "ChownInContainer"), "owned by the remote user")
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets", "755", "root"},
		{"c1", "/run/secrets/token", "400", "root"},
		{"c1", "/run/secrets/npmrc", "400", "root"},
	}, argsOf(fake, "ChmodInContainer"))
	for _, w := range argsOf(fake, "WriteFileInContainer") {
		assert.Equal(t, "root", w[2], "written as root, then handed over")
	}
}

func TestMountSecretsDefaultsAndRefusals(t *testing.T) {
	ctx := context.Background()

	fake := &containertest.Fake{}
	require.NoError(t, container.MountSecretsToContainer(ctx, fake, "c1", nil, "dev"))
	assert.Empty(t, fake.Calls(), "no secrets: nothing touched")

	require.NoError(t, container.MountSecretsToContainer(ctx, fake, "c1", []secrets.Secret{{Name: "s", Value: []byte("v")}}, ""))
	assert.Equal(t, []string{"c1", "/run/secrets/s", "root"}, fake.CallsTo("ChownInContainer")[0].Args, "no owner: root")

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
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets", "root"},
		{"c1", "/run/secrets/dcx", "root"},
	}, argsOf(fake, "MkdirInContainer"))
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets", "755", "root"},
		{"c1", "/run/secrets/dcx", "755", "root"},
		{"c1", "/run/secrets/dcx/authorized_keys", "0444", "root"},
		{"c1", "/run/secrets/dcx/host_key", "0400", "root"},
		{"c1", "/run/secrets/dcx/user_file", "0440", "root"},
	}, argsOf(fake, "ChmodInContainer"), "the host key defaults to 0400")
	assert.Equal(t, [][]string{
		{"c1", "/run/secrets/dcx/authorized_keys", "root"},
		{"c1", "/run/secrets/dcx/host_key", "root"},
		{"c1", "/run/secrets/dcx/user_file", "dev"},
	}, argsOf(fake, "ChownInContainer"))

	err := container.MountDCXSecrets(context.Background(), &containertest.Fake{}, "c1", []container.DCXSecret{{Name: "../x"}})
	require.Error(t, err)
}
