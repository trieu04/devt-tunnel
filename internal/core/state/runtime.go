package state

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

// Runtime is separate from shareable tunnel definitions. LeaseStarted survives
// the originating lease closing before the other holders finish.
type RuntimeOwner struct {
	Pinned       bool `json:"pinned,omitempty"`
	LeaseStarted bool `json:"leaseStarted,omitempty"`
}
type Runtime struct {
	Version   int                     `json:"version"`
	Ephemeral []model.Tunnel          `json:"ephemeral"`
	Leases    []model.Lease           `json:"leases"`
	Owners    map[string]RuntimeOwner `json:"owners"`
	AutoPorts map[string][]int        `json:"autoPorts,omitempty"`
	// Promotions is a write-ahead intent, allowing recovery across the two files.
	Promotions map[string]string `json:"promotions,omitempty"`
}

type RuntimeStore struct{ Path string }

func (s RuntimeStore) Load() (Runtime, error) {
	st := Runtime{Version: 1, Ephemeral: []model.Tunnel{}, Leases: []model.Lease{}, Owners: map[string]RuntimeOwner{}, AutoPorts: map[string][]int{}, Promotions: map[string]string{}}
	data, err := os.ReadFile(s.Path)
	if errors.Is(err, os.ErrNotExist) {
		return st, nil
	}
	if err != nil {
		return Runtime{}, err
	}
	if err := json.Unmarshal(data, &st); err != nil {
		return Runtime{}, err
	}
	ids := map[string]bool{}
	for _, t := range st.Ephemeral {
		if ids[t.ID] {
			return Runtime{}, fmt.Errorf("duplicate ephemeral id %q", t.ID)
		}
		ids[t.ID] = true
		if !strings.HasPrefix(t.ID, "tmp-") {
			return Runtime{}, fmt.Errorf("ephemeral id %q must start with tmp-", t.ID)
		}
		for _, f := range t.Forwards {
			if f.ListenPort != "" {
				return Runtime{}, errors.New("runtime.json contains an unresolved listen_port")
			}
		}
	}
	if st.Version != 1 {
		return Runtime{}, errors.New("unsupported runtime.json version")
	}
	if st.Owners == nil {
		st.Owners = map[string]RuntimeOwner{}
	}
	if st.AutoPorts == nil {
		st.AutoPorts = map[string][]int{}
	}
	if st.Promotions == nil {
		st.Promotions = map[string]string{}
	}
	return st, nil
}

func (s RuntimeStore) Save(st Runtime) error {
	data, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return err
	}
	return writeAtomic(s.Path, data)
}

// Clone gives engine transactions a private copy, including nested definitions.
func (st Runtime) Clone() Runtime {
	data, _ := json.Marshal(st)
	var out Runtime
	_ = json.Unmarshal(data, &out)
	if out.Owners == nil {
		out.Owners = map[string]RuntimeOwner{}
	}
	if out.AutoPorts == nil {
		out.AutoPorts = map[string][]int{}
	}
	if out.Promotions == nil {
		out.Promotions = map[string]string{}
	}
	return out
}
