package webassets

import (
	"os/exec"
	"path/filepath"
	"testing"
)

// TestScripts runs the scripts' own tests (jstest/*.test.js, node's test
// runner) when node is installed; they test the pure parts of the page
// scripts, such as the Sync sheet's waveform arithmetic.
func TestScripts(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node not installed")
	}
	files, err := filepath.Glob("jstest/*.test.js")
	if err != nil || len(files) == 0 {
		t.Fatalf("no script tests: %v", err)
	}
	out, err := exec.Command(node, append([]string{"--test"}, files...)...).CombinedOutput()
	if err != nil {
		t.Fatalf("node --test: %v\n%s", err, out)
	}
}
