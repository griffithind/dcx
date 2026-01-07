// Package shortcuts provides command shortcut resolution for dcx.
package shortcuts

import (
	"sort"
	"strings"

	"github.com/griffithind/dcx/internal/config"
)

// ResolvedCommand represents a resolved shortcut command.
type ResolvedCommand struct {
	// Command is the full command to execute as separate arguments.
	Command []string

	// Found indicates whether a shortcut was matched.
	Found bool
}

// Resolve resolves a command against configured shortcuts.
// Returns the resolved command or the original if no shortcut matches.
//
// Examples:
//   - shortcuts["rw"] = "bin/jobs --skip-recurring"
//     Resolve(shortcuts, ["rw"]) -> ["bin/jobs", "--skip-recurring"]
//
//   - shortcuts["r"] = {prefix: "rails", passArgs: true}
//     Resolve(shortcuts, ["r", "console"]) -> ["rails", "console"]
//     Resolve(shortcuts, ["r", "server", "-p", "3001"]) -> ["rails", "server", "-p", "3001"]
func Resolve(shortcuts map[string]config.Shortcut, args []string) ResolvedCommand {
	if len(args) == 0 || len(shortcuts) == 0 {
		return ResolvedCommand{Command: args, Found: false}
	}

	shortcutName := args[0]
	shortcut, exists := shortcuts[shortcutName]
	if !exists {
		return ResolvedCommand{Command: args, Found: false}
	}

	remainingArgs := args[1:]

	// Simple command shortcut - split the command and append remaining args
	if shortcut.Command != "" {
		cmdParts := strings.Fields(shortcut.Command)
		return ResolvedCommand{
			Command: append(cmdParts, remainingArgs...),
			Found:   true,
		}
	}

	// Prefix with passthrough
	if shortcut.Prefix != "" {
		prefixParts := strings.Fields(shortcut.Prefix)
		if shortcut.PassArgs {
			return ResolvedCommand{
				Command: append(prefixParts, remainingArgs...),
				Found:   true,
			}
		}
		// No passArgs - just return the prefix
		return ResolvedCommand{
			Command: prefixParts,
			Found:   true,
		}
	}

	// No valid shortcut definition
	return ResolvedCommand{Command: args, Found: false}
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
func ListShortcuts(shortcuts map[string]config.Shortcut) []ShortcutInfo {
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
