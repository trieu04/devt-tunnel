package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestSavedTunnelRejectsTemporaryNamespaceAndUnresolvedPort(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tunnels.toml")
	tunnel := model.Tunnel{ID: "tmp-example", Name: "Temporary", Type: model.TypeSSH, SSH: &model.SSH{Host: "staging"}}
	if err := Save(path, model.Config{Tunnels: []model.Tunnel{tunnel}}); err == nil || !strings.Contains(err.Error(), "reserved tmp-") {
		t.Fatalf("saved temporary id: %v", err)
	}
	data := `[[tunnel]]
id = "tmp-manual"
name = "Manual"
type = "ssh"
[tunnel.ssh]
host = "staging"
`
	if err := os.WriteFile(path, []byte(data), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path); err == nil {
		t.Fatal("accepted hand-edited reserved id")
	}
	tunnel.ID = "saved"
	tunnel.Forwards = []model.Forward{{Kind: model.ForwardLocal, Bind: "127.0.0.1:20000", ListenPort: "auto", Target: "80"}}
	if err := Save(path, model.Config{Tunnels: []model.Tunnel{tunnel}}); err == nil {
		t.Fatal("saved unresolved ephemeral-only request")
	}
}
