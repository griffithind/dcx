package devcontainer

import (
	"bytes"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
	"strings"

	"github.com/tidwall/jsonc"
)

// A config is read as a generic JSON tree first (parseTree), so that
// variable substitution can run over every string in it once, as the
// reference implementation does, before the tree is decoded into the typed
// DevContainerConfig. The raw (unsubstituted) and substituted configs are
// both decoded from the same tree.

// utf8BOM is stripped before parsing: editors on Windows write it.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// parseTree parses JSONC (comments, trailing commas, a UTF-8 BOM) into a
// generic value, keeping numbers exact.
func parseTree(data []byte) (any, error) {
	data = bytes.TrimPrefix(data, utf8BOM)
	dec := json.NewDecoder(bytes.NewReader(jsonc.ToJSON(data)))
	dec.UseNumber()
	var tree any
	if err := dec.Decode(&tree); err != nil {
		return nil, err
	}
	return tree, nil
}

// decodeTree decodes a generic value into v.
func decodeTree(tree any, v any) error {
	data, err := json.Marshal(tree)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, v)
}

// dropMiscasedKeys removes, from obj and from the objects at the nested
// keys, properties whose name matches one of t's JSON field names only when
// case is ignored ("Image", "dockerfile" for "dockerFile"): JSON property
// names are case-sensitive, but Go's decoder would honour them. It returns
// a warning per dropped property.
func dropMiscasedKeys(obj map[string]any, t reflect.Type, where string) []string {
	exact := map[string]bool{}
	nested := map[string]reflect.Type{}
	for i := 0; i < t.NumField(); i++ {
		f := t.Field(i)
		name, _, _ := strings.Cut(f.Tag.Get("json"), ",")
		if name == "" || name == "-" {
			continue
		}
		exact[name] = true
		ft := f.Type
		for ft.Kind() == reflect.Pointer {
			ft = ft.Elem()
		}
		if ft.Kind() == reflect.Struct && ft.PkgPath() == t.PkgPath() {
			nested[name] = ft
		}
	}
	var warnings []string
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		if exact[k] {
			if nt, ok := nested[k]; ok {
				if sub, ok := obj[k].(map[string]any); ok {
					warnings = append(warnings, dropMiscasedKeys(sub, nt, where+k+".")...)
				}
			}
			continue
		}
		for name := range exact {
			if strings.EqualFold(name, k) {
				delete(obj, k)
				warnings = append(warnings, fmt.Sprintf("ignoring %q: property names are case-sensitive (did you mean %q?)", where+k, where+name))
				break
			}
		}
	}
	return warnings
}

// varRefPattern matches ${...} references, non-greedily, like the reference
// implementation's /\$\{(.*?)\}/g.
var varRefPattern = regexp.MustCompile(`\$\{(.*?)\}`)

// substituteTree returns a copy of tree with every string substituted.
func substituteTree(tree any, ctx *SubstitutionContext) any {
	switch v := tree.(type) {
	case string:
		return Substitute(v, ctx)
	case []any:
		out := make([]any, len(v))
		for i := range v {
			out[i] = substituteTree(v[i], ctx)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(v))
		for k, val := range v {
			out[k] = substituteTree(val, ctx)
		}
		return out
	default:
		return v
	}
}

// Substitute replaces the variable references in s in a single pass (a
// substituted value is not expanded again). Unknown references, and
// ${containerEnv:...} (resolved at exec time, see ResolveContainerEnvRefs),
// are left as they are.
func Substitute(s string, ctx *SubstitutionContext) string {
	if !strings.Contains(s, "${") {
		return s
	}
	return varRefPattern.ReplaceAllStringFunc(s, func(match string) string {
		inner := match[2 : len(match)-1]
		if value, ok := ctx.lookup(inner); ok {
			return value
		}
		return match
	})
}

// lookup resolves the inside of a ${...} reference.
func (ctx *SubstitutionContext) lookup(ref string) (string, bool) {
	if ctx == nil {
		return "", false
	}
	name, args, _ := strings.Cut(ref, ":")
	switch name {
	case "localEnv", "env":
		// ${localEnv:VAR} or ${localEnv:VAR:default}. The default applies
		// when the variable is unset (an empty value is kept, as the
		// reference implementation does); it may contain colons.
		varName, def, _ := strings.Cut(args, ":")
		if varName == "" {
			return "", false
		}
		if value, ok := ctx.lookupEnv(varName); ok {
			return value, true
		}
		return def, true
	case "localWorkspaceFolder":
		return ctx.LocalWorkspaceFolder, ctx.LocalWorkspaceFolder != ""
	case "localWorkspaceFolderBasename":
		return filepath.Base(ctx.LocalWorkspaceFolder), ctx.LocalWorkspaceFolder != ""
	case "containerWorkspaceFolder":
		return ctx.ContainerWorkspaceFolder, ctx.ContainerWorkspaceFolder != ""
	case "containerWorkspaceFolderBasename":
		if ctx.ContainerWorkspaceFolder == "" {
			return "", false
		}
		base := path.Base(ctx.ContainerWorkspaceFolder)
		if base == "/" {
			base = "" // the compose default "/" has no basename
		}
		return base, true
	case "devcontainerId":
		return ctx.DevcontainerID, ctx.DevcontainerID != ""
	case "userHome":
		return ctx.UserHome, ctx.UserHome != ""
	case "pathSeparator":
		return string(filepath.Separator), true
	}
	return "", false
}

func (ctx *SubstitutionContext) lookupEnv(name string) (string, bool) {
	if ctx.LookupEnv != nil {
		return ctx.LookupEnv(name)
	}
	return "", false
}
