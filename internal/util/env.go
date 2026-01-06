package util

import (
	"os"
	"strings"
)

// GetEnv returns an environment variable value with a default fallback.
func GetEnv(key, defaultValue string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return defaultValue
}

// GetEnvBool returns an environment variable as a boolean.
func GetEnvBool(key string, defaultValue bool) bool {
	value := os.Getenv(key)
	if value == "" {
		return defaultValue
	}
	switch strings.ToLower(value) {
	case "1", "true", "yes", "on":
		return true
	case "0", "false", "no", "off":
		return false
	default:
		return defaultValue
	}
}

// ExpandEnv expands environment variables in a string.
// Supports both $VAR and ${VAR} syntax.
func ExpandEnv(s string) string {
	return os.ExpandEnv(s)
}

// MergeEnv merges environment variables, with later values overriding earlier ones.
func MergeEnv(envMaps ...map[string]string) map[string]string {
	result := make(map[string]string)
	for _, env := range envMaps {
		for k, v := range env {
			result[k] = v
		}
	}
	return result
}

// EnvMapToSlice converts a map of environment variables to a slice of KEY=VALUE strings.
func EnvMapToSlice(env map[string]string) []string {
	result := make([]string, 0, len(env))
	for k, v := range env {
		result = append(result, k+"="+v)
	}
	return result
}

// EnvSliceToMap converts a slice of KEY=VALUE strings to a map.
func EnvSliceToMap(env []string) map[string]string {
	result := make(map[string]string)
	for _, e := range env {
		parts := strings.SplitN(e, "=", 2)
		if len(parts) == 2 {
			result[parts[0]] = parts[1]
		}
	}
	return result
}

// LookupEnv returns the value of an environment variable and whether it was set.
func LookupEnv(key string) (string, bool) {
	return os.LookupEnv(key)
}
