package container

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"

	"github.com/griffithind/dcx/internal/state"
)

// ComposeProject is a docker compose project together with the files that
// describe it. Every compose command dcx runs goes through it, always with
// explicit -f files, so compose never picks up a default-named file from
// whatever directory it happens to run in.
type ComposeProject struct {
	Name  string
	Dir   string   // working directory; relative -f paths resolve against it
	Files []string // -f files, in order (dcx's override last)
}

// ExistingComposeProject returns the project an existing compose
// environment was created with. It uses the -f files and project directory
// compose recorded on the container (which include dcx's persistent
// override), skipping files that no longer exist, such as one-off build
// secret overrides.
//
// When none of the recorded files exist, it runs from an empty directory
// with only -p, so compose acts on the project's containers by label and
// cannot load an unrelated compose file.
func ExistingComposeProject(info *state.ContainerInfo, defaultProject, defaultDir string) ComposeProject {
	p := ComposeProject{
		Name: info.GetComposeProject(defaultProject),
		Dir:  info.GetConfigDir(defaultDir),
	}
	if info == nil || info.Labels == nil {
		return p
	}
	if dir := info.Labels.ComposeWorkingDir; dir != "" && isDir(dir) {
		p.Dir = dir
	}
	for _, f := range info.Labels.ComposeConfigFiles {
		if !filepath.IsAbs(f) {
			f = filepath.Join(p.Dir, f)
		}
		if _, err := os.Stat(f); err == nil {
			p.Files = append(p.Files, f)
		}
	}
	if len(p.Files) == 0 {
		p.Dir = os.TempDir()
	}
	return p
}

// args returns the global compose arguments followed by extra.
func (p ComposeProject) args(extra ...string) []string {
	args := []string{"compose"}
	if p.Name != "" {
		args = append(args, "-p", p.Name)
	}
	for _, f := range p.Files {
		args = append(args, "-f", f)
	}
	return append(args, extra...)
}

// command returns the docker compose command for extra, run from p.Dir.
func (p ComposeProject) command(ctx context.Context, extra ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, "docker", p.args(extra...)...)
	cmd.Dir = p.Dir
	return cmd
}

// run runs a compose command, streaming its output.
func (p ComposeProject) run(ctx context.Context, extra ...string) error {
	cmd := p.command(ctx, extra...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	return cmd.Run()
}

// Start starts the project's existing, stopped services.
func (p ComposeProject) Start(ctx context.Context) error {
	return p.run(ctx, "start")
}

// Stop stops the project's services without removing them.
func (p ComposeProject) Stop(ctx context.Context) error {
	return p.run(ctx, "stop")
}

// Down removes the project's containers and networks (and, if requested,
// its volumes and orphan containers).
func (p ComposeProject) Down(ctx context.Context, opts DownOptions) error {
	args := []string{"down"}
	if opts.RemoveVolumes {
		args = append(args, "-v")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	return p.run(ctx, args...)
}

// with returns a copy of p with extra files appended.
func (p ComposeProject) with(files ...string) ComposeProject {
	p.Files = append(append([]string(nil), p.Files...), files...)
	return p
}

func isDir(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}
