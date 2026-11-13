package devcontainer

import (
	"fmt"
	"strconv"
	"strings"
)

// HostRequirementsResult contains the result of host requirements validation.
type HostRequirementsResult struct {
	Satisfied bool
	Warnings  []string
	Errors    []string
}

// DockerResources represents the resources available to Docker.
// This may be less than the host's actual resources (e.g., Docker Desktop VM limits).
type DockerResources struct {
	CPUs   int    // Number of CPUs available to Docker
	Memory uint64 // Total memory available to Docker in bytes
	GPU    bool   // Docker has a GPU (NVIDIA) runtime, so --gpus works
}

// ValidateHostRequirementsWithDocker checks if Docker's configured resources meet the requirements.
// This is the preferred method as it checks Docker's actual available resources,
// which may be limited by Docker Desktop VM settings or cgroup limits.
func ValidateHostRequirementsWithDocker(reqs *HostRequirements, dockerRes *DockerResources) *HostRequirementsResult {
	result := &HostRequirementsResult{Satisfied: true}

	if reqs == nil {
		return result
	}

	// CPU and memory minimums are advice: many repositories set
	// Codespaces-sized ones, and refusing to start on a laptop helps no
	// one. They warn; a missing GPU runtime fails (--gpus would).
	if reqs.CPUs > 0 && dockerRes.CPUs < reqs.CPUs {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("hostRequirements: %d CPUs recommended, Docker has %d", reqs.CPUs, dockerRes.CPUs))
	}
	if reqs.Memory != "" {
		reqBytes, err := parseMemoryString(reqs.Memory)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Could not parse memory requirement '%s': %v", reqs.Memory, err))
		} else if dockerRes.Memory > 0 && dockerRes.Memory < reqBytes {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("hostRequirements: %s of memory recommended, Docker has %s", reqs.Memory, formatBytes(dockerRes.Memory)))
		}
	}

	// Check GPU against Docker's GPU runtime: without it --gpus fails.
	if reqs.GPU.Wanted() && !dockerRes.GPU {
		if reqs.GPU.Mode == GPUOptional {
			result.Warnings = append(result.Warnings, "No GPU runtime on the Docker host; starting without GPUs (optional requirement)")
		} else {
			result.Satisfied = false
			result.Errors = append(result.Errors, "GPU requirement not met: the Docker host has no GPU runtime")
		}
	}

	// storage can't be checked meaningfully; it is not reported.

	return result
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
