// Package frontend exposes the embedded SPA assets shipped with
// otel-magnify. Consumers pass the returned fs.FS to
// server.WithStaticFS so any edition binary can serve the same UI.
package frontend

import (
	"embed"
	"fmt"
	"io/fs"
)

//go:embed dist
var dist embed.FS

// FS returns the embedded frontend distribution rooted at the dist
// directory. It panics if the embed is malformed, which is a compile-
// time invariant and should never occur at runtime.
func FS() fs.FS {
	sub, err := fs.Sub(dist, "dist")
	if err != nil {
		panic(fmt.Errorf("frontend: dist subtree unreadable: %w", err))
	}
	return sub
}
