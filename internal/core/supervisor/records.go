package supervisor

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
)

// Record identifies a started process so that it can be found again after an app crash.
// Identity is the process's OS start time, which protects against PID reuse.
type Record struct {
	TunnelID string `json:"tunnelId"`
	PID      int    `json:"pid"`
	Identity string `json:"identity"`
}

// Records persists the processes the app currently owns.
type Records interface {
	Add(Record) error
	Remove(pid int) error
	List() ([]Record, error)
}

// FileRecords stores records as JSON in the state directory.
type FileRecords struct {
	Path string
	mu   sync.Mutex
}

func (f *FileRecords) List() ([]Record, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.read()
}

func (f *FileRecords) Add(r Record) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	recs, err := f.read()
	if err != nil {
		return err
	}
	return f.write(append(recs, r))
}

func (f *FileRecords) Remove(pid int) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	recs, err := f.read()
	if err != nil {
		return err
	}
	kept := recs[:0]
	for _, r := range recs {
		if r.PID != pid {
			kept = append(kept, r)
		}
	}
	return f.write(kept)
}

func (f *FileRecords) read() ([]Record, error) {
	data, err := os.ReadFile(f.Path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var recs []Record
	if err := json.Unmarshal(data, &recs); err != nil {
		return nil, err
	}
	return recs, nil
}

func (f *FileRecords) write(recs []Record) error {
	data, err := json.Marshal(recs)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(f.Path), 0o755); err != nil {
		return err
	}
	tmp := f.Path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, f.Path)
}

// FindOrphans splits records into processes that are still alive with the same identity.
// Records whose process is gone are stale and not returned.
func FindOrphans(recs []Record, identity func(pid int) (string, error)) []Record {
	var orphans []Record
	for _, r := range recs {
		if got, err := identity(r.PID); err == nil && got == r.Identity {
			orphans = append(orphans, r)
		}
	}
	return orphans
}

// RenameTunnel updates crash recovery records without touching the process.
func (f *FileRecords) RenameTunnel(from, to string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	recs, err := f.read()
	if err != nil {
		return err
	}
	for i := range recs {
		if recs[i].TunnelID == from {
			recs[i].TunnelID = to
		}
	}
	return f.write(recs)
}
