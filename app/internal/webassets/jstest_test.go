package webassets

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScripts runs the scripts' own tests (jstest/*.test.mjs and
// jstest/*.test.js, Node's built-in runner): the parts of static/js that
// are plain logic, such as the song player's mix (player-mix.js) and the
// Sync sheet's waveform arithmetic. Skipped where Node isn't installed.
func TestScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	mjs, _ := filepath.Glob("jstest/*.test.mjs")
	js, _ := filepath.Glob("jstest/*.test.js")
	files := append(mjs, js...)
	if len(files) == 0 {
		t.Fatal("no script tests")
	}
	out, err := exec.Command(node, append([]string{"--test"}, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}
