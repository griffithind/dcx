package devcontainer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
}

func TestDockerfileBaseImage(t *testing.T) {
	dir := t.TempDir()
	df := filepath.Join(dir, "Dockerfile")
	tests := []struct {
		name, dockerfile, target string
		args                     map[string]string
		want                     string
	}{
		{"single stage", "FROM mcr.microsoft.com/devcontainers/typescript-node:1-20\nUSER node\n", "", nil, "mcr.microsoft.com/devcontainers/typescript-node:1-20"},
		{"platform flag and comments", "# syntax\nFROM --platform=linux/amd64 alpine:3.20 AS base\n", "", nil, "alpine:3.20"},
		{"ARG default", "ARG VARIANT=3.12\nFROM python:${VARIANT}\n", "", nil, "python:3.12"},
		{"build arg overrides", "ARG VARIANT=3.12\nFROM python:${VARIANT}\n", "", map[string]string{"VARIANT": "3.13"}, "python:3.13"},
		{"brace default", "FROM node:${NODE:-20}\n", "", nil, "node:20"},
		{"stages follow names", "FROM golang:1.24 AS build\nFROM build AS dev\nFROM alpine:3 AS final\n", "dev", nil, "golang:1.24"},
		{"last stage by default", "FROM golang:1.24 AS build\nFROM alpine:3\n", "", nil, "alpine:3"},
		{"continuation", "FROM \\\n  debian:12\n", "", nil, "debian:12"},
		{"unknown variable", "FROM ${IMAGE}\n", "", nil, ""},
		{"scratch", "FROM scratch\n", "", nil, ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			writeFile(t, df, tt.dockerfile)
			assert.Equal(t, tt.want, dockerfileBaseImage(df, tt.target, tt.args))
		})
	}
}

func TestComposeServiceBaseImage(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "app", "Dockerfile"), "ARG V=1-20\nFROM mcr.microsoft.com/devcontainers/javascript-node:${V}\n")
	base := filepath.Join(dir, "compose.yml")
	writeFile(t, base, "services:\n  app:\n    image: postgres-should-be-overridden\n  db:\n    image: postgres:16\n")
	override := filepath.Join(dir, "compose.override.yml")
	writeFile(t, override, "services:\n  app:\n    build:\n      context: ./app\n      args:\n        V: 1-22\n")

	assert.Equal(t, "mcr.microsoft.com/devcontainers/javascript-node:1-22", composeServiceBaseImage([]string{base, override}, "app"))
	assert.Equal(t, "postgres:16", composeServiceBaseImage([]string{base, override}, "db"))

	t.Setenv("DCX_TEST_TAG", "17")
	writeFile(t, base, "services:\n  db:\n    image: postgres:${DCX_TEST_TAG}\n")
	assert.Equal(t, "postgres:17", composeServiceBaseImage([]string{base}, "db"), "compose interpolation")
}
