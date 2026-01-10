package build

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// ContextBuilder helps create tar archives for Docker build contexts.
type ContextBuilder struct {
	buffer *bytes.Buffer
	writer *tar.Writer
}

// NewContextBuilder creates a new build context builder.
func NewContextBuilder() *ContextBuilder {
	buf := new(bytes.Buffer)
	return &ContextBuilder{
		buffer: buf,
		writer: tar.NewWriter(buf),
	}
}

// AddFile adds a file to the build context.
func (c *ContextBuilder) AddFile(name string, content []byte, mode int64) error {
	header := &tar.Header{
		Name: name,
		Mode: mode,
		Size: int64(len(content)),
	}

	if err := c.writer.WriteHeader(header); err != nil {
		return fmt.Errorf("failed to write tar header for %s: %w", name, err)
	}

	if _, err := c.writer.Write(content); err != nil {
		return fmt.Errorf("failed to write tar content for %s: %w", name, err)
	}

	return nil
}

// AddFileFromPath adds a file from the filesystem to the build context.
func (c *ContextBuilder) AddFileFromPath(name, sourcePath string) error {
	content, err := os.ReadFile(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to read file %s: %w", sourcePath, err)
	}

	info, err := os.Stat(sourcePath)
	if err != nil {
		return fmt.Errorf("failed to stat file %s: %w", sourcePath, err)
	}

	return c.AddFile(name, content, int64(info.Mode()))
}

// AddDirectory adds a directory recursively to the build context.
func (c *ContextBuilder) AddDirectory(prefix, sourcePath string) error {
	return filepath.Walk(sourcePath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Skip directories
		if info.IsDir() {
			return nil
		}

		// Compute relative path
		relPath, err := filepath.Rel(sourcePath, path)
		if err != nil {
			return err
		}

		// Build destination name
		name := relPath
		if prefix != "" {
			name = filepath.Join(prefix, relPath)
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return c.AddFile(name, content, int64(info.Mode()))
	})
}

// Build finalizes the build context and returns a reader.
func (c *ContextBuilder) Build() (io.Reader, error) {
	if err := c.writer.Close(); err != nil {
		return nil, fmt.Errorf("failed to close tar writer: %w", err)
	}
	return bytes.NewReader(c.buffer.Bytes()), nil
}

// CreateTarContext creates a tar archive from a directory for Docker build.
// This is a convenience function for simple use cases.
func CreateTarContext(contextPath string, excludePatterns []string) (io.Reader, error) {
	builder := NewContextBuilder()

	err := filepath.Walk(contextPath, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Compute relative path
		relPath, err := filepath.Rel(contextPath, path)
		if err != nil {
			return err
		}

		// Skip root directory entry
		if relPath == "." {
			return nil
		}

		// Check exclusion patterns
		for _, pattern := range excludePatterns {
			if matched, _ := filepath.Match(pattern, filepath.Base(path)); matched {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			// Also check if path contains the pattern
			if strings.Contains(relPath, pattern) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
		}

		// Skip directories (they're implied by file paths)
		if info.IsDir() {
			return nil
		}

		// Read file content
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}

		return builder.AddFile(relPath, content, int64(info.Mode()))
	})

	if err != nil {
		return nil, fmt.Errorf("failed to walk context directory: %w", err)
	}

	return builder.Build()
}

// CreateGzippedTarContext creates a gzipped tar archive from a directory.
func CreateGzippedTarContext(contextPath string, excludePatterns []string) (io.Reader, error) {
	tarReader, err := CreateTarContext(contextPath, excludePatterns)
	if err != nil {
		return nil, err
	}

	// Read tar content
	tarContent, err := io.ReadAll(tarReader)
	if err != nil {
		return nil, fmt.Errorf("failed to read tar content: %w", err)
	}

	// Compress with gzip
	var gzBuf bytes.Buffer
	gzWriter := gzip.NewWriter(&gzBuf)
	if _, err := gzWriter.Write(tarContent); err != nil {
		return nil, fmt.Errorf("failed to write gzip content: %w", err)
	}
	if err := gzWriter.Close(); err != nil {
		return nil, fmt.Errorf("failed to close gzip writer: %w", err)
	}

	return bytes.NewReader(gzBuf.Bytes()), nil
}

// DefaultExcludePatterns returns the default patterns to exclude from build contexts.
func DefaultExcludePatterns() []string {
	return []string{
		".git",
		".gitignore",
		".dockerignore",
		"node_modules",
		"__pycache__",
		".pytest_cache",
		".mypy_cache",
		"*.pyc",
		".devcontainer/.cache",
		".dcx",
	}
}
