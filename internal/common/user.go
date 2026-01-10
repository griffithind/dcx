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

// GenerateHomeResolutionScript returns a shell script that dynamically resolves
// user home directories using getent passwd. This is necessary because some container
// images have users with non-standard home directories (e.g., /var/lib/myapp).
//
// The script:
// 1. Creates /tmp/dcx-features/builtin.env
// 2. Uses getent passwd to look up actual home directories
// 3. Falls back to defaults if getent isn't available or user doesn't exist
// 4. Exports _CONTAINER_USER, _CONTAINER_USER_HOME, _REMOTE_USER, _REMOTE_USER_HOME
//
// This follows the devcontainer reference implementation pattern.
func GenerateHomeResolutionScript() string {
	return `mkdir -p /tmp/dcx-features && \
    echo "#!/bin/sh" > /tmp/dcx-features/builtin.env && \
    echo "# Dynamically resolved user home directories" >> /tmp/dcx-features/builtin.env && \
    if command -v getent >/dev/null 2>&1; then \
        _resolved_home=$(getent passwd "${_CONTAINER_USER}" 2>/dev/null | cut -d: -f6); \
        if [ -n "$_resolved_home" ]; then \
            echo "export _CONTAINER_USER_HOME=\"$_resolved_home\"" >> /tmp/dcx-features/builtin.env; \
        else \
            echo "export _CONTAINER_USER_HOME=\"${_CONTAINER_USER_HOME}\"" >> /tmp/dcx-features/builtin.env; \
        fi; \
        _resolved_home=$(getent passwd "${_REMOTE_USER}" 2>/dev/null | cut -d: -f6); \
        if [ -n "$_resolved_home" ]; then \
            echo "export _REMOTE_USER_HOME=\"$_resolved_home\"" >> /tmp/dcx-features/builtin.env; \
        else \
            echo "export _REMOTE_USER_HOME=\"${_REMOTE_USER_HOME}\"" >> /tmp/dcx-features/builtin.env; \
        fi; \
    else \
        echo "export _CONTAINER_USER_HOME=\"${_CONTAINER_USER_HOME}\"" >> /tmp/dcx-features/builtin.env; \
        echo "export _REMOTE_USER_HOME=\"${_REMOTE_USER_HOME}\"" >> /tmp/dcx-features/builtin.env; \
    fi && \
    echo "export _REMOTE_USER=\"${_REMOTE_USER}\"" >> /tmp/dcx-features/builtin.env && \
    echo "export _CONTAINER_USER=\"${_CONTAINER_USER}\"" >> /tmp/dcx-features/builtin.env`
}
