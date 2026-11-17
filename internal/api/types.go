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
	Groups            []model.Group        `json:"groups"`
	Tunnels           []TunnelView         `json:"tunnels"`
	Orphans           []supervisor.Record  `json:"orphans"`
	Prompts           []PendingPrompt      `json:"prompts"`
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
	Origin  string `json:"origin"`
	Error   string `json:"error"`
	// Download is set when devt-tunnel can download a pinned release of this
	// tool for this platform and the selected binary is not that version.
	Download *ToolDownloadOffer `json:"download,omitempty"`
}

// ToolDownloadOffer describes the compiled-in release a person may download.
type ToolDownloadOffer struct {
	Version string `json:"version"`
	Size    int64  `json:"size"`
}

// Download progress states.
const (
	DownloadDownloading = "downloading"
	DownloadVerifying   = "verifying"
	DownloadDone        = "done"
	DownloadFailed      = "failed"
	DownloadCancelled   = "cancelled"
)

// ToolDownloadProgress is the payload of EventToolDownload.
type ToolDownloadProgress struct {
	Tool     string `json:"tool"`
	Version  string `json:"version"`
	State    string `json:"state"` // downloading | verifying | done | failed | cancelled
	Received int64  `json:"received"`
	Total    int64  `json:"total"`
	Error    string `json:"error,omitempty"`
}

// ToolCandidate is one executable that could serve a tool.
type ToolCandidate struct {
	Path     string `json:"path"`     // absolute, as it will be stored
	Resolved string `json:"resolved"` // after EvalSymlinks; used to merge duplicates
	Version  string `json:"version"`  // first line of the version command; empty on error
	Origin   string `json:"origin"`   // path | homebrew | scoop | winget | system | downloaded | custom
	Auto     bool   `json:"auto"`     // what Auto would pick right now
	Selected bool   `json:"selected"` // equals the settings override
	Error    string `json:"error,omitempty"`
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
	EventApprovalAdded  = "approval.added"
	EventToolDownload   = "tool.download" // never delivered to MCP clients
)

type Event struct {
	Type       string                `json:"type"`
	ApprovalID string                `json:"approvalId,omitempty"`
	Prompt     *PendingPrompt        `json:"prompt,omitempty"`
	Download   *ToolDownloadProgress `json:"download,omitempty"`
}
