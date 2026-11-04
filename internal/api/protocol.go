package api

import (
	"encoding/json"
	"fmt"

	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/secret"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

const Version = 1
const MaxMessageSize = 4 * 1024 * 1024

type Request struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type Response struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *Error          `json:"error,omitempty"`
}

type ErrorKind string

const (
	Conflict           ErrorKind = "conflict"
	NeedsSecret        ErrorKind = "needs_secret"
	KeyringUnavailable ErrorKind = "keyring_unavailable"
	Stopped            ErrorKind = "stopped"
	Unknown            ErrorKind = "unknown"
	Internal           ErrorKind = "internal"
	Incompatible       ErrorKind = "incompatible"
	Timeout            ErrorKind = "timeout"
)

type ErrorData struct {
	Kind ErrorKind `json:"kind"`
}
type Error struct {
	Code    int       `json:"code"`
	Message string    `json:"message"`
	Data    ErrorData `json:"data"`
}

func (e *Error) Error() string { return e.Message }
func NewError(kind ErrorKind, message string) *Error {
	return &Error{Code: -32000, Message: message, Data: ErrorData{Kind: kind}}
}
func VersionError(found int) *Error {
	return NewError(Incompatible, fmt.Sprintf("daemon API %d is incompatible with client API %d; run dtt daemon restart (stops all tunnels)", found, Version))
}

type ClientInfo struct {
	Kind    string `json:"kind"`
	Name    string `json:"name"`
	Version string `json:"version"`
}
type HelloParams struct {
	APIVersion int        `json:"apiVersion"`
	Client     ClientInfo `json:"client"`
}
type HelloResult struct {
	APIVersion    int    `json:"apiVersion"`
	DaemonVersion string `json:"daemonVersion"`
	PID           int    `json:"pid"`
}
type DaemonStatus struct {
	HelloResult
	Active      bool                `json:"active"`
	Connections int                 `json:"connections"`
	Tunnels     []supervisor.Status `json:"tunnels"`
}
type Empty struct{}
type ShutdownParams struct {
	OnlyIfIdle  bool `json:"onlyIfIdle"`
	ExpectedPID int  `json:"expectedPid,omitempty"`
}
type IDParams struct {
	ID string `json:"id"`
}
type TunnelParams struct {
	Tunnel model.Tunnel `json:"tunnel"`
}
type SaveTunnelParams struct {
	Tunnel model.Tunnel `json:"tunnel"`
	Start  bool         `json:"start"`
}
type NewDraftParams struct {
	Kind model.TunnelType `json:"kind"`
}
type ImportParams struct {
	Line string `json:"line"`
}
type FavoriteParams struct {
	ID       string `json:"id"`
	Favorite bool   `json:"favorite"`
}
type LogsParams struct {
	ID       string `json:"id"`
	Sequence uint64 `json:"sequence"`
}
type LogsResult struct {
	Sequence uint64               `json:"sequence"`
	Lines    []supervisor.LogLine `json:"lines"`
}
type SaveProfileParams struct {
	Profile model.Profile `json:"profile"`
	Members []string      `json:"members"`
}
type AutostartParams struct {
	ID       string `json:"id"`
	TunnelID string `json:"tunnelId"`
	On       bool   `json:"on"`
}
type SecretNameParams struct {
	Name string `json:"name"`
}
type SetSecretParams struct {
	Name  string      `json:"name"`
	Value string      `json:"value"`
	Kind  secret.Kind `json:"kind"`
}
type RenameSecretParams struct {
	From string `json:"from"`
	To   string `json:"to"`
}
type AnswerPromptParams struct {
	ID        string `json:"id"`
	Value     string `json:"value"`
	Cancelled bool   `json:"cancelled"`
}
type SSHParams struct {
	Alias     string `json:"alias"`
	ProfileID string `json:"profileId"`
}
type ToolPathParams struct {
	Tool string `json:"tool"`
	Path string `json:"path"`
}
type ShortcutParams struct {
	Shortcut string `json:"shortcut"`
}
type UpdateChecksParams struct {
	Enabled bool `json:"enabled"`
}
type SettingsResult struct {
	Shortcut     string `json:"shortcut"`
	UpdateChecks bool   `json:"updateChecks"`
}
type OrphanParams struct {
	PID int `json:"pid"`
}

const (
	DaemonHello        = "daemon.hello"
	DaemonStatusMethod = "daemon.status"
	DaemonShutdown     = "daemon.shutdown"
	EventsSubscribe    = "events.subscribe"
)

var Methods = []string{
	DaemonHello, DaemonStatusMethod, DaemonShutdown, EventsSubscribe,
	"tunnel.list", "tunnel.get", "tunnel.save", "tunnel.delete", "tunnel.start", "tunnel.stop", "tunnel.restart", "tunnel.waitReady", "tunnel.evaluateDraft", "tunnel.import", "tunnel.newDraft", "tunnel.duplicateDraft", "tunnel.setFavorite", "tunnel.findings", "tunnel.recordUsed", "tunnel.startOnLaunch",
	"logs.read", "logs.since", "profile.list", "profile.save", "profile.delete", "profile.duplicate", "profile.activate", "profile.setAutostart", "profile.start", "profile.stop",
	"secret.list", "secret.set", "secret.delete", "secret.rename", "secret.forget", "prompt.list", "prompt.answer",
	"ssh.hosts", "ssh.resolve", "ssh.reload", "tool.list", "tool.setPath", "settings.get", "settings.setShortcut", "settings.setUpdateChecks", "orphan.list", "orphan.stop", "config.overview", "config.path", "config.reload", "system.wake", "system.focus",
}
