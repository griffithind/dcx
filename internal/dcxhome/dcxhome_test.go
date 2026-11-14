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
