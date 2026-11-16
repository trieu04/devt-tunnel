package host

import (
	"slices"
	"time"

	"github.com/trieu04/devt-tunnel/internal/api"
)

// MCPCall is one row of the MCP history table. It comes from the audit trail,
// so it covers calls that change state or were refused, approved or denied;
// read-only calls such as tunnel_list are not recorded.
type MCPCall struct {
	Seq      uint64 `json:"seq"`
	Time     string `json:"time"` // RFC3339
	Client   string `json:"client"`
	Action   string `json:"action"`
	TunnelID string `json:"tunnelId"`
	Decision string `json:"decision"`
	Result   string `json:"result"`
	Count    uint64 `json:"count"` // coalesced policy.deny records, otherwise 0
}

// MCPHistory returns every retained MCP call, newest first. The audit files are
// size-bounded by rotation, so the list is bounded too; the UI paginates it.
func (s *Service) MCPHistory() ([]MCPCall, error) {
	out := []MCPCall{}
	var after uint64
	for {
		batch, err := call[api.AuditReadResult](s, "audit.read", api.AuditReadParams{Kind: "mcp", After: after, Limit: 1000})
		if err != nil {
			return nil, err
		}
		for _, e := range batch.Events {
			out = append(out, MCPCall{
				Seq: e.Sequence, Time: e.Time.Format(time.RFC3339), Client: e.Actor.Name, Action: string(e.Action),
				TunnelID: e.TunnelID, Decision: string(e.Decision), Result: string(e.Result), Count: e.Count,
			})
		}
		// Responses are also bounded by bytes, so a short page does not mean the
		// end: stop only when nothing new came back.
		if len(batch.Events) == 0 || batch.Next <= after {
			break
		}
		after = batch.Next
	}
	slices.Reverse(out)
	return out, nil
}
