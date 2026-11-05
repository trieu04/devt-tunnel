package cli

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
	"github.com/trieu04/devt-tunnel/internal/core/supervisor"
)

func TestOrphansListRealDaemon(t *testing.T) {
	organizeDaemon(t, &model.Config{}, nil)
	out := organizeSuccess(t, "", "orphans")
	if !strings.Contains(out, `"orphans":[]`) {
		t.Fatalf("orphans=%s", out)
	}
}

func TestOrphansListAndStop(t *testing.T) {
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		switch method {
		case "orphan.list":
			return []supervisor.Record{{TunnelID: "db", PID: 12345, Identity: "process-start-time"}}, nil
		case "orphan.stop":
			var p api.OrphanParams
			if err := json.Unmarshal(params, &p); err != nil || p.PID != 12345 {
				t.Errorf("stop params=%s err=%v", params, err)
			}
			return api.Empty{}, nil
		default:
			return nil, api.NewError(api.Internal, "unexpected method "+method)
		}
	})
	out := organizeSuccess(t, "", "orphans")
	if !strings.Contains(out, `"pid":12345`) || !strings.Contains(out, "process-start-time") {
		t.Fatalf("orphans=%s", out)
	}
	organizeSuccess(t, "", "orphans", "stop", "12345")
	for _, pid := range []string{"0", "-1", "not-a-pid", "9999999999999999999999999999999999"} {
		code, out, diagnostics := organizeInvoke("", "orphans", "stop", "--", pid)
		if code != 2 || out != "" || !strings.Contains(diagnostics, `"code":"usage"`) {
			t.Errorf("pid=%q code=%d stdout=%s stderr=%s", pid, code, out, diagnostics)
		}
	}
}
