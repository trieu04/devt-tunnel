package cli

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os/exec"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/client"
)

type UsageError struct{ Message string }

func (e *UsageError) Error() string { return e.Message }

type daemonError struct{ error }

func (e *daemonError) Unwrap() error { return e.error }

// ExitCodes is the CLI contract; API kinds with the same outcome share a row.
// Kinds not yet emitted by the engine are included for future command groups.
var ExitCodes = []struct {
	Code  int
	Name  string
	Kinds []api.ErrorKind
}{
	{0, "", nil},
	{1, "failed", []api.ErrorKind{api.Stopped, api.Internal, api.LeaseRevoked}},
	{2, "usage", []api.ErrorKind{api.Unknown}},
	{3, "needs_secret", []api.ErrorKind{api.NeedsSecret, api.KeyringUnavailable}},
	{4, "needs_user", []api.ErrorKind{api.NeedsUser}},
	{5, "conflict", []api.ErrorKind{api.Conflict}},
	{6, "timeout", []api.ErrorKind{api.Timeout}},
	{7, "daemon", []api.ErrorKind{api.Incompatible}},
	{8, "not_found", []api.ErrorKind{api.NotFound}},
}

// errorStatus is the single mapping from Go/API errors to the CLI exit contract.
func errorStatus(err error) (int, string) {
	if err == nil {
		return 0, ""
	}
	var usage *UsageError
	if errors.As(err, &usage) {
		return 2, "usage"
	}
	var rpc *api.Error
	if errors.As(err, &rpc) {
		for _, row := range ExitCodes {
			for _, kind := range row.Kinds {
				if kind == rpc.Data.Kind {
					return row.Code, string(kind)
				}
			}
		}
		return 1, string(rpc.Data.Kind)
	}
	var timeout net.Error
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &timeout) && timeout.Timeout()) {
		return 6, "timeout"
	}
	var daemon *daemonError
	var connection *net.OpError
	if errors.As(err, &daemon) || errors.As(err, &connection) || client.TransportError(err) {
		return 7, "daemon"
	}
	if errors.Is(err, exec.ErrNotFound) {
		return 8, "not_found"
	}
	return 1, "failed"
}

func ExitCode(err error) int {
	code, _ := errorStatus(err)
	return code
}

// tunnelError adds optional machine-readable context without changing the exit kind.
type tunnelError struct {
	err    error
	tunnel string
	hint   string
}

func (e *tunnelError) Error() string { return e.err.Error() }
func (e *tunnelError) Unwrap() error { return e.err }

func (a *app) writeError(err error) {
	_, code := errorStatus(err)
	if a.json {
		detail := struct {
			Code    string `json:"code"`
			Message string `json:"message"`
			Tunnel  string `json:"tunnel,omitempty"`
			Hint    string `json:"hint,omitempty"`
		}{Code: code, Message: err.Error()}
		var tunnel *tunnelError
		if errors.As(err, &tunnel) {
			detail.Tunnel, detail.Hint = tunnel.tunnel, tunnel.hint
		}
		_ = encodeJSON(a.io.Err, map[string]any{"error": detail})
		return
	}
	fmt.Fprintln(a.io.Err, err)
}
