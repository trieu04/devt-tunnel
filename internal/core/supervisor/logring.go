package supervisor

import (
	"bytes"
	"sync"
)

// logRing keeps the last lines of a tunnel's output and reports each complete line.
type logRing struct {
	mu         sync.Mutex
	lines      []string
	max        int
	partial    []byte
	onLine     func(string)
	seq        uint64
	lastOutput string
}

type LogLine struct {
	Seq  uint64 `json:"seq"`
	Line string `json:"line"`
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
		if line != "" {
			r.lastOutput = line
		}
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
	r.seq++
	r.lines = append(r.lines, line)
	if over := len(r.lines) - r.max; over > 0 {
		r.lines = append(r.lines[:0], r.lines[over:]...)
	}
}

func (r *logRing) lastMeaningful() string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lastOutput
}

func (r *logRing) snapshotSince(after uint64) (uint64, []LogLine) {
	r.mu.Lock()
	defer r.mu.Unlock()
	first := uint64(1)
	if uint64(len(r.lines)) <= r.seq {
		first = r.seq - uint64(len(r.lines)) + 1
	}
	start := 0
	if after >= first {
		start = int(after-first) + 1
	}
	if start > len(r.lines) {
		start = len(r.lines)
	}
	out := make([]LogLine, 0, len(r.lines)-start)
	for i := start; i < len(r.lines); i++ {
		out = append(out, LogLine{Seq: first + uint64(i), Line: r.lines[i]})
	}
	return r.seq, out
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
