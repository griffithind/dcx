package devcontainer

import (
	"context"
	"encoding/json"
	"flag"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

var updateGolden = flag.Bool("update", false, "rewrite resolve golden files")

// goldenView is the deterministic projection of a ResolvedDevContainer that
// golden files record. Host-specific values are replaced by placeholders.
type goldenView struct {
	Name            string              `json:"name"`
	Plan            any                 `json:"plan"`
	BaseImage       string              `json:"baseImage,omitempty"`
	MetadataImage   string              `json:"metadataImage,omitempty"`
	ResourceName    string              `json:"resourceName"`
	SSHHost         string              `json:"sshHost"`
	OverrideCommand bool                `json:"overrideCommand"`
	WorkspaceFolder string              `json:"workspaceFolder"`
	WorkspaceMount  *Mount              `json:"workspaceMount,omitempty"`
	RemoteUser      string              `json:"remoteUser,omitempty"`
	ContainerUser   string              `json:"containerUser,omitempty"`
	EffectiveUser   string              `json:"effectiveUser,omitempty"`
	ContainerEnv    map[string]string   `json:"containerEnv,omitempty"`
	RemoteEnv       map[string]string   `json:"remoteEnv,omitempty"`
	Mounts          []Mount             `json:"mounts,omitempty"`
	CapAdd          []string            `json:"capAdd,omitempty"`
	SecurityOpt     []string            `json:"securityOpt,omitempty"`
	Privileged      bool                `json:"privileged,omitempty"`
	Init            bool                `json:"init,omitempty"`
	RunArgs         *ParsedRunArgs      `json:"runArgs,omitempty"`
	ForwardPorts    []PortForward       `json:"forwardPorts,omitempty"`
	AppPorts        []PortForward       `json:"appPorts,omitempty"`
	Features        []string            `json:"features,omitempty"`
	Hooks           map[string][]string `json:"hooks,omitempty"`
}

// hooksView lists each phase's hooks as "<source>: <step>".
func hooksView(h Hooks) map[string][]string {
	out := map[string][]string{}
	for phase, hooks := range map[string][]Hook{
		"initialize": h.Initialize, "onCreate": h.OnCreate, "updateContent": h.UpdateContent,
		"postCreate": h.PostCreate, "postStart": h.PostStart, "postAttach": h.PostAttach,
	} {
		for _, hook := range hooks {
			src := string(hook.Source)
			if hook.Feature != "" {
				src += " " + hook.Feature
			}
			for _, step := range hook.Command.Steps() {
				out[phase] = append(out[phase], src+": "+step.String())
			}
		}
	}
	if len(out) == 0 {
		return nil
	}
	return out
}

func planView(p ExecutionPlan) any {
	switch v := p.(type) {
	case *ImagePlan:
		return map[string]any{"type": "image", "image": v.Image}
	case *DockerfilePlan:
		return map[string]any{"type": "dockerfile", "dockerfile": v.Dockerfile, "context": v.Context, "args": v.Args, "target": v.Target, "cacheFrom": v.CacheFrom, "options": v.Options}
	case *ComposePlan:
		return map[string]any{"type": "compose", "files": v.Files, "service": v.Service, "runServices": v.RunServices, "project": v.ProjectName}
	}
	return nil
}

func copyTree(t *testing.T, src, dst string) {
	t.Helper()
	err := filepath.WalkDir(src, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, path)
		target := filepath.Join(dst, rel)
		if d.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		return os.WriteFile(target, data, 0o644)
	})
	if err != nil {
		t.Fatalf("copy %s: %v", src, err)
	}
}

// goldenImages serves a case's image-metadata.json (the devcontainer.metadata
// label of every image) when the case has one.
type goldenImages string

func (g goldenImages) ImageMetadata(_ context.Context, _ string) (string, error) {
	data, err := os.ReadFile(filepath.Join(string(g), "image-metadata.json"))
	if os.IsNotExist(err) {
		return "", nil
	}
	return string(data), err
}

// goldenCompose serves a case's compose-config.json: `docker compose config
// --format json` recorded for the case's workspace, with its path written
// as <workspace>.
type goldenCompose struct{ caseDir, workspace string }

func (g goldenCompose) ComposeConfig(context.Context, string, string, []string) ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(g.caseDir, "compose-config.json"))
	return []byte(strings.ReplaceAll(string(data), "<workspace>", g.workspace)), err
}

// TestResolveGolden runs ResolveWorkspace, the production pipeline, over
// every case in testdata/resolve and compares a normalized view with the case's
// expected.golden.json. Run with -update to rewrite the golden files.
//
// A case directory contains:
//
//	workspace/     the workspace root (with .devcontainer/...)
//	env            optional KEY=VALUE lines exported for the run
//	config-path    optional --config value (relative to the workspace)
//	image-metadata.json  optional devcontainer.metadata label of the image
//	compose-config.json  the compose model of a compose case (recorded
//	                     with docker compose config, see goldenCompose)
func TestResolveGolden(t *testing.T) {
	cases, err := os.ReadDir("testdata/resolve")
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range cases {
		if !c.IsDir() {
			continue
		}
		name := c.Name()
		t.Run(name, func(t *testing.T) {
			caseDir := filepath.Join("testdata/resolve", name)
			ws := filepath.Join(t.TempDir(), "proj")
			copyTree(t, filepath.Join(caseDir, "workspace"), ws)
			home := t.TempDir()
			t.Setenv("HOME", home)

			if data, err := os.ReadFile(filepath.Join(caseDir, "env")); err == nil {
				for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
					if k, v, ok := strings.Cut(line, "="); ok {
						t.Setenv(k, v)
					}
				}
			}
			configPath := ""
			if data, err := os.ReadFile(filepath.Join(caseDir, "config-path")); err == nil {
				configPath = strings.TrimSpace(string(data))
			}

			resolved, err := ResolveWorkspace(context.Background(), ResolveOptions{
				WorkspacePath: ws,
				ConfigPath:    configPath,
				Images:        goldenImages(caseDir),
				Compose:       goldenCompose{caseDir, ws},
				Lockfile:      LockfileUse,
			})
			if err != nil {
				t.Fatalf("ResolveWorkspace: %v", err)
			}
			// Commands that don't resolve (stop, down, ssh) must agree with
			// resolution on the workspace's identity.
			if ids := LoadIdentity(ws, configPath); ids != resolved.Identity {
				t.Errorf("LoadIdentity = %+v, resolution has %+v", ids, resolved.Identity)
			}

			view := goldenView{
				Name: resolved.Name, Plan: planView(resolved.Plan), BaseImage: resolved.BaseImage, MetadataImage: resolved.MetadataImage,
				ResourceName: resolved.ResourceName, SSHHost: resolved.SSHHost, OverrideCommand: resolved.OverrideCommand, WorkspaceFolder: resolved.WorkspaceFolder,
				WorkspaceMount: resolved.WorkspaceMount, RemoteUser: resolved.RemoteUser,
				ContainerUser: resolved.ContainerUser, EffectiveUser: resolved.EffectiveUser,
				ContainerEnv: resolved.ContainerEnv, RemoteEnv: resolved.RemoteEnv,
				Mounts: resolved.Mounts, CapAdd: resolved.CapAdd, SecurityOpt: resolved.SecurityOpt,
				Privileged: resolved.Privileged, Init: resolved.Init, RunArgs: resolved.RunArgs,
				ForwardPorts: resolved.ForwardPorts, AppPorts: resolved.AppPorts,
			}
			view.Hooks = hooksView(resolved.Hooks)
			for _, f := range resolved.Features {
				view.Features = append(view.Features, f.ID)
			}
			sort.Strings(view.Features)

			got, err := json.MarshalIndent(view, "", "  ")
			if err != nil {
				t.Fatal(err)
			}
			out := string(got)
			out = strings.ReplaceAll(out, ws, "<workspace>")
			out = strings.ReplaceAll(out, home, "<home>")
			out = strings.ReplaceAll(out, resolved.WorkspaceID, "<id>")
			out = strings.ReplaceAll(out, resolved.WorkspaceID[:resourceIDLength], "<id8>")
			out += "\n"

			goldenPath := filepath.Join(caseDir, "expected.golden.json")
			if *updateGolden {
				if err := os.WriteFile(goldenPath, []byte(out), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(goldenPath)
			if err != nil {
				t.Fatalf("missing golden file (run with -update): %v", err)
			}
			if string(want) != out {
				t.Errorf("resolved view differs from %s\n--- want\n%s\n--- got\n%s", goldenPath, want, out)
			}
		})
	}
}
