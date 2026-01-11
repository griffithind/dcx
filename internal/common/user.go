package common

// GetDefaultHomeDir returns the default home directory for a user.
// This follows the standard Unix convention:
//   - root → /root
//   - other users → /home/<username>
//
// Note: For accurate home directory resolution in containers, use getent passwd
// which handles non-standard home directories. This function provides fallback defaults.
func GetDefaultHomeDir(user string) string {
	if user == "" || user == "root" {
		return "/root"
	}
	return "/home/" + user
}
