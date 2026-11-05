// Package redact replaces exact known secret values in output destined for an
// MCP client. Callers must supply only values for secrets named by that tunnel;
// this package never fetches secrets itself.
//
// Matching is byte-exact, leftmost-longest, and does not rescan replacement
// markers. Identical values use the lexicographically first secret name. Empty
// values and values shorter than MinSecretBytes are ignored (see Ignored).
//
// This is not general sensitive-data detection. Unknown values, base64 or URL
// encodings, hashes, case changes, character-by-character/partial echoes and
// other transformations are not caught. Nor can a stream detect input omitted
// by log retention, or join different streams. Keep one stream per tunnel and
// preserve source order. Redacted process output is still untrusted data.
package redact

import (
	"errors"
	"sort"
	"strings"
	"sync"
)

// MinSecretBytes avoids replacing common tiny strings throughout ordinary logs.
// This is a byte length, not a rune count. Short secrets are NOT protected.
const MinSecretBytes = 4

var (
	ErrClosed   = errors.New("redact: stream is closed")
	ErrSequence = errors.New("redact: expected the next consecutive sequence number")
)

// IgnoredSecret records a name, never its value.
type IgnoredSecret struct {
	Name   string
	Reason IgnoreReason
}

type IgnoreReason string

const (
	IgnoreEmpty IgnoreReason = "empty"
	IgnoreShort IgnoreReason = "short"
)

type node struct {
	children map[byte]*node
	name     string
	terminal bool
	// candidate is the longest secret below this node, then first name.
	candidate string
	length    int
}

// Redactor is immutable and safe for concurrent use. It retains the supplied
// values in its matching trie for its lifetime; do not log or serialize it.
type Redactor struct {
	root    node
	ignored []IgnoredSecret
}

func New(secrets map[string]string) *Redactor {
	r := &Redactor{}
	names := make([]string, 0, len(secrets))
	for name := range secrets {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		value := secrets[name]
		if len(value) < MinSecretBytes {
			reason := IgnoreShort
			if value == "" {
				reason = IgnoreEmpty
			}
			r.ignored = append(r.ignored, IgnoredSecret{Name: name, Reason: reason})
			continue
		}
		n := &r.root
		for i := 0; i < len(value); i++ {
			if n.children == nil {
				n.children = make(map[byte]*node)
			}
			if n.children[value[i]] == nil {
				n.children[value[i]] = &node{}
			}
			n = n.children[value[i]]
			if len(value) > n.length {
				n.candidate, n.length = name, len(value)
			}
		}
		if !n.terminal { // Names were sorted, so identical values are deterministic.
			n.name, n.terminal = name, true
		}
	}
	return r
}

// Ignored returns a copy of the sorted inventory of unprotected short secrets.
func (r *Redactor) Ignored() []IgnoredSecret {
	return append([]IgnoredSecret(nil), r.ignored...)
}

func marker(name string) string { return "[secret:" + name + "]" }

// match finds a longest complete match at the start of text. pending means the
// entire remaining text could extend to a longer match on the next Push.
func (r *Redactor) match(text []byte) (length int, name string, pending bool, candidate string) {
	n := &r.root
	for i, b := range text {
		n = n.children[b]
		if n == nil {
			return
		}
		if n.terminal {
			length, name = i+1, n.name
		}
		if i == len(text)-1 && len(n.children) != 0 {
			pending, candidate = true, n.candidate
		}
	}
	return
}

// Redact replaces complete matches in an isolated string. Use a Stream rather
// than calling Redact independently on consecutive writes/lines: independent
// calls cannot catch split values. Unlike Stream.Flush, an isolated string's
// incomplete trailing prefix is left unchanged.
func (r *Redactor) Redact(text string) string {
	input := []byte(text)
	var out strings.Builder
	for i := 0; i < len(input); {
		length, name, _, _ := r.match(input[i:])
		if length == 0 {
			out.WriteByte(input[i])
			i++
		} else {
			out.WriteString(marker(name))
			i += length
		}
	}
	return out.String()
}

// Line is a source record, compatible in shape with supervisor.LogLine without
// depending on supervisor. It can also represent a raw write with an assigned
// sequence number. Line delimiters are NOT inserted between records: a secret
// split over two consecutive source lines is therefore caught too. If a value
// actually includes a newline, supply that byte as part of the input.
type Line struct {
	Seq  uint64 `json:"seq"`
	Line string `json:"line"`
}

type pendingLine struct {
	seq uint64
	end uint64
	out strings.Builder
}

// Stream delays records whose suffix could still become a secret. A match
// crossing records is replaced in its FIRST record; consumed bytes disappear
// from later records, which can become empty. Each input record is eventually
// emitted once with its original sequence number, never amended after emission.
//
// Trade-off: even a long line is withheld if its last bytes are ambiguous.
// Unresolved input bytes are bounded by the longest secret, but completed text
// and record metadata are retained until that ambiguity resolves. Call Flush on
// an idle timeout and Close at EOF to bound latency (and empty-record buffering).
// There is deliberately no timer/goroutine or background callback in this core
// API. Flush conservatively masks an incomplete prefix rather than leaking it;
// this can redact innocent suffixes and cannot catch a value spanning a flush.
//
// The first sequence may be arbitrary (a retained log snapshot); later records
// MUST be consecutive. On a retention gap, Flush and start a NEW stream. Poll
// logs.since using the last ACCEPTED input sequence to avoid duplicate input,
// but advertise only the last EMITTED sequence to consumers. Do not advertise
// the raw supervisor high-water mark while a record is buffered. A caller must
// retain this state across polls; recreating it per request loses split matches.
// Methods are safe for concurrent use, but callers still own source ordering.
type Stream struct {
	mu      sync.Mutex
	r       *Redactor
	buffer  []byte
	lines   []*pendingLine
	pos     uint64
	total   uint64
	last    uint64
	started bool
	closed  bool
}

func (r *Redactor) NewStream() *Stream { return &Stream{r: r} }

// Push accepts one complete source record and returns only resolved records.
// An error does not accept the record or change the stream.
func (s *Stream) Push(line Line) ([]Line, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return nil, ErrClosed
	}
	if s.started && (s.last == ^uint64(0) || line.Seq != s.last+1) {
		return nil, ErrSequence
	}
	s.started, s.last = true, line.Seq
	s.total += uint64(len(line.Line))
	s.lines = append(s.lines, &pendingLine{seq: line.Seq, end: s.total})
	s.buffer = append(s.buffer, line.Line...)
	var result []Line
	s.drain(&result)
	i := 0
	for i < len(s.buffer) {
		length, name, pending, _ := s.r.match(s.buffer[i:])
		if pending {
			break
		}
		if length == 0 {
			s.lines[0].out.WriteByte(s.buffer[i])
			length = 1
		} else {
			s.lines[0].out.WriteString(marker(name))
		}
		i += length
		s.pos += uint64(length)
		s.drain(&result)
	}
	// Copy just the held suffix, not the entire last input backing array.
	s.buffer = append([]byte(nil), s.buffer[i:]...)
	return result, nil
}

func (s *Stream) drain(out *[]Line) {
	for len(s.lines) > 0 && s.lines[0].end <= s.pos {
		line := s.lines[0]
		*out = append(*out, Line{Seq: line.seq, Line: line.out.String()})
		s.lines[0] = nil
		s.lines = s.lines[1:]
	}
	if len(s.lines) == 0 {
		s.lines = nil
	}
}

// Flush resolves the held prefix conservatively, and emits every pending
// record. Call it on an idle timeout. Future Push calls begin a new matching
// boundary, but keep consecutive sequence numbering. Repeated Flush is empty.
func (s *Stream) Flush() []Line {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.flush()
}

func (s *Stream) flush() []Line {
	if len(s.buffer) != 0 {
		length, name, _, candidate := s.r.match(s.buffer)
		if length == len(s.buffer) {
			candidate = name // A complete shorter value wins over a speculative one.
		}
		s.lines[0].out.WriteString(marker(candidate))
		s.pos = s.total
		s.buffer = nil
	}
	var result []Line
	s.drain(&result)
	return result
}

// Close flushes pending records and prevents further input. It is idempotent.
func (s *Stream) Close() []Line {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	return s.flush()
}
