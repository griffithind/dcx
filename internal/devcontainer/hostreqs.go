package devcontainer

import (
	"fmt"
	"strconv"
	"strings"
)

// DockerResources represents the resources available to Docker.
// This may be less than the host's actual resources (e.g., Docker Desktop VM limits).
type DockerResources struct {
	CPUs   int    // Number of CPUs available to Docker
	Memory uint64 // Total memory available to Docker in bytes
	GPU    bool   // Docker has a GPU (NVIDIA) runtime, so --gpus works
}

// HostRequirementWarnings checks hostRequirements against Docker's
// resources (which Docker Desktop VM settings or cgroup limits may cap)
// and returns what falls short. Nothing refuses to start, as in
// devcontainers/cli: CPU and memory minimums are advice (many repositories
// set Codespaces-sized ones), and without a GPU runtime the container
// starts without GPUs (a required GPU warns, an optional one is silent).
func HostRequirementWarnings(reqs *HostRequirements, dockerRes *DockerResources) []string {
	var warnings []string
	if reqs == nil {
		return nil
	}

	if reqs.CPUs > 0 && dockerRes.CPUs < reqs.CPUs {
		warnings = append(warnings,
			fmt.Sprintf("hostRequirements: %d CPUs recommended, Docker has %d", reqs.CPUs, dockerRes.CPUs))
	}
	if reqs.Memory != "" {
		reqBytes, err := parseMemoryString(reqs.Memory)
		if err != nil {
			warnings = append(warnings,
				fmt.Sprintf("Could not parse memory requirement '%s': %v", reqs.Memory, err))
		} else if dockerRes.Memory > 0 && dockerRes.Memory < reqBytes {
			warnings = append(warnings,
				fmt.Sprintf("hostRequirements: %s of memory recommended, Docker has %s", reqs.Memory, formatBytes(dockerRes.Memory)))
		}
	}

	if reqs.GPU.Wanted() && reqs.GPU.Mode != GPUOptional && !dockerRes.GPU {
		warnings = append(warnings, `hostRequirements: a GPU is required but the Docker host has no GPU runtime; starting without GPUs (mark it "optional" to silence this)`)
	}

	// storage can't be checked meaningfully; it is not reported.

	return warnings
}

// parseMemoryString parses a memory string like "4gb", "4096mb", "4g" into bytes.
// Supported formats:
//   - Plain number: "1024" (interpreted as bytes)
//   - With unit: "4k", "512m", "2g", "1t"
//   - With 'b' suffix: "4kb", "512mb", "2gb", "1tb"
//   - Float values: "1.5g", "2.5gb"
func parseMemoryString(s string) (uint64, error) {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	s = strings.ToLower(s)

	// Find where the numeric part ends
	numEnd := 0
	hasDecimal := false
	for i, c := range s {
		if c >= '0' && c <= '9' {
			numEnd = i + 1
		} else if c == '.' && !hasDecimal {
			hasDecimal = true
			numEnd = i + 1
		} else {
			break
		}
	}

	if numEnd == 0 {
		return 0, fmt.Errorf("invalid memory format: %s", s)
	}

	numPart := s[:numEnd]
	unitPart := strings.TrimSpace(s[numEnd:])

	// Parse the numeric value
	value, err := strconv.ParseFloat(numPart, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", numPart)
	}

	// Determine multiplier from unit
	var multiplier int64

	// Remove trailing 'b' if present (e.g., "kb" -> "k", "gb" -> "g")
	if len(unitPart) > 0 && unitPart[len(unitPart)-1] == 'b' {
		unitPart = unitPart[:len(unitPart)-1]
	}

	switch unitPart {
	case "":
		multiplier = 1
	case "k":
		multiplier = 1024
	case "m":
		multiplier = 1024 * 1024
	case "g":
		multiplier = 1024 * 1024 * 1024
	case "t":
		multiplier = 1024 * 1024 * 1024 * 1024
	default:
		return 0, fmt.Errorf("invalid unit: %s", unitPart)
	}

	return uint64(value * float64(multiplier)), nil
}

// formatBytes formats bytes as a human-readable string.
func formatBytes(b uint64) string {
	const unit = 1024
	if b < unit {
		return fmt.Sprintf("%d B", b)
	}
	div, exp := uint64(unit), 0
	for n := b / unit; n >= unit; n /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(b)/float64(div), "KMGTPE"[exp])
}
