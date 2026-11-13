// Package lifecyclecmd is the lifecycle command type shared by
// devcontainer.json, image metadata and feature metadata
// (onCreateCommand, postStartCommand, …), and its only parser.
package lifecyclecmd

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
)

// Command is a lifecycle command in one of the spec's three forms:
//
//	"npm install"                  a shell command (sh -c)
//	["npm", "install"]             an argv, executed directly
//	{"a": "cmd", "b": ["x", "y"]}  named commands, run in parallel
//
// Config fields hold a *Command; nil means the property is absent. A
// Command marshals back to the form it was written in, so the config hash
// (the staleness key) and image metadata labels are unchanged by typing.
type Command struct {
	// Shell is the string form.
	Shell string
	// Argv is the array form (non-nil, possibly empty).
	Argv []string
	// Named is the object form; each value is a string or array Command.
	Named map[string]Command
}

// Step is one command to run: Shell (via sh -c) or Argv. Name is set for
// the named (parallel) form.
type Step struct {
	Name  string
	Shell string
	Argv  []string
}

// UnmarshalJSON accepts a string, an array of strings or an object whose
// values are strings or arrays. Non-string array items and other value
// types are ignored, as they always were.
func (c *Command) UnmarshalJSON(data []byte) error {
	var raw interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	cmd, ok := fromValue(raw, true)
	if !ok {
		return fmt.Errorf("lifecycle command: expected a string, array or object, got %s", data)
	}
	*c = cmd
	return nil
}

func fromValue(v interface{}, allowNamed bool) (Command, bool) {
	switch t := v.(type) {
	case string:
		return Command{Shell: t}, true
	case []interface{}:
		argv := make([]string, 0, len(t))
		for _, item := range t {
			if s, ok := item.(string); ok {
				argv = append(argv, s)
			}
		}
		return Command{Argv: argv}, true
	case map[string]interface{}:
		if !allowNamed {
			return Command{}, false
		}
		named := make(map[string]Command, len(t))
		for name, value := range t {
			if cmd, ok := fromValue(value, false); ok {
				named[name] = cmd
			}
		}
		return Command{Named: named}, true
	}
	return Command{}, false
}

// MarshalJSON writes the command in the form it was read in.
func (c Command) MarshalJSON() ([]byte, error) {
	switch {
	case c.Named != nil:
		return json.Marshal(c.Named) // map keys are sorted
	case c.Argv != nil:
		return json.Marshal(c.Argv)
	default:
		return json.Marshal(c.Shell)
	}
}

// Steps returns the commands to run, skipping empty ones. Named commands
// are sorted by name so output is deterministic.
func (c *Command) Steps() []Step {
	if c == nil {
		return nil
	}
	if c.Named == nil {
		if s, ok := c.step(""); ok {
			return []Step{s}
		}
		return nil
	}
	names := make([]string, 0, len(c.Named))
	for name := range c.Named {
		names = append(names, name)
	}
	sort.Strings(names)
	var steps []Step
	for _, name := range names {
		sub := c.Named[name]
		if s, ok := sub.step(name); ok {
			steps = append(steps, s)
		}
	}
	return steps
}

func (c *Command) step(name string) (Step, bool) {
	if c.Argv != nil {
		return Step{Name: name, Argv: c.Argv}, len(c.Argv) > 0
	}
	return Step{Name: name, Shell: c.Shell}, strings.TrimSpace(c.Shell) != ""
}

// Parallel reports whether the steps run in parallel (the named form).
func (c *Command) Parallel() bool { return c != nil && c.Named != nil }

// Substitute returns a copy of c with sub applied to every string.
func (c *Command) Substitute(sub func(string) string) *Command {
	if c == nil {
		return nil
	}
	out := Command{Shell: sub(c.Shell)}
	if c.Argv != nil {
		out.Argv = make([]string, len(c.Argv))
		for i, a := range c.Argv {
			out.Argv[i] = sub(a)
		}
	}
	if c.Named != nil {
		out.Named = make(map[string]Command, len(c.Named))
		for name, cmd := range c.Named {
			out.Named[name] = *cmd.Substitute(sub)
		}
	}
	return &out
}

// String renders a step for display.
func (s Step) String() string {
	text := s.Shell
	if s.Argv != nil {
		text = strings.Join(s.Argv, " ")
	}
	if s.Name != "" {
		return "[" + s.Name + "] " + text
	}
	return text
}
