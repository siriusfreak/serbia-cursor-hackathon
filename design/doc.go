// Package design holds the source for the design canvas: one .dc.html file per
// artboard, plus canvas.json for the layout.
//
// The canvas itself is assembled by the /design skill's seeder and is a build
// artifact (gitignored) — these files are what gets edited.
//
// design_test.go checks the artboards against the running app's own theme, so
// a design that drifts from the code, or code that drifts from the design,
// fails the build rather than being discovered on a screenshot.
package design
