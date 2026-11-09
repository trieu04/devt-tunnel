package state

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestRuntimeAtomicRoundTripAndPermissions(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	store := RuntimeStore{Path: path}
	st, err := store.Load()
	if err != nil || st.Version != 1 || len(st.Ephemeral) != 0 || len(st.Leases) != 0 {
		t.Fatalf("missing runtime=%+v %v", st, err)
	}
	st.Ephemeral = []model.Tunnel{{ID: "tmp-one", Name: "Temporary", Type: model.TypeSSH, SSH: &model.SSH{Host: "staging"}, Forwards: []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:20000", Target: "80"}}}}
	st.Leases = []model.Lease{{ID: "ls_one", TunnelID: "tmp-one", Holder: model.Actor{Kind: "mcp", Name: "adapter"}, Session: "session", ExpiresAt: time.Now().Add(time.Hour)}}
	st.Owners["tmp-one"] = RuntimeOwner{LeaseStarted: true}
	st.AutoPorts["tmp-one"] = []int{0}
	if err := store.Save(st); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	failed := make(chan error, 1)
	go func() {
		defer close(done)
		for i := 0; i < 50; i++ {
			if _, err := store.Load(); err != nil {
				failed <- err
				return
			}
		}
	}()
	for i := 0; i < 50; i++ {
		st.Ephemeral[0].Name = "Changed"
		if err := store.Save(st); err != nil {
			t.Fatal(err)
		}
	}
	<-done
	select {
	case err := <-failed:
		t.Fatalf("reader observed partial JSON: %v", err)
	default:
	}
	loaded, err := store.Load()
	if err != nil || len(loaded.Ephemeral) != 1 || len(loaded.Leases) != 1 || loaded.AutoPorts["tmp-one"][0] != 0 {
		t.Fatalf("loaded=%+v %v", loaded, err)
	}
	info, err := os.Stat(path)
	if err != nil || (runtime.GOOS != "windows" && info.Mode().Perm() != 0600) {
		t.Fatalf("permissions=%v %v", info, err)
	}
	matches, err := filepath.Glob(filepath.Join(filepath.Dir(path), ".state-*.tmp"))
	if err != nil || len(matches) > 0 {
		t.Fatalf("temporary files leaked: %+v %v", matches, err)
	}
}
func TestRuntimeRejectsCorruptVersionAndNamespaces(t *testing.T) {
	for name, data := range map[string]string{
		"corrupt": "{", "version": `{"version":2}`, "saved id": `{"version":1,"ephemeral":[{"id":"saved"}]}`,
		"duplicate":        `{"version":1,"ephemeral":[{"id":"tmp-x"},{"id":"tmp-x"}]}`,
		"unallocated port": `{"version":1,"ephemeral":[{"id":"tmp-x","forwards":[{"listen_port":"auto"}]}]}`,
	} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "runtime.json")
			if err := os.WriteFile(path, []byte(data), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := (RuntimeStore{Path: path}).Load(); err == nil {
				t.Fatal("accepted invalid runtime")
			}
		})
	}
}
