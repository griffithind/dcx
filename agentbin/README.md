# agentbin

`make build-agent` writes the gzip-compressed Linux dcx-agent binaries
here (`dcx-agent-linux-{amd64,arm64}.gz`); `agent-embed.go` embeds this
directory into the dcx binary. The `.gz` files are not committed.

Without them dcx still compiles (so `go build`, `go vet`, `go test` and
gopls work on a clean checkout), but `dcx up` reports that the agent
binaries were not built.
