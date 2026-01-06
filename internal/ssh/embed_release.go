//go:build embed

package ssh

import _ "embed"

// Release build: embed Linux binaries for cross-platform SSH agent forwarding.
// Build Linux binaries first, then build with -tags embed:
//
//   CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -o internal/ssh/bin/dcx-linux-amd64 ./cmd/dcx
//   CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -o internal/ssh/bin/dcx-linux-arm64 ./cmd/dcx
//   CGO_ENABLED=0 GOOS=darwin GOARCH=arm64 go build -tags embed -o dcx ./cmd/dcx

//go:embed bin/dcx-linux-amd64
var dcxLinuxAmd64 []byte

//go:embed bin/dcx-linux-arm64
var dcxLinuxArm64 []byte
