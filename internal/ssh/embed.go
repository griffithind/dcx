//go:build !embed

package ssh

// Default build: empty placeholders for Linux binaries.
// When running on Linux, the current executable is used as the container binary.
// When running on macOS without embedded binaries, SSH agent forwarding
// will look for dcx-linux-* binaries next to the executable.
//
// To build with embedded binaries (for release), use: go build -tags embed
// after placing Linux binaries in internal/ssh/bin/

var dcxLinuxAmd64 []byte
var dcxLinuxArm64 []byte
