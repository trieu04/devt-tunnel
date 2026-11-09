//go:build !windows

package audit

import "os"

// Open files do not block rename or unlink on Unix.
func openShared(path string) (*os.File, error) { return os.Open(path) }
