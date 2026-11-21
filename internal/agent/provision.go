package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/env"
)

// runProvisionCmd reads an agentproto.ProvisionRequest on stdin, provisions
// the container (see provision) and prints an agentproto.ProvisionResult on
// stdout, also when it fails. dcx runs it as root, in one `docker exec`, on
// every `dcx up` that does anything.
func runProvisionCmd(args []string) error {
	fs := flag.NewFlagSet("provision", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if os.Geteuid() != 0 {
		return fmt.Errorf("provision must run as root")
	}
	var req agentproto.ProvisionRequest
	if err := json.NewDecoder(os.Stdin).Decode(&req); err != nil {
		return fmt.Errorf("read provision request: %w", err)
	}
	p := newProvisioner()
	res, err := p.provision(context.Background(), req)
	if err != nil {
		res.Error = err.Error()
	}
	if encErr := json.NewEncoder(os.Stdout).Encode(res); encErr != nil && err == nil {
		err = encErr
	}
	return err
}

// provisioner writes what a ProvisionRequest asks for. Its paths are the
// agentproto ones; tests point them at a temporary directory and run
// without root (no chown) and without starting a listener.
type provisioner struct {
	stateDir, runDir, shellDir, logPath  string
	secretsDir, dcxSecretsDir            string
	hostKey, authorizedKeys, sessionPath string
	etcEnvironment, etcProfile           string
	profileMarker, legacyEnvMarker       string

	// root is true when running as root: files are chowned.
	root bool
	// listening reports whether an agent answers on addr.
	listening func(addr string) bool
	// start starts the listener (detached) and waits until it answers.
	start func(ctx context.Context, cfg agentproto.ListenConfig) error
	// probeTimeout bounds the userEnvProbe.
	probeTimeout time.Duration
	// binary is the agent binary the listener runs ("" is this one).
	binary string
}

func newProvisioner() *provisioner {
	p := &provisioner{
		stateDir:        agentproto.StateDir,
		runDir:          agentproto.RunDir,
		shellDir:        agentproto.ShellIntegrationDir,
		logPath:         agentproto.LogPath,
		secretsDir:      agentproto.SecretsDir,
		dcxSecretsDir:   agentproto.DCXSecretsDir,
		hostKey:         agentproto.HostKeyPath,
		authorizedKeys:  agentproto.AuthorizedKeysPath,
		sessionPath:     agentproto.SessionConfigPath,
		etcEnvironment:  "/etc/environment",
		etcProfile:      "/etc/profile",
		profileMarker:   agentproto.ProvisionMarkerEtcProfile,
		legacyEnvMarker: agentproto.LegacyMarkerEtcEnvironment,
		root:            os.Geteuid() == 0,
		probeTimeout:    10 * time.Second,
		listening: func(addr string) bool {
			return pingAddr(agentproto.PingAddr(addr)) == nil
		},
	}
	p.start = p.startListener
	return p
}

// provision does, in order: the root-owned directories, shell integration
// and log (setup); the SSH host key and authorized_keys; the runtime
// secrets; dcx's block of /etc/environment and the one-time /etc/profile
// patch; the userEnvProbe; the session configuration; and the listener,
// unless one already answers. A failed probe or system-file patch is a
// warning (sessions still work); anything else stops provisioning.
func (p *provisioner) provision(ctx context.Context, req agentproto.ProvisionRequest) (agentproto.ProvisionResult, error) {
	var res agentproto.ProvisionResult
	if req.Listen.User == "" {
		req.Listen.User = defaultUserName(req.DefaultUser)
	}
	if req.ProbeUser == "" {
		req.ProbeUser = req.Listen.User
	}
	if err := p.setup(); err != nil {
		return res, fmt.Errorf("prepare agent: %w", err)
	}
	if err := p.writeSSHSecrets(req); err != nil {
		return res, err
	}
	if err := p.writeRuntimeSecrets(req.Secrets, req.SecretsOwner); err != nil {
		return res, err
	}

	skipped, err := p.patchEtcEnvironment(req.Environment)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Failed to patch /etc/environment: %v", err))
	}
	res.Skipped = skipped
	if err := p.patchEtcProfile(); err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Failed to patch /etc/profile: %v", err))
	}

	probed, err := p.probe(ctx, req.Probe, req.ProbeUser)
	if err != nil {
		res.Warnings = append(res.Warnings, fmt.Sprintf("Environment probe failed: %v", err))
	}
	res.Probed = len(probed)

	session := req.Session
	if len(probed)+len(req.Session.Env) > 0 {
		session.Env = make(map[string]string, len(probed)+len(req.Session.Env))
		for k, v := range probed {
			session.Env[k] = v
		}
		for k, v := range req.Session.Env {
			session.Env[k] = v
		}
	}
	data, err := json.Marshal(session)
	if err != nil {
		return res, err
	}
	// World-readable: sessions run as the session user; it holds nothing a
	// session would not see anyway.
	if err := p.writeAtomic(p.sessionPath, data, 0o644, 0, 0); err != nil {
		return res, fmt.Errorf("write session configuration: %w", err)
	}

	if p.listening(req.Listen.Addr) {
		return res, nil
	}
	if err := p.start(ctx, req.Listen); err != nil {
		return res, err
	}
	res.Started = true
	return res, nil
}

// setup prepares the root-owned directories the agent uses (no container
// user can plant symlinks or swap files there), the shell integration
// scripts and the agent log.
func (p *provisioner) setup() error {
	for _, dir := range []string{p.stateDir, p.runDir, p.shellDir} {
		if err := p.ensureRootDir(dir); err != nil {
			return err
		}
	}
	if err := WriteShellIntegration(p.shellDir); err != nil {
		return fmt.Errorf("write shell integration: %w", err)
	}
	f, err := os.OpenFile(p.logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC|syscall.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create agent log: %w", err)
	}
	defer func() { _ = f.Close() }()
	if !p.root {
		return nil
	}
	// A log left by an older agent may belong to its non-root listener.
	return f.Chown(0, 0)
}

// ensureRootDir creates dir if needed and makes sure it is a real
// directory (not a symlink), mode 0755, owned by root.
func (p *provisioner) ensureRootDir(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	st, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !st.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}
	if p.root {
		if err := os.Chown(dir, 0, 0); err != nil {
			return err
		}
	}
	return os.Chmod(dir, 0o755)
}

// writeSSHSecrets writes the host key (0400) and authorized_keys (0444),
// root-owned, into the root-owned dcx secrets directory.
func (p *provisioner) writeSSHSecrets(req agentproto.ProvisionRequest) error {
	for _, dir := range []string{p.secretsDir, p.dcxSecretsDir} {
		if err := p.ensureRootDir(dir); err != nil {
			return fmt.Errorf("prepare %s: %w", dir, err)
		}
	}
	if err := p.writeAtomic(p.authorizedKeys, req.AuthorizedKeys, 0o444, 0, 0); err != nil {
		return fmt.Errorf("write authorized_keys: %w", err)
	}
	if err := p.writeAtomic(p.hostKey, req.HostKey, 0o400, 0, 0); err != nil {
		return fmt.Errorf("write host key: %w", err)
	}
	return nil
}

// writeRuntimeSecrets writes each secret to the secrets directory, 0400,
// owned by owner ("" is root).
func (p *provisioner) writeRuntimeSecrets(secrets []agentproto.Secret, owner string) error {
	if len(secrets) == 0 {
		return nil
	}
	uid, gid, err := userIDs(owner)
	if err != nil {
		return fmt.Errorf("secrets owner: %w", err)
	}
	if err := p.ensureRootDir(p.secretsDir); err != nil {
		return fmt.Errorf("prepare %s: %w", p.secretsDir, err)
	}
	for _, s := range secrets {
		if s.Name == "" || s.Name == "." || s.Name == ".." || strings.ContainsAny(s.Name, "/\x00") {
			return fmt.Errorf("invalid secret name %q", s.Name)
		}
		if err := p.writeAtomic(filepath.Join(p.secretsDir, s.Name), s.Value, 0o400, uid, gid); err != nil {
			return fmt.Errorf("write secret %q: %w", s.Name, err)
		}
	}
	return nil
}

// writeAtomic writes data to path through a new file in the same
// directory, renamed into place: never through a symlink, never partially.
// The directory must be root-owned (no container user can plant files in
// it).
func (p *provisioner) writeAtomic(path string, data []byte, mode os.FileMode, uid, gid int) error {
	f, err := os.CreateTemp(filepath.Dir(path), ".dcx-*")
	if err != nil {
		return err
	}
	tmp := f.Name()
	defer func() { _ = os.Remove(tmp) }()
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if p.root {
		if err := f.Chown(uid, gid); err != nil {
			_ = f.Close()
			return err
		}
	}
	if err := f.Chmod(mode); err != nil {
		_ = f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// patchEtcEnvironment rewrites dcx's block of /etc/environment with env
// (kept in place: the file may be a bind mount). It returns the variables
// the file cannot express. Only a missing file reads as empty: a read
// failure must not drop the lines that are there.
func (p *provisioner) patchEtcEnvironment(environment map[string]string) ([]string, error) {
	_ = os.Remove(p.legacyEnvMarker)
	current, err := os.ReadFile(p.etcEnvironment)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	content, skipped := env.RenderEtcEnvironment(string(current), environment)
	if content == string(current) {
		return skipped, nil
	}
	return skipped, writeInPlace(p.etcEnvironment, []byte(content), 0o644)
}

// patchEtcProfile applies env.PatchProfilePATH to /etc/profile once per
// container (a marker records it).
func (p *provisioner) patchEtcProfile() error {
	if _, err := os.Lstat(p.profileMarker); err == nil {
		return nil
	}
	current, err := os.ReadFile(p.etcProfile)
	if err == nil {
		if patched := env.PatchProfilePATH(string(current)); patched != string(current) {
			if err := writeInPlace(p.etcProfile, []byte(patched), 0); err != nil {
				return err
			}
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	return p.writeAtomic(p.profileMarker, nil, 0o600, 0, 0)
}

// writeInPlace replaces path's content without replacing the file (a
// symlink is refused). mode, when not 0, is applied.
func writeInPlace(path string, data []byte, mode os.FileMode) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC|syscall.O_NOFOLLOW, 0o644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		_ = f.Close()
		return err
	}
	if mode != 0 {
		if err := f.Chmod(mode); err != nil {
			_ = f.Close()
			return err
		}
	}
	return f.Close()
}

// probe runs the userEnvProbe as user ("" is the agent's own user) with a
// login environment like `docker exec --user` gives (the container's
// environment, the user's HOME) and returns what the shell exported.
func (p *provisioner) probe(ctx context.Context, probeType, userName string) (map[string]string, error) {
	argv := env.ProbeCommand(env.ProbeType(probeType))
	if argv == nil {
		return nil, nil
	}
	ctx, cancel := context.WithTimeout(ctx, p.probeTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	environ := os.Environ()
	if userName != "" {
		cred, err := credentialFor(userName)
		if err != nil {
			return nil, err
		}
		applyCredential(cmd, cred)
		if u, err := lookupUser(userName); err == nil {
			environ = setEnv(environ, "HOME", u.HomeDir)
		}
	}
	cmd.Env = environ
	cmd.Dir = "/"
	var out bytes.Buffer
	cmd.Stdout = &out
	if err := cmd.Run(); err != nil {
		return nil, err
	}
	return env.ParseProbeOutput(out.String()), nil
}

// setEnv returns environ with key set to value.
func setEnv(environ []string, key, value string) []string {
	out := make([]string, 0, len(environ)+1)
	for _, kv := range environ {
		if !strings.HasPrefix(kv, key+"=") {
			out = append(out, kv)
		}
	}
	return append(out, key+"="+value)
}

// defaultUserName names the user `docker exec` runs as by default, given
// the container's Config.User: its user part, by name when the passwd
// database has one (as `id -un || id -u` printed); "" is root.
func defaultUserName(spec string) string {
	name, _, _ := strings.Cut(spec, ":")
	if name == "" {
		return "root"
	}
	if _, err := parseUint32(name); err == nil {
		if u, err := user.LookupId(name); err == nil {
			return u.Username
		}
	}
	return name
}

// userIDs returns the uid and gid of name ("" or root is 0:0; a numeric id
// without a passwd entry is that id for both).
func userIDs(name string) (int, int, error) {
	if name == "" || name == "root" {
		return 0, 0, nil
	}
	u, err := lookupUser(name)
	if err != nil {
		if id, perr := parseUint32(name); perr == nil {
			return int(id), int(id), nil
		}
		return 0, 0, fmt.Errorf("user %q not found in the container", name)
	}
	uid, err := parseUint32(u.Uid)
	if err != nil {
		return 0, 0, err
	}
	gid, err := parseUint32(u.Gid)
	if err != nil {
		return 0, 0, err
	}
	return int(uid), int(gid), nil
}

// startListener starts `dcx-agent listen` detached (its own session, its
// output in the agent log, stdin from /dev/null, so the provisioning exec
// returns without waiting for it) and waits until it answers.
func (p *provisioner) startListener(ctx context.Context, cfg agentproto.ListenConfig) error {
	self := p.binary
	if self == "" {
		exe, err := os.Executable()
		if err != nil {
			return err
		}
		self = exe
	}
	workDir := cfg.WorkDir
	if workDir == "" {
		workDir = "/workspace"
	}
	args := []string{"listen",
		"--addr", cfg.Addr,
		"--host-key", p.hostKey,
		"--authorized-keys", p.authorizedKeys,
		"--user", cfg.User,
		"--workdir", workDir,
	}
	if len(cfg.AllowCIDRs) > 0 {
		args = append(args, "--allow-cidrs", strings.Join(cfg.AllowCIDRs, ","))
	}
	log, err := os.OpenFile(p.logPath, os.O_WRONLY|os.O_APPEND|syscall.O_NOFOLLOW, 0)
	if err != nil {
		return fmt.Errorf("open agent log: %w", err)
	}
	defer func() { _ = log.Close() }()
	cmd := exec.Command(self, args...)
	cmd.Stdout, cmd.Stderr = log, log
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start listener: %w", err)
	}
	_ = cmd.Process.Release()

	deadline := time.Now().Add(listenTimeout)
	for time.Now().Before(deadline) {
		if p.listening(cfg.Addr) {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(50 * time.Millisecond):
		}
	}
	msg := fmt.Sprintf("dcx agent did not start listening within %s", listenTimeout)
	if tail := logTail(p.logPath, 20); tail != "" {
		msg += "; agent log:\n" + tail
	}
	return errors.New(msg)
}

// listenTimeout bounds how long provision waits for the listener.
var listenTimeout = 15 * time.Second

// logTail returns the last n lines of the agent log.
func logTail(path string, n int) string {
	f, err := os.Open(path)
	if err != nil {
		return ""
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) > n {
		lines = lines[len(lines)-n:]
	}
	return strings.Join(lines, "\n")
}

// credentialFor returns the credential to run as name, or nil when it is
// the agent's own user (see sessionCredential).
func credentialFor(name string) (*syscall.Credential, error) {
	u, err := lookupUser(name)
	if err != nil {
		if id, perr := parseUint32(name); perr == nil {
			if id == uint32(os.Geteuid()) {
				return nil, nil
			}
			return &syscall.Credential{Uid: id, Gid: id}, nil
		}
		return nil, fmt.Errorf("user %q not found in the container", name)
	}
	return credentialOf(u, name)
}

func credentialOf(u *user.User, name string) (*syscall.Credential, error) {
	uid, err := parseUint32(u.Uid)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", name, err)
	}
	gid, err := parseUint32(u.Gid)
	if err != nil {
		return nil, fmt.Errorf("user %q: %w", name, err)
	}
	if uid == uint32(os.Geteuid()) {
		return nil, nil
	}
	cred := &syscall.Credential{Uid: uid, Gid: gid}
	if ids, err := u.GroupIds(); err == nil {
		for _, id := range ids {
			if g, err := parseUint32(id); err == nil {
				cred.Groups = append(cred.Groups, g)
			}
		}
	}
	return cred, nil
}
