package dcxhome

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// WorkspacesDir holds one folder per workspace (WorkspaceDir).
const WorkspacesDir = "workspaces"

// Files in a workspace's folder.
const (
	// HostKeyFile is the workspace's SSH host key.
	HostKeyFile = "host_key"
	// LockFile is the workspace's lock (dcx commands on it run one at a
	// time).
	LockFile = "lock"
	// ComposeOverrideFile is the compose override of a compose workspace.
	ComposeOverrideFile = "docker-compose.dcx.yml"
	// RecordFile is the Workspace record.
	RecordFile = "workspace.json"
	// ControlSocketPrefix starts the names of the workspace's shared SSH
	// connection sockets (one per SSH agent; hostconfig.ShareOptions).
	ControlSocketPrefix = "ssh-"
)

// WorkspaceDir returns the workspace's folder, $DCX_HOME/workspaces/<id>,
// which holds everything dcx keeps on the host for it: its host key, lock,
// compose override and record. Container state lives in Docker labels;
// this is only what must exist without (or before) a container.
func WorkspaceDir(workspaceID string) (string, error) {
	if workspaceID == "" || strings.ContainsAny(workspaceID, `/\`) || workspaceID == "." || workspaceID == ".." {
		return "", fmt.Errorf("invalid workspace ID %q", workspaceID)
	}
	return Path(WorkspacesDir, workspaceID)
}

// WorkspacePath returns a file in the workspace's folder.
func WorkspacePath(workspaceID, name string) (string, error) {
	dir, err := WorkspaceDir(workspaceID)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, name), nil
}

// Workspace is the record in a workspace's folder: which workspace it
// belongs to, for people browsing $DCX_HOME and for `dcx clean`.
type Workspace struct {
	Path   string `json:"path"`
	Config string `json:"config"`
	Name   string `json:"name,omitempty"`
}

// WriteWorkspace writes the workspace's record (atomically).
func WriteWorkspace(workspaceID string, w Workspace) error {
	path, err := WorkspacePath(workspaceID, RecordFile)
	if err != nil {
		return err
	}
	data, err := json.MarshalIndent(w, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".workspace-")
	if err != nil {
		return err
	}
	_, werr := tmp.Write(append(data, '\n'))
	cerr := tmp.Close()
	if err := errors.Join(werr, cerr); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	if err := os.Rename(tmp.Name(), path); err != nil {
		_ = os.Remove(tmp.Name())
		return err
	}
	return nil
}

// ReadWorkspace reads the workspace's record (ok false without one).
func ReadWorkspace(workspaceID string) (Workspace, bool) {
	var w Workspace
	path, err := WorkspacePath(workspaceID, RecordFile)
	if err != nil {
		return w, false
	}
	data, err := os.ReadFile(path)
	if err != nil || json.Unmarshal(data, &w) != nil {
		return w, false
	}
	return w, true
}

// WorkspaceIDs lists the workspaces that have a folder.
func WorkspaceIDs() ([]string, error) {
	dir, err := Path(WorkspacesDir)
	if err != nil {
		return nil, err
	}
	entries, err := os.ReadDir(dir)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var ids []string
	for _, e := range entries {
		if e.IsDir() && !strings.HasPrefix(e.Name(), ".") {
			ids = append(ids, e.Name())
		}
	}
	return ids, nil
}
