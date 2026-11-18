package devcontainer

import (
	"bufio"
	"os"
	"regexp"
	"strings"
)

// metadataImage returns the image whose devcontainer.metadata applies to the
// config: the image of an image plan, the base (FROM) image of a Dockerfile
// plan's target stage, and for compose the primary service's image or the
// base image of its build, from the compose model. "" when it can't be
// determined (a FROM built from an unknown variable, scratch).
func metadataImage(cfg *DevContainerConfig, configPath string, compose *ComposeConfig) string {
	switch cfg.PlanType() {
	case PlanTypeImage:
		return cfg.Image
	case PlanTypeDockerfile:
		plan := buildPlan(cfg, configPath, "").(*DockerfilePlan)
		return dockerfileBaseImage(plan.Dockerfile, cfg.Build.Target, cfg.Build.Args)
	case PlanTypeCompose:
		return compose.baseImage(cfg.Service)
	}
	return ""
}

// dockerfileBaseImage returns the image the target stage (the last stage
// when target is "") is built FROM, following stage references and
// substituting ARG defaults and build args.
func dockerfileBaseImage(path, target string, buildArgs map[string]string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	args := map[string]string{}
	stages := map[string]string{} // lower-cased stage name -> base image
	var last string
	beforeFirstFrom := true
	for _, line := range dockerfileInstructions(string(data)) {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		switch strings.ToUpper(fields[0]) {
		case "ARG":
			// Only ARGs before the first FROM are in scope for FROM lines.
			if !beforeFirstFrom {
				continue
			}
			name, def, _ := strings.Cut(fields[1], "=")
			if v, ok := buildArgs[name]; ok {
				args[name] = v
			} else {
				args[name] = strings.Trim(def, `"'`)
			}
		case "FROM":
			beforeFirstFrom = false
			rest := fields[1:]
			for len(rest) > 0 && strings.HasPrefix(rest[0], "--") {
				rest = rest[1:]
			}
			if len(rest) == 0 {
				continue
			}
			image := expandArgs(rest[0], args)
			if base, ok := stages[strings.ToLower(image)]; ok {
				image = base
			}
			last = image
			if len(rest) >= 3 && strings.EqualFold(rest[1], "AS") {
				stages[strings.ToLower(rest[2])] = image
			}
		}
	}
	image := last
	if target != "" {
		image = stages[strings.ToLower(target)]
	}
	if image == "" || strings.EqualFold(image, "scratch") || strings.Contains(image, "$") {
		return ""
	}
	return image
}

// dockerfileInstructions joins continuation lines and drops comments.
func dockerfileInstructions(text string) []string {
	var out []string
	var current strings.Builder
	scanner := bufio.NewScanner(strings.NewReader(text))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasSuffix(line, "\\") {
			current.WriteString(strings.TrimSuffix(line, "\\"))
			current.WriteString(" ")
			continue
		}
		current.WriteString(line)
		if s := strings.TrimSpace(current.String()); s != "" {
			out = append(out, s)
		}
		current.Reset()
	}
	return out
}

// varRef matches $VAR, ${VAR}, ${VAR:-default} and ${VAR-default}.
var varRef = regexp.MustCompile(`\$(?:\{([A-Za-z_][A-Za-z0-9_]*)(?::?-([^}]*))?\}|([A-Za-z_][A-Za-z0-9_]*))`)

// expandArgs substitutes variable references from values. ${VAR:-def} and
// ${VAR-def} fall back to def when VAR is unset or empty; an unknown
// variable without a default is left as is (the caller then treats the
// image as unknown).
func expandArgs(s string, values map[string]string) string {
	return varRef.ReplaceAllStringFunc(s, func(ref string) string {
		m := varRef.FindStringSubmatch(ref)
		name, def := m[1], m[2]
		hasDefault := name != "" && strings.Contains(ref, "-")
		if name == "" {
			name = m[3]
		}
		v, ok := values[name]
		switch {
		case ok && v != "":
			return v
		case hasDefault:
			return def
		case ok:
			return v
		}
		return ref
	})
}
