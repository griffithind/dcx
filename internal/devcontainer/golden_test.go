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
	Name            string            `json:"name"`
	Plan            any               `json:"plan"`
	BaseImage       string            `json:"baseImage,omitempty"`
	ServiceName     string            `json:"serviceName"`
	WorkspaceFolder string            `json:"workspaceFolder"`
	WorkspaceMount  string            `json:"workspaceMount,omitempty"`
	RemoteUser      string            `json:"remoteUser,omitempty"`
	ContainerUser   string            `json:"containerUser,omitempty"`
	EffectiveUser   string            `json:"effectiveUser,omitempty"`
	ContainerEnv    map[string]string `json:"containerEnv,omitempty"`
	RemoteEnv       map[string]string `json:"remoteEnv,omitempty"`
	Mounts          []Mount           `json:"mounts,omitempty"`
	CapAdd          []string          `json:"capAdd,omitempty"`
	SecurityOpt     []string          `json:"securityOpt,omitempty"`
	Privileged      bool              `json:"privileged,omitempty"`
	Init            bool              `json:"init,omitempty"`
	RunArgs         *ParsedRunArgs    `json:"runArgs,omitempty"`
	ForwardPorts    []PortForward     `json:"forwardPorts,omitempty"`
	AppPorts        []PortForward     `json:"appPorts,omitempty"`
	Features        []string          `json:"features,omitempty"`
}

func planView(p ExecutionPlan) any {
	switch v := p.(type) {
	case *ImagePlan:
		return map[string]any{"type": "image", "image": v.Image}
	case *DockerfilePlan:
		return map[string]any{"type": "dockerfile", "dockerfile": v.Dockerfile, "context": v.Context, "args": v.Args, "target": v.Target}
	case *ComposePlan:
		return map[string]any{"type": "compose", "files": v.Files, "service": v.Service, "project": v.ProjectName}
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

// TestResolveGolden runs the real Load + Builder.Build pipeline over every
// case in testdata/resolve and compares a normalized view with the case's
// expected.golden.json. Run with -update to rewrite the golden files.
//
// A case directory contains:
//
//	workspace/     the workspace root (with .devcontainer/...)
//	env            optional KEY=VALUE lines exported for the run
//	config-path    optional --config value (relative to the workspace)
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

			cfg, cfgPath, err := Load(ws, configPath)
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			resolved, err := NewBuilder(nil, nil).Build(context.Background(), BuilderOptions{
				ConfigPath:    cfgPath,
				WorkspaceRoot: ws,
				Config:        cfg,
			})
			if err != nil {
				t.Fatalf("Build: %v", err)
			}

			view := goldenView{
				Name: resolved.Name, Plan: planView(resolved.Plan), BaseImage: resolved.BaseImage,
				ServiceName: resolved.ServiceName, WorkspaceFolder: resolved.WorkspaceFolder,
				WorkspaceMount: resolved.WorkspaceMount, RemoteUser: resolved.RemoteUser,
				ContainerUser: resolved.ContainerUser, EffectiveUser: resolved.EffectiveUser,
				ContainerEnv: resolved.ContainerEnv, RemoteEnv: resolved.RemoteEnv,
				Mounts: resolved.Mounts, CapAdd: resolved.CapAdd, SecurityOpt: resolved.SecurityOpt,
				Privileged: resolved.Privileged, Init: resolved.Init, RunArgs: resolved.RunArgs,
				ForwardPorts: resolved.ForwardPorts, AppPorts: resolved.AppPorts,
			}
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
			out = strings.ReplaceAll(out, resolved.ID, "<id>")
			out = strings.ReplaceAll(out, resolved.ID[:resourceIDLength], "<id8>")
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
