package deploy

import (
	"os"
	"os/exec"
	"runtime"
	"testing"

	"github.com/griffithind/dcx"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// minValidBinarySize is the minimum size for a valid dcx-agent binary (1MB).
// Placeholder embeds are much smaller than this.
const minValidBinarySize = 1024 * 1024

func TestGetEmbeddedBinaryAmd64(t *testing.T) {
	binary, err := dcxembed.GetBinary("amd64")
	if err != nil {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}
	require.NotNil(t, binary)
	if len(binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders")
	}
	assert.Greater(t, len(binary), minValidBinarySize, "amd64 binary should be larger than 1MB")
}

func TestGetEmbeddedBinaryArm64(t *testing.T) {
	binary, err := dcxembed.GetBinary("arm64")
	if err != nil {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}
	require.NotNil(t, binary)
	if len(binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders")
	}
	assert.Greater(t, len(binary), minValidBinarySize, "arm64 binary should be larger than 1MB")
}

func TestGetEmbeddedBinaryArchAliases(t *testing.T) {
	binary, err := dcxembed.GetBinary("x86_64")
	if err != nil || len(binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}
	assert.NotNil(t, binary)

	binary, err = dcxembed.GetBinary("aarch64")
	if err != nil || len(binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders")
	}
	assert.NotNil(t, binary)
}

func TestGetEmbeddedBinaryInvalidArch(t *testing.T) {
	_, err := dcxembed.GetBinary("invalid-arch")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unsupported architecture")
}

func TestEmbeddedBinaryIsValidELF(t *testing.T) {
	amd64Binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(amd64Binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	elfMagic := []byte{0x7F, 'E', 'L', 'F'}

	require.GreaterOrEqual(t, len(amd64Binary), 4, "binary too small for ELF header")
	assert.Equal(t, elfMagic, amd64Binary[:4], "amd64 binary should have ELF magic bytes")

	arm64Binary, err := dcxembed.GetBinary("arm64")
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(arm64Binary), 4, "binary too small for ELF header")
	assert.Equal(t, elfMagic, arm64Binary[:4], "arm64 binary should have ELF magic bytes")
}

func TestEmbeddedBinaryExecutable(t *testing.T) {
	binary, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	if runtime.GOOS == "linux" {
		arch := runtime.GOARCH
		binary, err := dcxembed.GetBinary(arch)
		require.NoError(t, err)

		// Use t.TempDir() for automatic cleanup
		tmpDir := t.TempDir()
		tmpPath := tmpDir + "/dcx-agent-test"

		err = os.WriteFile(tmpPath, binary, 0755)
		require.NoError(t, err)

		cmd := exec.Command(tmpPath, "--help")
		output, err := cmd.CombinedOutput()
		require.NoError(t, err, "binary --help failed: %s", string(output))

		assert.Contains(t, string(output), "dcx-agent", "help output should contain 'dcx-agent'")
		return
	}

	if err := exec.Command("docker", "info").Run(); err != nil {
		t.Skip("Skipping execution test: Docker not available")
	}

	// Use t.TempDir() for automatic cleanup
	tmpDir := t.TempDir()
	tmpPath := tmpDir + "/dcx-agent-test"

	err = os.WriteFile(tmpPath, binary, 0755)
	require.NoError(t, err)

	cmd := exec.Command("docker", "run", "--rm", "--platform=linux/amd64",
		"-v", tmpPath+":/dcx-agent:ro",
		"alpine:latest", "/dcx-agent", "--help")
	output, err := cmd.CombinedOutput()
	require.NoError(t, err, "binary --help in Docker failed: %s", string(output))

	assert.Contains(t, string(output), "dcx-agent", "help output should contain 'dcx-agent'")
}

func TestDecompressOnlyOnce(t *testing.T) {
	binary1, err := dcxembed.GetBinary("amd64")
	if err != nil || len(binary1) < minValidBinarySize {
		t.Skip("Skipping: embedded binaries are placeholders (run 'make build' first)")
	}

	binary2, err := dcxembed.GetBinary("amd64")
	require.NoError(t, err)

	assert.Equal(t, &binary1[0], &binary2[0], "multiple calls should return cached data")
}
