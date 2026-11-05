// Package api defines the shared client and engine types.
package api

import (
	"time"

	"github.com/trieu04/devt-tunnel/internal/askpass"
	"github.com/trieu04/devt-tunnel/internal/core/checks"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/sshconfig"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

type Endpoint struct {
	Kind   string `json:"kind"`
	Listen string `json:"listen"`
	Target string `json:"target,omitempty"`
	Public bool   `json:"public"`
}

type Actor = model.Actor
type Lease = model.Lease
type TunnelOwner = model.TunnelOwner

type TunnelView struct {
	Owner          TunnelOwner       `json:"owner"`
	Endpoints      []Endpoint        `json:"endpoints"`
	Tunnel         model.Tunnel      `json:"tunnel"`
	Status         supervisor.Status `json:"status"`
	Preview        string            `json:"preview"`
	MissingSecrets []string          `json:"missingSecrets"`
	ToolPath       string            `json:"toolPath,omitempty"`
}

type PendingPrompt struct {
	ID     string         `json:"id"`
	Prompt askpass.Prompt `json:"prompt"`
}

type Overview struct {
	ConfigPath        string               `json:"configPath"`
	ConfigError       string               `json:"configError"`
	Shortcut          string               `json:"shortcut"`
	ShortcutError     string               `json:"shortcutError"`
	Profiles          []model.Profile      `json:"profiles"`
	Tunnels           []TunnelView         `json:"tunnels"`
	Orphans           []supervisor.Record  `json:"orphans"`
	Prompts           []PendingPrompt      `json:"prompts"`
	ActiveProfile     string               `json:"activeProfile"`
	LastUsed          map[string]time.Time `json:"lastUsed"`
	KeychainAvailable bool                 `json:"keychainAvailable"`
	KeychainName      string               `json:"keychainName"`
}

type DraftResult struct {
	Preview    string           `json:"preview"`
	BuildError string           `json:"buildError"`
	Findings   []checks.Finding `json:"findings"`
}

type LogAppend struct {
	ID      string               `json:"id"`
	Lines   []supervisor.LogLine `json:"lines"`
	Replace bool                 `json:"replace,omitempty"`
}

type SSHHostList struct {
	ConfigPath string           `json:"configPath"`
	Hosts      []sshconfig.Host `json:"hosts"`
	Warnings   []string         `json:"warnings"`
}

type ToolInfo struct {
	Name    string `json:"name"`
	Path    string `json:"path"`
	Version string `json:"version"`
	Source  string `json:"source"`
	Error   string `json:"error"`
}

type SecretView struct {
	Name     string      `json:"name"`
	Kind     secret.Kind `json:"kind"`
	State    string      `json:"state"`
	UsedBy   []string    `json:"usedBy"`
	LastUsed string      `json:"lastUsed"`
}

const (
	EventTunnelsChanged = "tunnels.changed"
	EventPromptAdded    = "prompt.added"
)

type Event struct {
	Type   string         `json:"type"`
	Prompt *PendingPrompt `json:"prompt,omitempty"`
}
