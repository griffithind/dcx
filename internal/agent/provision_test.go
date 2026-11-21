package agent

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// testProvisioner provisions into a temporary directory, without root
// (no chown) and with a fake listener.
func testProvisioner(t *testing.T) (*provisioner, string, *[]agentproto.ListenConfig) {
	t.Helper()
	root := t.TempDir()
	in := func(rel string) string { return filepath.Join(root, rel) }
	require.NoError(t, os.MkdirAll(in("etc"), 0o755))
	var started []agentproto.ListenConfig
	p := &provisioner{
		stateDir:        in("var/lib/dcx"),
		runDir:          in("run/dcx"),
		shellDir:        in("run/dcx/shell"),
		logPath:         in("var/lib/dcx/agent.log"),
		secretsDir:      in("run/secrets"),
		dcxSecretsDir:   in("run/secrets/dcx"),
		hostKey:         in("run/secrets/dcx/ssh_host_ed25519_key"),
		authorizedKeys:  in("run/secrets/dcx/authorized_keys"),
		sessionPath:     in("run/dcx/session.json"),
		etcEnvironment:  in("etc/environment"),
		etcProfile:      in("etc/profile"),
		profileMarker:   in("var/lib/dcx/.patchEtcProfileMarker"),
		legacyEnvMarker: in("var/lib/dcx/.patchEtcEnvironmentMarker"),
		probeTimeout:    10 * time.Second,
		listening:       func(string) bool { return false },
	}
	p.start = func(_ context.Context, cfg agentproto.ListenConfig) error {
		started = append(started, cfg)
		return nil
	}
	return p, root, &started
}

func currentUser(t *testing.T) string {
	t.Helper()
	u, err := user.Current()
	require.NoError(t, err)
	return u.Username
}

func mode(t *testing.T, path string) os.FileMode {
	t.Helper()
	st, err := os.Stat(path)
	require.NoError(t, err)
	return st.Mode().Perm()
}

// One request writes everything: SSH and runtime secrets with their
// modes, dcx's /etc/environment block, the /etc/profile patch, the session
// configuration (remoteEnv over the probed environment), and starts the
// listener as the container's default user.
func TestProvisionWritesEverything(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the probe reads /proc/self/environ")
	}
	p, _, started := testProvisioner(t)
	require.NoError(t, os.WriteFile(p.etcEnvironment, []byte("LANG=C\n"), 0o600))
	require.NoError(t, os.WriteFile(p.etcProfile, []byte("PATH=/usr/bin:/bin\nexport PATH\n"), 0o644))
	require.NoError(t, os.MkdirAll(filepath.Dir(p.legacyEnvMarker), 0o755))
	require.NoError(t, os.WriteFile(p.legacyEnvMarker, nil, 0o600))
	t.Setenv("DCX_PROVISION_TEST", "probed")
	me := currentUser(t)

	res, err := p.provision(context.Background(), agentproto.ProvisionRequest{
		HostKey:        []byte("PRIVATE"),
		AuthorizedKeys: []byte("ssh-ed25519 AAAA"),
		Secrets:        []agentproto.Secret{{Name: "token", Value: []byte("t0k")}},
		Environment:    map[string]string{"A": "1", "BAD": `a"b`},
		Probe:          "loginShell",
		Session: agentproto.SessionConfig{
			Env:   map[string]string{"DCX_PROVISION_TEST": "remote", "R": "r"},
			Unset: []string{"GONE"},
		},
		Listen:      agentproto.ListenConfig{Addr: "0.0.0.0:48022", WorkDir: "/w"},
		DefaultUser: me,
	})
	require.NoError(t, err)
	assert.True(t, res.Started)
	assert.Equal(t, []string{"BAD"}, res.Skipped)
	assert.Positive(t, res.Probed)
	assert.Empty(t, res.Warnings)

	for path, want := range map[string]os.FileMode{
		p.hostKey: 0o400, p.authorizedKeys: 0o444,
		filepath.Join(p.secretsDir, "token"): 0o400,
		p.etcEnvironment:                     0o644,
		p.sessionPath:                        0o644,
		p.secretsDir:                         0o755, p.dcxSecretsDir: 0o755, p.runDir: 0o755,
	} {
		assert.Equal(t, want, mode(t, path), path)
	}
	secret, _ := os.ReadFile(filepath.Join(p.secretsDir, "token"))
	assert.Equal(t, "t0k", string(secret))
	envFile, _ := os.ReadFile(p.etcEnvironment)
	assert.Equal(t, "LANG=C\n# BEGIN dcx managed environment\nA=\"1\"\n# END dcx managed environment\n", string(envFile))
	profile, _ := os.ReadFile(p.etcProfile)
	assert.Equal(t, "PATH=${PATH:-/usr/bin:/bin}\nexport PATH\n", string(profile))
	assert.NoFileExists(t, p.legacyEnvMarker, "the marker older dcx left is removed")

	var session agentproto.SessionConfig
	data, _ := os.ReadFile(p.sessionPath)
	require.NoError(t, json.Unmarshal(data, &session))
	assert.Equal(t, "remote", session.Env["DCX_PROVISION_TEST"], "remoteEnv wins over the probed value")
	assert.Equal(t, "r", session.Env["R"])
	assert.NotEmpty(t, session.Env["PATH"], "the probed environment is kept")
	assert.Equal(t, []string{"GONE"}, session.Unset)

	require.Len(t, *started, 1)
	assert.Equal(t, agentproto.ListenConfig{Addr: "0.0.0.0:48022", User: me, WorkDir: "/w"}, (*started)[0],
		"without a session user, the container's default user")
}

// A second provision leaves a listening agent running and patches
// /etc/profile only once.
func TestProvisionIsIdempotent(t *testing.T) {
	p, _, started := testProvisioner(t)
	require.NoError(t, os.WriteFile(p.etcProfile, []byte("PATH=/bin\n"), 0o644))
	req := agentproto.ProvisionRequest{Listen: agentproto.ListenConfig{Addr: "0.0.0.0:48022", User: currentUser(t)}}
	_, err := p.provision(context.Background(), req)
	require.NoError(t, err)

	require.NoError(t, os.WriteFile(p.etcProfile, []byte("PATH=/sbin\n"), 0o644))
	p.listening = func(string) bool { return true }
	res, err := p.provision(context.Background(), req)
	require.NoError(t, err)
	assert.False(t, res.Started)
	assert.Len(t, *started, 1)
	profile, _ := os.ReadFile(p.etcProfile)
	assert.Equal(t, "PATH=/sbin\n", string(profile), "patched once per container")
}

// Secret names are file names in the secrets directory, never paths.
func TestProvisionRejectsSecretPaths(t *testing.T) {
	for _, name := range []string{"../escape", "a/b", "", ".."} {
		p, root, _ := testProvisioner(t)
		_, err := p.provision(context.Background(), agentproto.ProvisionRequest{
			Secrets: []agentproto.Secret{{Name: name, Value: []byte("x")}},
		})
		require.Error(t, err, "name %q", name)
		assert.NoFileExists(t, filepath.Join(root, "run/escape"))
	}
}

// Files are replaced, never written through a symlink a container user
// could have planted.
func TestWriteAtomicReplacesSymlinks(t *testing.T) {
	p, root, _ := testProvisioner(t)
	target := filepath.Join(root, "outside")
	require.NoError(t, os.WriteFile(target, []byte("keep"), 0o644))
	link := filepath.Join(root, "file")
	require.NoError(t, os.Symlink(target, link))
	require.NoError(t, p.writeAtomic(link, []byte("new"), 0o600, 0, 0))
	data, _ := os.ReadFile(target)
	assert.Equal(t, "keep", string(data))
	st, err := os.Lstat(link)
	require.NoError(t, err)
	assert.True(t, st.Mode().IsRegular())

	// /etc/environment is written in place (it may be a bind mount), so a
	// symlink there is refused instead.
	require.NoError(t, os.Symlink(target, p.etcEnvironment))
	_, err = p.patchEtcEnvironment(map[string]string{"A": "1"})
	assert.Error(t, err)
	data, _ = os.ReadFile(target)
	assert.Equal(t, "keep", string(data))
}

// A listener that never answers fails with the agent log's tail, so the
// cause (a bad host key, a port in use) is visible.
func TestStartListenerTimeoutIncludesLog(t *testing.T) {
	old := listenTimeout
	listenTimeout = 300 * time.Millisecond
	t.Cleanup(func() { listenTimeout = old })
	p, _, _ := testProvisioner(t)
	require.NoError(t, p.setup())
	require.NoError(t, os.WriteFile(p.logPath, []byte("bind: address already in use\n"), 0o600))
	trueBin, err := exec.LookPath("true") // /bin/true on Linux, /usr/bin/true on macOS
	require.NoError(t, err)
	p.binary = trueBin

	err = p.startListener(context.Background(), agentproto.ListenConfig{Addr: "0.0.0.0:48022"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not start listening")
	assert.Contains(t, err.Error(), "address already in use")
}

func TestDefaultUserName(t *testing.T) {
	assert.Equal(t, "root", defaultUserName(""))
	assert.Equal(t, "node", defaultUserName("node"))
	assert.Equal(t, "node", defaultUserName("node:staff"))
	assert.Equal(t, "root", defaultUserName("0"), "a uid with a passwd entry is named")
	assert.Equal(t, "54321", defaultUserName("54321:54321"), "a uid without one stays numeric")
}
