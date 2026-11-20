package webassets

import (
	"os/exec"
	"testing"
)

// TestScripts runs the scripts' own tests (jstest/*.test.mjs, Node's
// built-in runner): the parts of static/js that are plain logic, such as
// the song player's mix (player-mix.js). Skipped where Node isn't
// installed.
func TestScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	cmd := exec.Command(node, "--test", "jstest/*.test.mjs")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("node --test jstest/*.test.mjs: %v\n%s", err, out)
	}
}
