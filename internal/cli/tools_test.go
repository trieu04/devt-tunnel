package cli

import (
	"encoding/json"
	"runtime"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/api"
	"github.com/trieu04/devt-tunnel/internal/core/model"
)

func TestToolsListRealDaemon(t *testing.T) {
	organizeDaemon(t, &model.Config{}, nil)
	out := organizeSuccess(t, "", "tools")
	var result struct{ Tools []api.ToolInfo }
	if err := json.Unmarshal([]byte(out), &result); err != nil || len(result.Tools) != 3 {
		t.Fatalf("tools: %s (%v)", out, err)
	}
	if result.Tools[0].Name != "ssh" || result.Tools[1].Name != "ngrok" || result.Tools[2].Name != "cloudflared" {
		t.Fatalf("tools: %+v", result.Tools)
	}
	for _, args := range [][]string{{"tools", "set", "ssh", "--clear"}, {"tools", "set", "ssh", ""}} {
		organizeSuccess(t, "", args...)
	}
}

func TestToolsSetParameters(t *testing.T) {
	toolPath := "/test/ssh"
	if runtime.GOOS == "windows" {
		toolPath = `C:\test\ssh`
	}
	var paths []api.ToolPathParams
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method != "tool.setPath" {
			return nil, api.NewError(api.Internal, "unexpected method "+method)
		}
		var p api.ToolPathParams
		if err := json.Unmarshal(params, &p); err != nil {
			t.Error(err)
		}
		paths = append(paths, p)
		return []api.ToolInfo{{Name: p.Tool, Path: p.Path, Source: "override", Version: "test-version"}}, nil
	})
	for _, args := range [][]string{{"tools", "set", "ssh", toolPath}, {"tools", "set", "ssh", ""}, {"tools", "set", "ssh", "--clear"}} {
		organizeSuccess(t, "", args...)
	}
	if len(paths) != 3 || paths[0].Path != toolPath || paths[0].Tool != "ssh" || paths[1].Path != "" || paths[2].Path != "" {
		t.Fatalf("params: %+v", paths)
	}
	for _, args := range [][]string{
		{"tools", "set", "ssh"}, {"tools", "set", "unsupported", "/test/tool"},
		{"tools", "set", "ssh", "relative"}, {"tools", "set", "ssh", toolPath, "--clear"},
	} {
		code, out, diagnostics := organizeInvoke("", args...)
		if code != 2 || out != "" || !strings.Contains(diagnostics, `"code":"usage"`) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}
