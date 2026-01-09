package util

import (
	"os"
	"path/filepath"
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
