// Package buildinfo holds the version string stamped in at link time
// (-ldflags -X, see scripts/build-artifacts.sh and .goreleaser.yaml).
package buildinfo

// Version is "dev" for a plain `go run`/`go build`.
var Version = "dev"
