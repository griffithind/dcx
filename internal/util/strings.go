package util

import "fmt"

// BoolToString converts a boolean to its string representation.
func BoolToString(b bool) string {
	if b {
		return "true"
	}
	return "false"
}

// UnionStrings returns a union of two string slices without duplicates.
// The order of elements is preserved, with elements from 'a' appearing first.
func UnionStrings(a, b []string) []string {
	if len(a) == 0 && len(b) == 0 {
		return nil
	}

	seen := make(map[string]bool)
	result := make([]string, 0, len(a)+len(b))

	for _, s := range a {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}
	for _, s := range b {
		if !seen[s] {
			seen[s] = true
			result = append(result, s)
		}
	}

	return result
}

// UnionInterfaces returns a union of two interface slices without duplicates.
// Elements are compared by their string representation.
func UnionInterfaces(a, b interface{}) []interface{} {
	seen := make(map[string]bool)
	result := []interface{}{}

	addItems := func(items interface{}) {
		if arr, ok := items.([]interface{}); ok {
			for _, item := range arr {
				key := fmt.Sprint(item)
				if !seen[key] {
					seen[key] = true
					result = append(result, item)
				}
			}
		}
	}

	addItems(a)
	addItems(b)
	return result
}
