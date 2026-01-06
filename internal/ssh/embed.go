package ssh

// Embedded Linux binaries for cross-platform deployment.
// These are populated at build time using go:embed.
// If empty, the agent proxy will attempt to copy the current binary
// (which works when already running on Linux).

// To populate these, build with:
//   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o bin/dcx-linux-amd64 ./cmd/dcx
//   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o bin/dcx-linux-arm64 ./cmd/dcx
// Then rebuild the main binary.

// For now, these are empty placeholders.
// The agent_proxy.go code handles the case when these are empty
// by copying the current executable (works on Linux).

var dcxLinuxAmd64 []byte
var dcxLinuxArm64 []byte

// Note: To enable embedding, uncomment the following and ensure binaries exist:
//
// //go:embed bin/dcx-linux-amd64
// var dcxLinuxAmd64 []byte
//
// //go:embed bin/dcx-linux-arm64
// var dcxLinuxArm64 []byte
