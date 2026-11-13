package env

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/agentproto"
	"github.com/griffithind/dcx/internal/container"
)

const (
	// etcEnvironment is the file pam_env (and anything sourcing it) reads.
	etcEnvironment = "/etc/environment"

	// blockBegin and blockEnd delimit the block dcx owns in
	// /etc/environment; everything outside it is left untouched.
	blockBegin = "# BEGIN dcx managed environment"
	blockEnd   = "# END dcx managed environment"

	// markerEtcProfile records that /etc/profile was patched.
	markerEtcProfile = agentproto.StateDir + "/.patchEtcProfileMarker"

	// legacyMarkerEtcEnvironment was written by older dcx versions, which
	// appended to /etc/environment once and never updated it.
	legacyMarkerEtcEnvironment = agentproto.StateDir + "/.patchEtcEnvironmentMarker"
)

// Patcher patches container system files for proper environment setup.
type Patcher struct {
	engine container.Engine
}

// NewPatcher creates a Patcher that works through engine.
func NewPatcher(engine container.Engine) *Patcher {
	return &Patcher{engine: engine}
}

// PatchEtcEnvironment rewrites dcx's block in /etc/environment with env, so
// the variables reach processes that read it (PAM sessions, cron) and
// changed values replace old ones on every `dcx up`.
//
// The file content is built here and written through the engine (stdin to
// a fixed command): values are never spliced into a shell script.
// Variables whose value the file format cannot express (newlines, quotes,
// backslashes) are returned as skipped; sessions still get them through
// the agent's session configuration.
func (p *Patcher) PatchEtcEnvironment(ctx context.Context, containerName string, env map[string]string) (skipped []string, err error) {
	// Only a missing file reads as empty: any other failure must not make
	// the rewrite drop the lines that are there.
	current, err := p.engine.ExecOutput(ctx, containerName, readIfExists)
	if err != nil {
		return nil, fmt.Errorf("read %s: %w", etcEnvironment, err)
	}
	content, skipped := renderEtcEnvironment(string(current), env)
	if content == string(current) {
		return skipped, nil
	}
	if err := p.engine.WriteFileInContainer(ctx, containerName, etcEnvironment, []byte(content), "root"); err != nil {
		return skipped, fmt.Errorf("write %s: %w", etcEnvironment, err)
	}
	if err := p.engine.ChmodInContainer(ctx, containerName, etcEnvironment, "0644", "root"); err != nil {
		return skipped, fmt.Errorf("chmod %s: %w", etcEnvironment, err)
	}
	_, _ = p.engine.ExecAsRoot(ctx, containerName, []string{"rm", "-f", legacyMarkerEtcEnvironment})
	return skipped, nil
}

// readIfExists prints /etc/environment, or nothing when it does not exist.
var readIfExists = []string{"sh", "-c", `if [ -e "$1" ]; then cat "$1"; fi`, "dcx-read", etcEnvironment}

// renderEtcEnvironment returns current with dcx's block replaced by one
// holding env (sorted), or removed when env is empty.
func renderEtcEnvironment(current string, env map[string]string) (string, []string) {
	var kept []string
	inBlock := false
	for _, line := range strings.Split(strings.TrimRight(current, "\n"), "\n") {
		switch {
		case line == blockBegin:
			inBlock = true
		case line == blockEnd:
			inBlock = false
		case !inBlock && (line != "" || len(kept) > 0):
			kept = append(kept, line)
		}
	}

	keys := make([]string, 0, len(env))
	for k := range env {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	var block, skipped []string
	for _, k := range keys {
		v := env[k]
		// pam_env reads KEY="value" without escapes: a quote or backslash
		// would end or alter the value.
		if !validEnvKey(k) || strings.ContainsAny(v, "\n\r\"\\") {
			skipped = append(skipped, k)
			continue
		}
		block = append(block, k+`="`+v+`"`)
	}

	lines := kept
	if len(block) > 0 {
		lines = append(lines, blockBegin)
		lines = append(lines, block...)
		lines = append(lines, blockEnd)
	}
	if len(lines) == 0 {
		return "", skipped
	}
	return strings.Join(lines, "\n") + "\n", skipped
}

// validEnvKey reports whether k is a portable environment variable name.
func validEnvKey(k string) bool {
	if k == "" || (k[0] >= '0' && k[0] <= '9') {
		return false
	}
	for _, r := range k {
		if r != '_' && (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') {
			return false
		}
	}
	return true
}

// PatchEtcProfile modifies /etc/profile to preserve an existing PATH, so the
// system profile does not clobber PATH entries features added. It runs
// once per container (a marker records it). The script is fixed; nothing
// is interpolated into it.
func (p *Patcher) PatchEtcProfile(ctx context.Context, containerName string) error {
	// The sed expression turns PATH=/some/path into PATH=${PATH:-/some/path}.
	const script = `test -f "$1" && exit 0
mkdir -p "$(dirname "$1")"
sed -i -E 's/((^|[[:space:]])PATH=)([^$]*)$/\1\${PATH:-\3}/g' /etc/profile 2>/dev/null || true
touch "$1"`
	if _, err := p.engine.ExecAsRoot(ctx, containerName, []string{"sh", "-c", script, "dcx-patch", markerEtcProfile}); err != nil {
		return fmt.Errorf("patch /etc/profile: %w", err)
	}
	return nil
}
