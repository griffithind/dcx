package devcontainer

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// metadataImage returns the image whose devcontainer.metadata applies to the
// config: the image of an image plan, the base (FROM) image of a Dockerfile
// plan's target stage, and for compose the primary service's image or the
// base image of its build. "" when it can't be determined (a FROM built from
// an unknown variable, scratch). It reads files only; resolving it needs no
// Docker.
func metadataImage(cfg *DevContainerConfig, configPath string) string {
	switch cfg.PlanType() {
	case PlanTypeImage:
		return cfg.Image
	case PlanTypeDockerfile:
		plan := buildPlan(cfg, configPath, "").(*DockerfilePlan)
		return dockerfileBaseImage(plan.Dockerfile, cfg.Build.Target, cfg.Build.Args)
	case PlanTypeCompose:
		return composeServiceBaseImage(composeFilePaths(cfg, configPath), cfg.Service)
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

// composeServiceBaseImage returns the primary service's image, or the base
// image of its build, from the compose files (later files override earlier
// ones). Compose's ${VAR} interpolation uses the environment.
func composeServiceBaseImage(files []string, service string) string {
	var image string
	var build struct {
		context, dockerfile, target string
		args                        map[string]string
		set                         bool
	}
	env := environMap()
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			continue
		}
		var doc struct {
			Services map[string]struct {
				Image string    `yaml:"image"`
				Build yaml.Node `yaml:"build"`
			} `yaml:"services"`
		}
		if yaml.Unmarshal(data, &doc) != nil {
			continue
		}
		svc, ok := doc.Services[service]
		if !ok {
			continue
		}
		if svc.Image != "" {
			image = expandArgs(svc.Image, env)
		}
		switch svc.Build.Kind {
		case yaml.ScalarNode:
			build.set, build.context, build.dockerfile = true, resolveFrom(file, expandArgs(svc.Build.Value, env)), ""
		case yaml.MappingNode:
			var b struct {
				Context    string    `yaml:"context"`
				Dockerfile string    `yaml:"dockerfile"`
				Target     string    `yaml:"target"`
				Args       yaml.Node `yaml:"args"`
			}
			if svc.Build.Decode(&b) != nil {
				continue
			}
			build.set = true
			if b.Context != "" || build.context == "" {
				build.context = resolveFrom(file, expandArgs(b.Context, env))
			}
			if b.Dockerfile != "" {
				build.dockerfile = expandArgs(b.Dockerfile, env)
			}
			if b.Target != "" {
				build.target = b.Target
			}
			build.args = composeArgs(b.Args, env)
		}
	}
	if !build.set {
		return image
	}
	dockerfile := build.dockerfile
	if dockerfile == "" {
		dockerfile = "Dockerfile"
	}
	if !filepath.IsAbs(dockerfile) {
		dockerfile = filepath.Join(build.context, dockerfile)
	}
	return dockerfileBaseImage(dockerfile, build.target, build.args)
}

// composeArgs reads build args given as a map or a KEY=VALUE list.
func composeArgs(node yaml.Node, env map[string]string) map[string]string {
	args := map[string]string{}
	switch node.Kind {
	case yaml.MappingNode:
		var m map[string]string
		if node.Decode(&m) == nil {
			for k, v := range m {
				args[k] = expandArgs(v, env)
			}
		}
	case yaml.SequenceNode:
		var list []string
		if node.Decode(&list) == nil {
			for _, kv := range list {
				if k, v, ok := strings.Cut(kv, "="); ok {
					args[k] = expandArgs(v, env)
				}
			}
		}
	}
	return args
}

// resolveFrom resolves a compose path relative to the compose file.
func resolveFrom(composeFile, p string) string {
	if p == "" {
		p = "."
	}
	if filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(filepath.Dir(composeFile), p)
}

func environMap() map[string]string {
	env := map[string]string{}
	for _, kv := range os.Environ() {
		if k, v, ok := strings.Cut(kv, "="); ok {
			env[k] = v
		}
	}
	return env
}
