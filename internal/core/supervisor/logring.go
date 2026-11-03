package supervisor

import (
	"bytes"
	"sync"
)

// logRing keeps the last lines of a tunnel's output and reports each complete line.
type logRing struct {
	mu      sync.Mutex
	lines   []string
	max     int
	partial []byte
	onLine  func(string)
}

func newLogRing(max int) *logRing { return &logRing{max: max} }

// Write implements io.Writer for the child's stdout and stderr.
func (r *logRing) Write(p []byte) (int, error) {
	r.mu.Lock()
	r.partial = append(r.partial, p...)
	var complete []string
	for {
		i := bytes.IndexByte(r.partial, '\n')
		if i < 0 {
			break
		}
		complete = append(complete, string(bytes.TrimRight(r.partial[:i], "\r")))
		r.partial = r.partial[i+1:]
	}
	for _, line := range complete {
		r.appendLocked(line)
	}
	onLine := r.onLine
	r.mu.Unlock()
	if onLine != nil {
		for _, line := range complete {
			onLine(line)
		}
	}
	return len(p), nil
}

// Note adds a line from the supervisor itself.
func (r *logRing) Note(line string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.appendLocked("[devt-tunnel] " + line)
}

func (r *logRing) appendLocked(line string) {
	r.lines = append(r.lines, line)
	if over := len(r.lines) - r.max; over > 0 {
		r.lines = append(r.lines[:0], r.lines[over:]...)
	}
}

func (r *logRing) setOnLine(f func(string)) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.onLine = f
}

func (r *logRing) snapshot() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.lines...)
}
