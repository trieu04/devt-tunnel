package mcpserver

import (
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestTypedDaemonErrorsThroughMCP(t *testing.T) {
	t.Run("invalid_spec", func(t *testing.T) {
		f := newFixture(t, nil)
		args := inline("allowed", strings.Repeat("x", 257))
		res, value := f.tool(t, "tunnel_open", args)
		if !res.IsError || value["code"] != "invalid_spec" {
			t.Fatalf("invalid spec: %#v", value)
		}
	})
	t.Run("port_in_use", func(t *testing.T) {
		f := newFixture(t, nil)
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		args := inline("allowed", "occupied")
		forward := args["spec"].(map[string]any)["forwards"].([]any)[0].(map[string]any)
		forward["listen_port"] = strconv.Itoa(listener.Addr().(*net.TCPAddr).Port)
		res, value := f.tool(t, "tunnel_open", args)
		if !res.IsError || value["code"] != "port_in_use" {
			t.Fatalf("occupied port: %#v", value)
		}
	})
	t.Run("tool_not_found", func(t *testing.T) {
		f := newFixture(t, nil)
		path := filepath.Join(t.TempDir(), "missing-ssh")
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0700); err != nil {
			t.Fatal(err)
		}
		if _, err := f.e.SetToolPath("ssh", path); err != nil {
			t.Fatal(err)
		}
		if err := os.Remove(path); err != nil {
			t.Fatal(err)
		}
		res, value := f.tool(t, "tunnel_open", inline("allowed", "missing-tool"))
		if !res.IsError || value["code"] != "tool_not_found" {
			t.Fatalf("missing tool: %#v", value)
		}
	})
}

func TestEndedLeaseRenewIsLeaseExpired(t *testing.T) {
	f := newFixture(t, nil)
	args := inline("allowed", "ended-lease")
	args["wait_seconds"] = 2
	res, opened := f.tool(t, "tunnel_open", args)
	good(t, res, opened)
	res, closed := f.tool(t, "tunnel_close", map[string]any{"lease_id": opened["lease_id"]})
	good(t, res, closed)
	res, value := f.tool(t, "tunnel_renew", map[string]any{"lease_id": opened["lease_id"]})
	if !res.IsError || value["code"] != "lease_expired" {
		t.Fatalf("ended lease did not retain lease error semantics: %#v", value)
	}
}
