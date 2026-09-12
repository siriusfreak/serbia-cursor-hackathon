// Command ext-github runs the GitHub scanner as a standalone plugin process.
//
// The plugin package itself is unchanged and unaware of this: it holds no host
// state and moves only bytes, so the same type serves in-process and here. That
// is the whole claim of the ABI, and this binary is the proof.
//
//	go build -o plugins/ext-github ./cmd/ext-github
//	go run ./cmd/cogdebt -cli -debug        # github_scan now runs out-of-process
package main

import (
	"github.com/sirius/cogdebt/exts/github"
	"github.com/sirius/cogdebt/internal/ext/subprocess"
)

func main() {
	subprocess.Serve(github.New())
}
