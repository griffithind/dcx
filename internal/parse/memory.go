package parse

import (
	"fmt"
	"strconv"
	"strings"
)

// ParseMemorySize parses a memory size string (e.g., "4gb", "512m", "1.5g", "1024").
// Supported units: k/K, m/M, g/G, t/T (with optional 'b'/'B' suffix).
// Returns the size in bytes. Returns 0 for invalid/empty input.
func ParseMemorySize(s string) int64 {
	result, _ := ParseMemorySizeWithError(s)
	return result
}

// ParseMemorySizeWithError parses a memory size string with error reporting.
// Supported formats:
//   - Plain number: "1024" (interpreted as bytes)
//   - With unit: "4k", "512m", "2g", "1t"
//   - With 'b' suffix: "4kb", "512mb", "2gb", "1tb"
//   - Float values: "1.5g", "2.5gb"
func ParseMemorySizeWithError(s string) (int64, error) {
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

	return int64(value * float64(multiplier)), nil
}

// ParseShmSize parses a shared memory size string (e.g., "1g", "512m", "1024").
// This is an alias for ParseMemorySize for backward compatibility.
// Returns the size in bytes.
func ParseShmSize(size string) int64 {
	return ParseMemorySize(size)
}
