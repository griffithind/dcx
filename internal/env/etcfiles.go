package env

import (
	"regexp"
	"sort"
	"strings"
)

// blockBegin and blockEnd delimit the block dcx owns in /etc/environment;
// everything outside it is left untouched.
const (
	blockBegin = "# BEGIN dcx managed environment"
	blockEnd   = "# END dcx managed environment"
)

// RenderEtcEnvironment returns current with dcx's block replaced by one
// holding env (sorted), or removed when env is empty. Variables whose value
// the file format cannot express (newlines, quotes, backslashes) or whose
// name is not portable are left out and returned as skipped; sessions
// still get them through the agent's session configuration.
func RenderEtcEnvironment(current string, env map[string]string) (string, []string) {
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

// profilePATH matches an assignment PATH=<value without $> ending a line,
// as `sed -E 's/((^|[[:space:]])PATH=)([^$]*)$/.../'` did.
var profilePATH = regexp.MustCompile(`((^|[[:space:]])PATH=)([^$]*)$`)

// PatchProfilePATH turns each line's PATH=/some/path into
// PATH=${PATH:-/some/path}, so /etc/profile keeps a PATH that features (or
// the image) set instead of resetting it.
func PatchProfilePATH(profile string) string {
	lines := strings.Split(profile, "\n")
	for i, line := range lines {
		lines[i] = profilePATH.ReplaceAllString(line, "${1}$${PATH:-${3}}")
	}
	return strings.Join(lines, "\n")
}
