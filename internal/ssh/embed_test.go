package ssh

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minValidBinarySize is the minimum size for a valid dcx binary (1MB).
// Placeholder embeds are much smaller than this.
const minValidBinarySize = 1024 * 1024

// hasValidEmbeds returns true if the embedded binaries are real (not placeholders).
func hasValidEmbeds() bool {
	return len(dcxLinuxAmd64Compressed) > minValidBinarySize || len(dcxLinuxArm64Compressed) > minValidBinarySize
}

func TestHasEmbeddedBinaries(t *testing.T) {
	// This should always return true since we embed something (even placeholders)
	got := HasEmbeddedBinaries()
	assert.True(t, got, "HasEmbeddedBinaries should return true")
}

func TestGetEmbeddedBinaryAmd64(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	binary, err := GetEmbeddedBinary("amd64")
	require.NoError(t, err)
	require.NotNil(t, binary)
	assert.Greater(t, len(binary), minValidBinarySize, "amd64 binary should be larger than 1MB")
}

func TestGetEmbeddedBinaryArm64(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	binary, err := GetEmbeddedBinary("arm64")
	require.NoError(t, err)
	require.NotNil(t, binary)
	assert.Greater(t, len(binary), minValidBinarySize, "arm64 binary should be larger than 1MB")
}

func TestGetEmbeddedBinaryArchAliases(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	// Test x86_64 alias for amd64
	binary, err := GetEmbeddedBinary("x86_64")
	require.NoError(t, err)
	assert.NotNil(t, binary)

	// Test aarch64 alias for arm64
	binary, err = GetEmbeddedBinary("aarch64")
	require.NoError(t, err)
	assert.NotNil(t, binary)
}

func TestGetEmbeddedBinaryInvalidArch(t *testing.T) {
	_, err := GetEmbeddedBinary("invalid-arch")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported architecture")
}

func TestEmbeddedBinaryIsValidELF(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	// ELF magic bytes: 0x7F 'E' 'L' 'F'
	elfMagic := []byte{0x7F, 'E', 'L', 'F'}

	// Test amd64
	amd64Binary, err := GetEmbeddedBinary("amd64")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(amd64Binary), 4, "binary too small for ELF header")
	assert.Equal(t, elfMagic, amd64Binary[:4], "amd64 binary should have ELF magic bytes")

	// Test arm64
	arm64Binary, err := GetEmbeddedBinary("arm64")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(arm64Binary), 4, "binary too small for ELF header")
	assert.Equal(t, elfMagic, arm64Binary[:4], "arm64 binary should have ELF magic bytes")
}

func TestEmbeddedBinaryExecutable(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	// On Linux, we can execute directly
	if runtime.GOOS == "linux" {
		arch := runtime.GOARCH
		binary, err := GetEmbeddedBinary(arch)
		require.NoError(t, err)

		// Write to temp file
		tmpFile, err := os.CreateTemp("", "dcx-test-*")
		require.NoError(t, err)
		defer os.Remove(tmpFile.Name())

		_, err = tmpFile.Write(binary)
		require.NoError(t, err)
		tmpFile.Close()

		// Make executable
		err = os.Chmod(tmpFile.Name(), 0755)
		require.NoError(t, err)

		// Run with --version
		cmd := exec.Command(tmpFile.Name(), "--version")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "binary --version failed: %s", string(output))

		assert.Contains(t, string(output), "dcx", "version output should contain 'dcx'")
		return
	}

	// On macOS/Windows, use Docker to test execution
	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Skipping execution test: Docker not available")
	}

	// Use amd64 binary for Docker test (most common)
	binary, err := GetEmbeddedBinary("amd64")
	require.NoError(t, err)

	// Write to temp file
	tmpFile, err := os.CreateTemp("", "dcx-test-*")
	require.NoError(t, err)
	defer os.Remove(tmpFile.Name())

	_, err = tmpFile.Write(binary)
	require.NoError(t, err)
	tmpFile.Close()

	// Make executable
	err = os.Chmod(tmpFile.Name(), 0755)
	require.NoError(t, err)

	// Run in Docker container
	cmd := exec.Command("docker", "run", "--rm", "--platform=linux/amd64",
		"-v", tmpFile.Name()+":/dcx:ro",
		"alpine:latest", "/dcx", "--version")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "binary --version in Docker failed: %s", string(output))

	assert.Contains(t, string(output), "dcx", "version output should contain 'dcx'")
}

func TestDecompressOnlyOnce(t *testing.T) {
	if !hasValidEmbeds() {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	// Call GetEmbeddedBinary multiple times - should use cached decompressed data
	binary1, err := GetEmbeddedBinary("amd64")
	require.NoError(t, err)

	binary2, err := GetEmbeddedBinary("amd64")
	require.NoError(t, err)

	// Should return the same slice (same memory address)
	assert.Equal(t, &binary1[0], &binary2[0], "multiple calls should return cached data")
}
