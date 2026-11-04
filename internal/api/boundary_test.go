package api_test

import (
	"os/exec"
	"strings"
	"testing"
)

func TestHeadlessDependencyBoundary(t *testing.T) {
	cmd := exec.Command("go", "list", "-deps", "../api", "../engine", "../core/...", "../../cmd/...")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("go list: %v\n%s", err, output)
	}
	for _, dependency := range strings.Fields(string(output)) {
		name := strings.ToLower(dependency)
		if strings.Contains(name, "wailsapp") || strings.Contains(name, "wailsio") {
			t.Errorf("headless package depends on GUI library %s", dependency)
		}
	}
}
