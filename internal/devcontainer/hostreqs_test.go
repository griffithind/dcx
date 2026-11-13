package devcontainer

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseMemoryString(t *testing.T) {
	// Test valid cases return no error
	t.Run("valid cases return no error", func(t *testing.T) {
		validCases := []string{"1024", "1k", "64m", "2g", "1t", "1.5g", "4gb"}
		for _, input := range validCases {
			result, err := parseMemoryString(input)
			require.NoError(t, err, "input: %s", input)
			assert.Greater(t, result, uint64(0), "input: %s", input)
		}
	})

	// Test error cases
	t.Run("empty string returns error", func(t *testing.T) {
		_, err := parseMemoryString("")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "empty")
	})

	t.Run("invalid format returns error", func(t *testing.T) {
		_, err := parseMemoryString("abc")
		assert.Error(t, err)
	})

	t.Run("invalid unit returns error", func(t *testing.T) {
		_, err := parseMemoryString("100xyz")
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "invalid unit")
	})
}

func TestParseMemoryString_Values(t *testing.T) {
	tests := []struct {
		input    string
		expected uint64
	}{
		{"1024", 1024},
		{"1k", 1024},
		{"1kb", 1024},
		{"1m", 1024 * 1024},
		{"1mb", 1024 * 1024},
		{"1g", 1024 * 1024 * 1024},
		{"1gb", 1024 * 1024 * 1024},
		{"2g", 2 * 1024 * 1024 * 1024},
		{"1.5g", uint64(1.5 * 1024 * 1024 * 1024)},
		{"512m", 512 * 1024 * 1024},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result, err := parseMemoryString(tt.input)
			require.NoError(t, err)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestGPURequirementAgainstDockerRuntime(t *testing.T) {
	required := &HostRequirements{GPU: &GPURequirement{Mode: GPURequired}}
	optional := &HostRequirements{GPU: &GPURequirement{Mode: GPUOptional}}
	none := &HostRequirements{GPU: &GPURequirement{Mode: GPUNone}}

	if r := ValidateHostRequirementsWithDocker(required, &DockerResources{}); r.Satisfied {
		t.Error("a required GPU without a GPU runtime must fail")
	}
	if r := ValidateHostRequirementsWithDocker(required, &DockerResources{GPU: true}); !r.Satisfied {
		t.Errorf("a GPU runtime satisfies a required GPU: %v", r.Errors)
	}
	if r := ValidateHostRequirementsWithDocker(optional, &DockerResources{}); !r.Satisfied || len(r.Warnings) != 1 {
		t.Errorf("an optional GPU only warns: %+v", r)
	}
	if r := ValidateHostRequirementsWithDocker(none, &DockerResources{}); !r.Satisfied || len(r.Warnings) != 0 {
		t.Errorf("gpu: false asks for nothing: %+v", r)
	}
}

// CPU and memory minimums warn instead of refusing to start (S3); storage
// is not reported.
func TestHostRequirementShortfallsWarn(t *testing.T) {
	r := ValidateHostRequirementsWithDocker(&HostRequirements{CPUs: 8, Memory: "32gb", Storage: "64gb"},
		&DockerResources{CPUs: 4, Memory: 8 << 30})
	assert.True(t, r.Satisfied)
	assert.Len(t, r.Warnings, 2, "%v", r.Warnings)
	assert.Empty(t, r.Errors)
}

// Image metadata's hostRequirements merge by taking the larger of each.
func TestMergeMetadataTakesLargerHostRequirements(t *testing.T) {
	local := &DevContainerConfig{HostRequirements: &HostRequirements{CPUs: 2, Memory: "4gb"}}
	image := []DevContainerConfig{{HostRequirements: &HostRequirements{CPUs: 4, Memory: "2gb", Storage: "32gb",
		GPU: &GPURequirement{Mode: GPUOptional}}}}
	merged := MergeMetadata(local, image)
	require.NotNil(t, merged.HostRequirements)
	assert.Equal(t, 4, merged.HostRequirements.CPUs)
	assert.Equal(t, "4gb", merged.HostRequirements.Memory)
	assert.Equal(t, "32gb", merged.HostRequirements.Storage)
	assert.Equal(t, GPUOptional, merged.HostRequirements.GPU.Mode)
	assert.Equal(t, 2, local.HostRequirements.CPUs, "inputs are not modified")
}
