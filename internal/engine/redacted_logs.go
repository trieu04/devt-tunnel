package engine

import (
	"crypto/sha256"
	"sync"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/redact"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

const redactionIdle = 250 * time.Millisecond
const redactedHistory = 1000

// tunnelRedaction belongs to the raw log ring, not to a connection or the latest
// editable definition. A stale process can still output an earlier injected
// value after its saved references change. We retain matching tries for all
// observed versions until that ring is removed, including across reconnects.
type tunnelRedaction struct {
	names    map[string]bool
	known    map[string]map[[32]byte]bool
	redactor *redact.Redactor
}

// setManagerConfig registers references BEFORE any presence/spawn secret reads.
// No value for an unrelated vault entry is ever fetched or retained here.
func (s *Engine) setManagerConfig(cfg model.Config) {
	s.redactionMu.Lock()
	if s.redactions == nil {
		s.redactions = map[string]*tunnelRedaction{}
	}
	defined := map[string]bool{}
	for _, t := range cfg.Tunnels {
		defined[t.ID] = true
		r := s.redactions[t.ID]
		if r == nil {
			r = &tunnelRedaction{names: map[string]bool{}, known: map[string]map[[32]byte]bool{}, redactor: redact.New(nil)}
			s.redactions[t.ID] = r
		}
		for _, name := range t.Secrets() {
			r.names[name] = true
		}
	}
	s.redactionMu.Unlock()
	s.manager.SetConfig(cfg)
	// Manager.SetConfig has stopped removed processes and discarded their rings.
	s.redactionMu.Lock()
	for id := range s.redactions {
		if !defined[id] {
			delete(s.redactions, id)
		}
	}
	s.redactionMu.Unlock()
}

// lifetimeSecrets invokes this on successful reads, before a value reaches the
// supervisor environment or askpass. The immutable trie is the only retained
// copy of the value; known stores digests just to deduplicate repeated reads.
func (s *Engine) observeRedactionSecret(name, value string) {
	digest := sha256.Sum256([]byte(value))
	s.redactionMu.Lock()
	defer s.redactionMu.Unlock()
	for _, r := range s.redactions {
		if !r.names[name] {
			continue
		}
		if r.known[name] == nil {
			r.known[name] = map[[32]byte]bool{}
		}
		if r.known[name][digest] {
			continue
		}
		r.redactor = r.redactor.WithSecrets(map[string]string{name: value})
		r.known[name][digest] = true
	}
}
func (s *Engine) renameRedaction(from, to string) {
	s.redactionMu.Lock()
	defer s.redactionMu.Unlock()
	if r := s.redactions[from]; r != nil {
		s.redactions[to] = r
		delete(s.redactions, from)
	}
}

// MCPLogReader is connection-local. Accepted input and emitted output cursors
// are distinct; caller-supplied cursors never advance the source cursor.
type MCPLogReader struct {
	mu   sync.Mutex
	logs map[string]*redactedLog
}
type redactedLog struct {
	stream            *redact.Stream
	redactor          *redact.Redactor
	accepted, emitted uint64
	lastInput         time.Time
	history           []supervisor.LogLine
}

func NewMCPLogReader() *MCPLogReader { return &MCPLogReader{logs: map[string]*redactedLog{}} }

func (s *Engine) RedactedLogsSince(reader *MCPLogReader, id string, after uint64) (api.LogsResult, error) {
	reader.mu.Lock()
	defer reader.mu.Unlock()
	// Refresh only current references. Historical values were captured when read
	// for their actual definition/execution, and remain in the daemon-owned trie.
	// A failure fetching any current value fails the whole read closed.
	for _, t := range s.manager.Config().Tunnels {
		if t.ID != id {
			continue
		}
		for _, name := range t.Secrets() {
			if _, err := s.secrets.Get(name); err != nil {
				return api.LogsResult{}, err
			}
		}
		break
	}
	// Capture the immutable matcher and raw input under the observer lock. A
	// newly injected value cannot enter output between the matcher snapshot and
	// the raw-ring snapshot, because observation precedes credential delivery.
	s.redactionMu.Lock()
	var r *redact.Redactor
	if current := s.redactions[id]; current != nil {
		r = current.redactor
	}
	state := reader.logs[id]
	if r == nil && state == nil {
		s.redactionMu.Unlock()
		return api.LogsResult{Lines: []supervisor.LogLine{}}, nil
	}
	if state == nil || (r != nil && state.redactor != r) {
		state = &redactedLog{stream: r.NewStream(), redactor: r}
		reader.logs[id] = state
	}
	_, raw := s.manager.LogsSince(id, state.accepted)
	s.redactionMu.Unlock()
	appendLines := func(lines []redact.Line) {
		for _, line := range lines {
			state.history = append(state.history, supervisor.LogLine{Seq: line.Seq, Line: line.Line})
			state.emitted = line.Seq
		}
		if len(state.history) > redactedHistory {
			state.history = append([]supervisor.LogLine(nil), state.history[len(state.history)-redactedHistory:]...)
		}
	}
	for _, line := range raw {
		if state.accepted != 0 && line.Seq != state.accepted+1 {
			appendLines(state.stream.Close())
			state.stream = state.redactor.NewStream()
		}
		lines, err := state.stream.Push(redact.Line{Seq: line.Seq, Line: line.Line})
		if err != nil {
			return api.LogsResult{}, err
		}
		state.accepted = line.Seq
		state.lastInput = s.clock.Now()
		appendLines(lines)
	}
	if len(raw) == 0 && s.clock.Now().Sub(state.lastInput) >= redactionIdle {
		appendLines(state.stream.Flush())
	}
	out := api.LogsResult{Sequence: state.emitted, Lines: []supervisor.LogLine{}}
	for _, line := range state.history {
		if line.Seq > after {
			out.Lines = append(out.Lines, line)
		}
	}
	return out, nil
}
