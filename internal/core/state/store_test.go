package state

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestStoreUpdateAndCorruptFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "state.json")
	s := NewStore(path)
	if got := s.Get(); len(got.Windows) != 0 {
		t.Fatalf("initial: %+v", got)
	}
	now := time.Now().UTC().Truncate(time.Second)
	if err := s.Update(func(st *State) {
		st.LastUsed = map[string]time.Time{"a": now}
		st.Windows = map[string]Bounds{"main": {X: 1, Y: 2, Width: 800, Height: 600}}
	}); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if !got.LastUsed["a"].Equal(now) || got.Windows["main"].Width != 800 {
		t.Fatalf("state: %+v", got)
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Fatalf("mode=%v err=%v", info, err)
	}
	if err := os.WriteFile(path, []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}
	if got := s.Get(); len(got.Windows) != 0 {
		t.Fatalf("corrupt state not empty: %+v", got)
	}
	if err := s.Update(func(st *State) { st.Windows = map[string]Bounds{"recovered": {}} }); err != nil {
		t.Fatal(err)
	}
	if _, ok := s.Get().Windows["recovered"]; !ok {
		t.Fatalf("recovered state: %+v", s.Get())
	}
}
