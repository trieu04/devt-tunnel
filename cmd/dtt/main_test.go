package main

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/trieu04/devt-tunnel/internal/core/secret"
)

func TestMain(m *testing.M) {
	// Only the compiled test binary can replace the OS credential store.
	if os.Getenv("DTT_PROCESS_TEST") == "1" {
		secretStore = func() secret.Store { return &secret.Memory{} }
		main()
		return
	}
	os.Exit(m.Run())
}

func TestVersionAndUsage(t *testing.T) {
	for _, tc := range []struct {
		args     []string
		code     int
		contains string
	}{{[]string{"version"}, 0, "API 1"}, {nil, 2, "usage:"}, {[]string{"daemon", "bad"}, 2, "unknown"}, {[]string{"daemon", "run", "--idle-exit", "-1s"}, 2, "invalid"}} {
		var out, err bytes.Buffer
		if got := run(tc.args, &out, &err); got != tc.code || !strings.Contains(out.String()+err.String(), tc.contains) {
			t.Fatalf("%v: %d %s %s", tc.args, got, &out, &err)
		}
	}
}
