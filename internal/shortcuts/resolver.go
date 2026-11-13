// Package shortcuts provides command shortcut resolution for dcx.
package shortcuts

import (
	"sort"

	"github.com/griffithind/dcx/internal/devcontainer"
	"github.com/griffithind/dcx/internal/shell"
)

// ResolvedCommand is a shortcut expanded into the command line to run in
// the container.
type ResolvedCommand struct {
	// CommandLine is a shell command line for the remote shell.
	CommandLine string

	// Found is false when args[0] is not a shortcut.
	Found bool
}

// Resolve expands args[0] when it names a shortcut.
//
// A shortcut's command or prefix is shell text, run with `sh -c`, so
// pipes, `&&`, quotes and variables in it work. Remaining arguments are
// passed to it as positional parameters ("$@") rather than pasted into
// the text, so they keep their exact value whatever characters they
// contain. Command shortcuts always receive the remaining arguments;
// prefix shortcuts only with passArgs.
func Resolve(shortcuts map[string]devcontainer.Shortcut, args []string) ResolvedCommand {
	if len(args) == 0 || len(shortcuts) == 0 {
		return ResolvedCommand{}
	}
	sc, ok := shortcuts[args[0]]
	if !ok {
		return ResolvedCommand{}
	}
	rest := args[1:]

	switch {
	case sc.Command != "":
		return ResolvedCommand{CommandLine: shellCommand(sc.Command, rest), Found: true}
	case sc.Prefix != "":
		if !sc.PassArgs {
			rest = nil
		}
		return ResolvedCommand{CommandLine: shellCommand(sc.Prefix, rest), Found: true}
	}
	return ResolvedCommand{}
}

// shellCommand builds `sh -c '<script> "$@"' dcx-run <args…>`, quoted for
// the remote shell.
func shellCommand(script string, args []string) string {
	if len(args) == 0 {
		return shell.Join([]string{"sh", "-c", script})
	}
	return shell.Join(append([]string{"sh", "-c", script + ` "$@"`, "dcx-run"}, args...))
}

// ShortcutInfo contains display information about a shortcut.
type ShortcutInfo struct {
	// Name is the shortcut name (e.g., "rw", "r").
	Name string

	// Expansion shows what the shortcut expands to.
	Expansion string

	// Description is optional help text.
	Description string
}

// ListShortcuts returns a sorted list of shortcut information for display.
func ListShortcuts(shortcuts map[string]devcontainer.Shortcut) []ShortcutInfo {
	if len(shortcuts) == 0 {
		return nil
	}

	result := make([]ShortcutInfo, 0, len(shortcuts))

	for name, sc := range shortcuts {
		info := ShortcutInfo{
			Name:        name,
			Description: sc.Description,
		}

		if sc.Command != "" {
			info.Expansion = sc.Command
		} else if sc.Prefix != "" {
			info.Expansion = sc.Prefix
			if sc.PassArgs {
				info.Expansion += " [args...]"
			}
		}

		result = append(result, info)
	}

	// Sort alphabetically by name
	sort.Slice(result, func(i, j int) bool {
		return result[i].Name < result[j].Name
	})

	return result
}
