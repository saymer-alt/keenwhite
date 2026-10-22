package tests

// R12: CI workflow integrity — a real WDTT build failure must FAIL, and no
// workflow may mask compiler errors with output redirection + fallback echo.
// (Issue #20, task #11 stage D.)

import (
	"os"
	"strings"
	"testing"
)

func readWorkflow(t *testing.T, name string) string {
	t.Helper()
	sep := string(os.PathSeparator)
	b, err := os.ReadFile(".." + sep + ".github" + sep + "workflows" + sep + name)
	if err != nil {
		t.Fatalf("read workflow %s: %v", name, err)
	}
	return string(b)
}

func TestQEMUDoesNotMaskBuildErrors(t *testing.T) {
	s := readWorkflow(t, "qemu-synthetic.yml")
	masking := "2>/dev/null || echo"
	if strings.Contains(s, masking) {
		t.Fatalf("qemu workflow masks build errors with %q — a real WDTT build failure would pass silently", masking)
	}
	if !strings.Contains(s, "go_client") {
		t.Fatal("qemu workflow must exercise the imported WDTT go_client build")
	}
	if !strings.Contains(s, "transports/wdtt/go_client/go.mod") {
		t.Fatal("nested WDTT module must be built with its own Go toolchain version")
	}
}

func TestCINamesRequiredContext(t *testing.T) {
	s := readWorkflow(t, "ci.yml")
	if !strings.Contains(s, "offline-checks:") {
		t.Fatal("ci.yml must keep the offline-checks job name — it is the required status check on main")
	}
	if strings.Contains(s, "pull_request_target") {
		t.Fatal("pull_request_target is forbidden in KeenWhite workflows")
	}
}
