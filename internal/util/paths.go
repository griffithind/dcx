package util

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// RealPath returns the absolute path with symlinks resolved.
func RealPath(path string) (string, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	return filepath.EvalSymlinks(absPath)
}

// BaseName returns the base name of a path.
func BaseName(path string) string {
	return filepath.Base(path)
}

// JoinPath joins path elements.
func JoinPath(elem ...string) string {
	return filepath.Join(elem...)
}

// RelPath returns a relative path from base to target.
func RelPath(base, target string) (string, error) {
	return filepath.Rel(base, target)
}

// Exists checks if a path exists.
func Exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// IsDir checks if a path is a directory.
func IsDir(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// IsFile checks if a path is a regular file.
func IsFile(path string) bool {
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.Mode().IsRegular()
}

// CacheDir returns the appropriate cache directory for dcx.
// Linux: $XDG_CACHE_HOME/dcx or ~/.cache/dcx
// macOS: ~/Library/Caches/dcx
func CacheDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Caches", "dcx"), nil

	default: // linux and others
		if cacheHome := os.Getenv("XDG_CACHE_HOME"); cacheHome != "" {
			return filepath.Join(cacheHome, "dcx"), nil
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, ".cache", "dcx"), nil
	}
}

// RuntimeDir returns the appropriate runtime directory for dcx.
// Linux: $XDG_RUNTIME_DIR/dcx or /run/user/$UID/dcx
// macOS: ~/Library/Caches/dcx (no separate runtime dir)
func RuntimeDir() (string, error) {
	switch runtime.GOOS {
	case "darwin":
		// macOS doesn't have a standard runtime dir, use cache
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		return filepath.Join(home, "Library", "Caches", "dcx"), nil

	default: // linux
		if runtimeDir := os.Getenv("XDG_RUNTIME_DIR"); runtimeDir != "" {
			return filepath.Join(runtimeDir, "dcx"), nil
		}
		// Fallback to /run/user/$UID
		uid := os.Getuid()
		return filepath.Join("/run", "user", string(rune(uid)), "dcx"), nil
	}
}

// EnsureDir creates a directory with the specified permissions if it doesn't exist.
func EnsureDir(path string, perm os.FileMode) error {
	if err := os.MkdirAll(path, perm); err != nil {
		return err
	}
	// Ensure permissions are set correctly even if dir existed
	return os.Chmod(path, perm)
}

// NormalizePath normalizes a path for comparison.
func NormalizePath(path string) string {
	// Clean the path
	path = filepath.Clean(path)
	// Normalize slashes
	path = filepath.ToSlash(path)
	// Remove trailing slash
	return strings.TrimSuffix(path, "/")
}
