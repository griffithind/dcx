package env

import (
	"context"
	"strings"
	"testing"

	"github.com/griffithind/dcx/internal/container/containertest"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderEtcEnvironmentReplacesOwnBlock(t *testing.T) {
	current := "PATH=\"/usr/bin\"\nLANG=C\n" + blockBegin + "\nOLD=\"gone\"\n" + blockEnd + "\n"
	got, skipped := renderEtcEnvironment(current, map[string]string{"B": "2", "A": "1"})
	assert.Empty(t, skipped)
	assert.Equal(t, "PATH=\"/usr/bin\"\nLANG=C\n"+blockBegin+"\nA=\"1\"\nB=\"2\"\n"+blockEnd+"\n", got,
		"user lines kept, block replaced and sorted")

	again, _ := renderEtcEnvironment(got, map[string]string{"A": "1", "B": "2"})
	assert.Equal(t, got, again, "idempotent")

	removed, _ := renderEtcEnvironment(got, nil)
	assert.Equal(t, "PATH=\"/usr/bin\"\nLANG=C\n", removed, "an empty env removes the block")
	empty, _ := renderEtcEnvironment("", nil)
	assert.Equal(t, "", empty)
}

// Values are data, never shell: a value that tried to end a heredoc or
// inject a command used to run as root.
func TestRenderEtcEnvironmentRejectsUnrepresentableValues(t *testing.T) {
	got, skipped := renderEtcEnvironment("", map[string]string{
		"OK":        `a'b c $(id) ; rm -rf /`,
		"QUOTE":     `a"b`,
		"BACKSLASH": `a\b`,
		"MULTILINE": "x\nDCXEOF\ntouch /pwned",
		"BAD-NAME":  "v",
		"1LEADING":  "v",
	})
	assert.ElementsMatch(t, []string{"QUOTE", "BACKSLASH", "MULTILINE", "BAD-NAME", "1LEADING"}, skipped)
	assert.Equal(t, blockBegin+"\nOK=\"a'b c $(id) ; rm -rf /\"\n"+blockEnd+"\n", got)
}

func TestPatchEtcEnvironmentWritesThroughEngine(t *testing.T) {
	read := strings.Join(readIfExists, " ")
	fake := &containertest.Fake{Outputs: map[string]string{read: "LANG=C\n"}}
	skipped, err := NewPatcher(fake).PatchEtcEnvironment(context.Background(), "c1", map[string]string{"FOO": "bar"})
	require.NoError(t, err)
	assert.Empty(t, skipped)
	assert.Equal(t, "LANG=C\n"+blockBegin+"\nFOO=\"bar\"\n"+blockEnd+"\n", string(fake.Files["c1:/etc/environment"]))
	writes := fake.CallsTo("WriteFileInContainer")
	require.Len(t, writes, 1)
	assert.Equal(t, []string{"c1", "/etc/environment", "", "0644", ""}, writes[0].Args, "root-owned, world-readable, one exec")
	for _, c := range fake.CallsTo("ExecAsRoot") {
		assert.False(t, strings.Contains(strings.Join(c.Args, " "), "bar"), "values must not reach a command line")
	}

	// Unchanged content is not rewritten.
	fake2 := &containertest.Fake{Outputs: map[string]string{read: string(fake.Files["c1:/etc/environment"])}}
	_, err = NewPatcher(fake2).PatchEtcEnvironment(context.Background(), "c1", map[string]string{"FOO": "bar"})
	require.NoError(t, err)
	assert.Empty(t, fake2.CallsTo("WriteFileInContainer"))

	// A failed read (R14) must not rewrite the file without its lines.
	fake3 := &containertest.Fake{}
	_, err = NewPatcher(fake3).PatchEtcEnvironment(context.Background(), "c1", map[string]string{"FOO": "bar"})
	require.Error(t, err)
	assert.Empty(t, fake3.CallsTo("WriteFileInContainer"))
}

func TestPatchEtcProfilePassesMarkerAsArgument(t *testing.T) {
	fake := &containertest.Fake{}
	require.NoError(t, NewPatcher(fake).PatchEtcProfile(context.Background(), "c1"))
	calls := fake.CallsTo("ExecAsRoot")
	require.Len(t, calls, 1)
	// Both markers are positional arguments: the profile marker, then the
	// one older dcx left for /etc/environment, removed in the same exec.
	assert.Equal(t, []string{markerEtcProfile, legacyMarkerEtcEnvironment}, calls[0].Args[len(calls[0].Args)-2:])
}
