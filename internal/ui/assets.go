package ui

import (
	_ "embed"
)

// demoDiagram is a real fal generation, kept so the scripted run can show the
// image step without a network call or a spend.
//
// Embedding it is what makes that step deterministic. A live generation takes
// two seconds when fal is healthy and the whole demo when it is not, and the
// picture would be different every time — the labels are the part an image
// model gets wrong, so a picture checked once is worth more than a fresh one.
//
//go:embed assets/wal-raft.jpg
var demoDiagram []byte

// demoDiagramURL is the key the image cache holds it under. Nothing fetches it:
// ui.Render looks in the cache first, so a URL that resolves to nowhere is the
// point rather than a problem.
const demoDiagramURL = "cogdebt://demo/wal-raft.jpg"
