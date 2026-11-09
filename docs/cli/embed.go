// Package clidocs contains the offline devt-tunnel guides shared by the CLI and website.
package clidocs

import "embed"

// Guides contains only hand-written guides; generated reference pages are excluded.
//
//go:embed *.md
var Guides embed.FS
