package dcxhome

import (
	"path/filepath"
	"testing"
)

func TestDir(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv(Env, "")
	if got, _ := Dir(); got != filepath.Join(home, ".dcx") {
		t.Errorf("Dir() = %q, want ~/.dcx", got)
	}
	t.Setenv(Env, "/tmp/dcx-state")
	if got, _ := Path("locks", "x.lock"); got != "/tmp/dcx-state/locks/x.lock" {
		t.Errorf("Path() = %q, want under $DCX_HOME", got)
	}
}

func TestWorkspaceFolder(t *testing.T) {
	t.Setenv(Env, t.TempDir())
	for _, bad := range []string{"", "..", "a/b", `a\b`} {
		if _, err := WorkspaceDir(bad); err == nil {
			t.Errorf("WorkspaceDir(%q) must fail", bad)
		}
	}
	if _, ok := ReadWorkspace("ws1"); ok {
		t.Error("no record yet")
	}
	want := Workspace{Path: "/home/u/proj", Config: "/home/u/proj/.devcontainer/devcontainer.json", Name: "proj"}
	if err := WriteWorkspace("ws1", want); err != nil {
		t.Fatal(err)
	}
	if got, ok := ReadWorkspace("ws1"); !ok || got != want {
		t.Errorf("ReadWorkspace = %+v, %v", got, ok)
	}
	if ids, err := WorkspaceIDs(); err != nil || len(ids) != 1 || ids[0] != "ws1" {
		t.Errorf("WorkspaceIDs = %v, %v", ids, err)
	}
}
