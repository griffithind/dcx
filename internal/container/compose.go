package container

import (
	"context"
	"os"
	"path/filepath"

	"github.com/griffithind/dcx/internal/state"
)

// ComposeProject is a docker compose project together with the files that
// describe it. Every compose command dcx runs is built from one (and run
// by Engine.Compose), always with explicit -f files, so compose never picks
// up a default-named file from whatever directory it happens to run in.
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
		Name: info.ComposeProjectOr(defaultProject),
		Dir:  info.ConfigDirOr(defaultDir),
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

// args returns the docker compose arguments for p followed by extra.
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

// Start starts the project's existing, stopped services.
//
// Like devcontainers/cli it runs `up -d --no-recreate` (for services, or
// all of them): stopped containers start as they are, and a service whose
// container is missing is created.
func (p ComposeProject) Start(ctx context.Context, e Engine, services []string) error {
	return e.Compose(ctx, p, append([]string{"up", "-d", "--no-recreate"}, services...)...)
}

// Stop stops the project's services without removing them.
func (p ComposeProject) Stop(ctx context.Context, e Engine) error {
	return e.Compose(ctx, p, "stop")
}

// Down removes the project's containers and networks (and, if requested,
// its volumes and orphan containers).
func (p ComposeProject) Down(ctx context.Context, e Engine, opts DownOptions) error {
	args := []string{"down"}
	if opts.RemoveVolumes {
		args = append(args, "-v")
	}
	if opts.RemoveOrphans {
		args = append(args, "--remove-orphans")
	}
	return e.Compose(ctx, p, args...)
}

// ForRemoval returns p without the workspace's dcx override file, for
// `down`. The override was written for the config the environment was
// created from: after the user renames or removes the primary service it
// describes a service the compose files no longer define, and compose
// rejects the whole project. It also keeps `down -v` from removing the
// named volumes of devcontainer.json mounts, which `docker rm -v` never
// removes for single containers either.
func (p ComposeProject) ForRemoval(workspaceID string) ComposeProject {
	override, err := ComposeOverridePath(workspaceID)
	if err != nil || workspaceID == "" {
		return p
	}
	files := make([]string, 0, len(p.Files))
	for _, f := range p.Files {
		if filepath.Clean(f) != override {
			files = append(files, f)
		}
	}
	p.Files = files
	if len(files) == 0 {
		p.Dir = os.TempDir()
	}
	return p
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
