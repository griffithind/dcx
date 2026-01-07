package config

import (
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"

	"github.com/griffithind/dcx/internal/util"
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
}

// ValidateHostRequirementsWithDocker checks if Docker's configured resources meet the requirements.
// This is the preferred method as it checks Docker's actual available resources,
// which may be limited by Docker Desktop VM settings or cgroup limits.
func ValidateHostRequirementsWithDocker(reqs *HostRequirements, dockerRes *DockerResources) *HostRequirementsResult {
	result := &HostRequirementsResult{Satisfied: true}

	if reqs == nil {
		return result
	}

	// Check CPU cores against Docker's available CPUs
	if reqs.CPUs > 0 {
		availCPUs := dockerRes.CPUs
		if availCPUs < reqs.CPUs {
			result.Satisfied = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("CPU requirement not met: need %d cores, Docker has %d", reqs.CPUs, availCPUs))
		}
	}

	// Check memory against Docker's available memory
	if reqs.Memory != "" {
		reqBytes, err := parseMemoryString(reqs.Memory)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Could not parse memory requirement '%s': %v", reqs.Memory, err))
		} else {
			availMemory := dockerRes.Memory
			if availMemory > 0 && availMemory < reqBytes {
				result.Satisfied = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("Memory requirement not met: need %s, Docker has %s",
						reqs.Memory, formatBytes(availMemory)))
			}
		}
	}

	// Check GPU (GPU detection is still host-based as Docker doesn't report GPU info in system info)
	if reqs.GPU != nil {
		needsGPU := false
		switch v := reqs.GPU.(type) {
		case bool:
			needsGPU = v
		case string:
			needsGPU = v == "true" || v == "optional"
		case map[string]interface{}:
			needsGPU = len(v) > 0
		}

		if needsGPU && !hasGPU() {
			optional := false
			if s, ok := reqs.GPU.(string); ok && s == "optional" {
				optional = true
			}
			if optional {
				result.Warnings = append(result.Warnings, "No GPU detected (optional requirement)")
			} else {
				result.Satisfied = false
				result.Errors = append(result.Errors, "GPU requirement not met: no GPU detected")
			}
		}
	}

	// Storage cannot be validated meaningfully
	if reqs.Storage != "" {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Storage requirement (%s) cannot be validated automatically", reqs.Storage))
	}

	return result
}

// ValidateHostRequirements checks if the host meets the specified requirements.
// Deprecated: Use ValidateHostRequirementsWithDocker for accurate Docker resource checking.
// This function checks host resources directly, which may not reflect Docker's limits.
func ValidateHostRequirements(reqs *HostRequirements) *HostRequirementsResult {
	result := &HostRequirementsResult{Satisfied: true}

	if reqs == nil {
		return result
	}

	// Check CPU cores
	if reqs.CPUs > 0 {
		hostCPUs := runtime.NumCPU()
		if hostCPUs < reqs.CPUs {
			result.Satisfied = false
			result.Errors = append(result.Errors,
				fmt.Sprintf("CPU requirement not met: need %d cores, have %d", reqs.CPUs, hostCPUs))
		}
	}

	// Check memory
	if reqs.Memory != "" {
		reqBytes, err := parseMemoryString(reqs.Memory)
		if err != nil {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("Could not parse memory requirement '%s': %v", reqs.Memory, err))
		} else {
			hostMemory := getHostMemory()
			if hostMemory > 0 && hostMemory < reqBytes {
				result.Satisfied = false
				result.Errors = append(result.Errors,
					fmt.Sprintf("Memory requirement not met: need %s, have %s",
						reqs.Memory, formatBytes(hostMemory)))
			}
		}
	}

	// Check GPU
	if reqs.GPU != nil {
		needsGPU := false
		switch v := reqs.GPU.(type) {
		case bool:
			needsGPU = v
		case string:
			needsGPU = v == "true" || v == "optional"
		case map[string]interface{}:
			// GPU requirement object - any non-empty object means GPU is needed
			needsGPU = len(v) > 0
		}

		if needsGPU && !hasGPU() {
			// Only error if GPU is required (not optional)
			optional := false
			if s, ok := reqs.GPU.(string); ok && s == "optional" {
				optional = true
			}
			if optional {
				result.Warnings = append(result.Warnings, "No GPU detected (optional requirement)")
			} else {
				result.Satisfied = false
				result.Errors = append(result.Errors, "GPU requirement not met: no GPU detected")
			}
		}
	}

	// Storage is harder to validate meaningfully without knowing where Docker stores data
	// Just warn if specified
	if reqs.Storage != "" {
		result.Warnings = append(result.Warnings,
			fmt.Sprintf("Storage requirement (%s) cannot be validated automatically", reqs.Storage))
	}

	return result
}

// parseMemoryString parses a memory string like "4gb", "4096mb", "4g" into bytes.
func parseMemoryString(s string) (uint64, error) {
	s = strings.ToLower(strings.TrimSpace(s))
	if s == "" {
		return 0, fmt.Errorf("empty memory string")
	}

	// Pattern to match number and unit
	re := regexp.MustCompile(`^(\d+(?:\.\d+)?)\s*([kmgt]?b?)?$`)
	matches := re.FindStringSubmatch(s)
	if matches == nil {
		return 0, fmt.Errorf("invalid memory format: %s", s)
	}

	value, err := strconv.ParseFloat(matches[1], 64)
	if err != nil {
		return 0, fmt.Errorf("invalid number: %s", matches[1])
	}

	unit := matches[2]
	var multiplier uint64 = 1

	switch {
	case strings.HasPrefix(unit, "k"):
		multiplier = 1024
	case strings.HasPrefix(unit, "m"):
		multiplier = 1024 * 1024
	case strings.HasPrefix(unit, "g"):
		multiplier = 1024 * 1024 * 1024
	case strings.HasPrefix(unit, "t"):
		multiplier = 1024 * 1024 * 1024 * 1024
	case unit == "b" || unit == "":
		multiplier = 1
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

// getHostMemory returns the total system memory in bytes.
func getHostMemory() uint64 {
	// Try platform-specific methods
	if runtime.GOOS == "darwin" {
		return getDarwinMemory()
	}
	if runtime.GOOS == "linux" {
		return getLinuxMemory()
	}
	return 0
}

// getDarwinMemory gets total memory on macOS.
func getDarwinMemory() uint64 {
	out, err := exec.Command("sysctl", "-n", "hw.memsize").Output()
	if err != nil {
		return 0
	}
	mem, err := strconv.ParseUint(strings.TrimSpace(string(out)), 10, 64)
	if err != nil {
		return 0
	}
	return mem
}

// getLinuxMemory gets total memory on Linux from /proc/meminfo.
func getLinuxMemory() uint64 {
	data, err := os.ReadFile("/proc/meminfo")
	if err != nil {
		return 0
	}

	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "MemTotal:") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				kb, err := strconv.ParseUint(fields[1], 10, 64)
				if err == nil {
					return kb * 1024 // Convert KB to bytes
				}
			}
		}
	}
	return 0
}

// hasGPU checks if a GPU is available.
func hasGPU() bool {
	// Try nvidia-smi first
	if _, err := exec.LookPath("nvidia-smi"); err == nil {
		if err := exec.Command("nvidia-smi").Run(); err == nil {
			return true
		}
	}

	// Check for Apple Silicon GPU (macOS)
	if runtime.GOOS == "darwin" {
		// Apple Silicon always has GPU
		out, err := exec.Command("sysctl", "-n", "machdep.cpu.brand_string").Output()
		if err == nil && strings.Contains(string(out), "Apple") {
			return true
		}
	}

	// Check for /dev/dri on Linux (GPU devices)
	if runtime.GOOS == "linux" {
		if util.IsDir("/dev/dri") {
			return true
		}
	}

	return false
}
