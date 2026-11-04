package host

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHostDoesNotDependOnEngine(t *testing.T) {
	out, err := exec.Command("go", "list", "-deps", ".").CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, out)
	}
	for _, dependency := range strings.Fields(string(out)) {
		if dependency == "github.com/trieu04/devt-tunnel/internal/engine" {
			t.Fatal("internal/host depends on internal/engine")
		}
	}
}
