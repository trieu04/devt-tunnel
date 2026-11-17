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

func TestToolsCandidatesTable(t *testing.T) {
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		if method != "tool.candidates" {
			return nil, api.NewError(api.Internal, "unexpected method "+method)
		}
		return []api.ToolCandidate{
			{Path: "/opt/homebrew/bin/cloudflared", Version: "2026.1.0", Origin: "homebrew", Auto: true, Selected: true},
			{Path: "/x/cloudflared", Origin: "path", Error: "boom"},
		}, nil
	})
	out := organizeSuccess(t, "", "tools", "candidates", "cloudflared")
	var result struct{ Candidates []api.ToolCandidate }
	if err := json.Unmarshal([]byte(out), &result); err != nil || len(result.Candidates) != 2 {
		t.Fatalf("candidates: %s (%v)", out, err)
	}
	if c := result.Candidates[0]; !c.Auto || !c.Selected || c.Origin != "homebrew" || result.Candidates[1].Error != "boom" {
		t.Fatalf("candidates: %+v", result.Candidates)
	}
}

func TestToolsDownloadAndRemove(t *testing.T) {
	var calls []string
	fakeDaemon(t, func(method string, params json.RawMessage) (any, *api.Error) {
		calls = append(calls, method+" "+string(params))
		switch method {
		case "tool.download":
			var p api.ToolDownloadParams
			_ = json.Unmarshal(params, &p)
			if p.Tool == "ngrok" {
				return nil, api.NewError(api.NotFound, "no ngrok download is available")
			}
			return []api.ToolInfo{{Name: "cloudflared", Path: "/tools/cloudflared/2026.1.0/cloudflared", Origin: "downloaded"}}, nil
		case "tool.removeDownloaded":
			return []api.ToolInfo{{Name: "cloudflared"}}, nil
		}
		return nil, api.NewError(api.Internal, "unexpected method "+method)
	})
	out := organizeSuccess(t, "", "tools", "download", "cloudflared")
	var result struct{ Tools []api.ToolInfo }
	if err := json.Unmarshal([]byte(out), &result); err != nil || len(result.Tools) != 1 || result.Tools[0].Origin != "downloaded" {
		t.Fatalf("download: %s (%v)", out, err)
	}
	organizeSuccess(t, "", "tools", "download", "cloudflared", "--no-use")
	organizeSuccess(t, "", "tools", "remove", "cloudflared", "2026.1.0")
	want := []string{
		`tool.download {"tool":"cloudflared","use":true}`,
		`tool.download {"tool":"cloudflared","use":false}`,
		`tool.removeDownloaded {"tool":"cloudflared","version":"2026.1.0"}`,
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Fatalf("calls:\n%s", strings.Join(calls, "\n"))
	}
	if code, _, diagnostics := organizeInvoke("", "tools", "download", "ngrok"); code != 8 || !strings.Contains(diagnostics, `"code":"not_found"`) {
		t.Errorf("ngrok: code=%d stderr=%s", code, diagnostics)
	}
	for _, args := range [][]string{
		{"tools", "download"}, {"tools", "download", "curl"},
		{"tools", "remove", "cloudflared"}, {"tools", "remove", "cloudflared", "../.."}, {"tools", "remove", "cloudflared", "latest"},
	} {
		code, out, diagnostics := organizeInvoke("", args...)
		if code != 2 || out != "" || !strings.Contains(diagnostics, `"code":"usage"`) {
			t.Errorf("%v: code=%d stdout=%s stderr=%s", args, code, out, diagnostics)
		}
	}
}

func TestDoctorSuggestsDownload(t *testing.T) {
	findings := doctorFindings(api.DaemonStatus{}, api.Overview{KeychainAvailable: true}, []api.ToolInfo{
		{Name: "cloudflared", Error: "cloudflared was not found on this machine", Download: &api.ToolDownloadOffer{Version: "2026.1.0", Size: 1}},
		{Name: "ngrok", Error: "ngrok was not found on this machine"},
	}, api.SSHHostList{})
	var cloudflared, ngrok string
	for _, f := range findings {
		switch f.Field {
		case "tools.cloudflared":
			cloudflared = f.Message
		case "tools.ngrok":
			ngrok = f.Message
		}
	}
	if !strings.Contains(cloudflared, "devt-tunnel tools download cloudflared") || strings.Contains(ngrok, "download") {
		t.Fatalf("findings: %q / %q", cloudflared, ngrok)
	}
}
