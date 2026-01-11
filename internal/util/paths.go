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

// NormalizePath normalizes a path for comparison.
func NormalizePath(path string) string {
	// Clean the path
	path = filepath.Clean(path)
	// Normalize slashes
	path = filepath.ToSlash(path)
	// Remove trailing slash
	return strings.TrimSuffix(path, "/")
}
