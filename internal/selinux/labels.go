package selinux

import (
	"fmt"
	"strings"
)

// MountOption represents SELinux bind mount label options.
type MountOption string

const (
	// MountOptionNone means no SELinux relabeling.
	MountOptionNone MountOption = ""

	// MountOptionZ means private unshared label (only this container can use).
	MountOptionZ MountOption = "Z"

	// MountOptionz means shared label (multiple containers can use).
	MountOptionz MountOption = "z"
)

// String returns the string representation of the mount option.
func (o MountOption) String() string {
	return string(o)
}

// GetMountOption returns the appropriate mount option based on SELinux mode.
// If SELinux is enforcing, returns :Z for private relabeling.
func GetMountOption() MountOption {
	mode, err := GetMode()
	if err != nil || !mode.IsEnforcing() {
		return MountOptionNone
	}
	return MountOptionZ
}

// ApplyToMount adds the SELinux mount option suffix to a mount spec.
func ApplyToMount(mountSpec string) string {
	option := GetMountOption()
	if option == MountOptionNone {
		return mountSpec
	}

	// Don't add if already has an option
	if strings.HasSuffix(mountSpec, ":Z") || strings.HasSuffix(mountSpec, ":z") {
		return mountSpec
	}

	return fmt.Sprintf("%s:%s", mountSpec, option)
}

// FormatMount formats a source:target mount with appropriate SELinux options.
func FormatMount(source, target string) string {
	mountSpec := fmt.Sprintf("%s:%s", source, target)
	return ApplyToMount(mountSpec)
}

// FormatMountWithOptions formats a mount with additional options and SELinux support.
func FormatMountWithOptions(source, target string, options []string) string {
	// Check if options already include SELinux labels
	hasSelinuxOption := false
	for _, opt := range options {
		if opt == "Z" || opt == "z" {
			hasSelinuxOption = true
			break
		}
	}

	// Add SELinux option if needed
	if !hasSelinuxOption {
		option := GetMountOption()
		if option != MountOptionNone {
			options = append(options, string(option))
		}
	}

	if len(options) == 0 {
		return fmt.Sprintf("%s:%s", source, target)
	}

	return fmt.Sprintf("%s:%s:%s", source, target, strings.Join(options, ","))
}

// ShouldRelabel returns true if SELinux relabeling should be applied.
func ShouldRelabel() bool {
	mode, err := GetMode()
	if err != nil {
		return false
	}
	return mode.IsEnforcing()
}
