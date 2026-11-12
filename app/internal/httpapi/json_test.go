package httpapi

import (
	"strings"
	"testing"

	"app/internal/song"
)

// TestJSONKeepsChordDiagrams: the Chart editor saves by sending the whole
// song through this JSON shape, so anything it can't carry is deleted on
// save. chord: diagram lines used to be.
func TestJSONKeepsChordDiagrams(t *testing.T) {
	src := "title: T\nchord: Cm 3:X(1333)\n\n[Verse]\ngrid: | Cm | Cm |\n"
	out := song.Write(fromJSON(toJSON(song.Parse(src))))
	if !strings.Contains(out, "chord: Cm 3:X(1333)") {
		t.Fatalf("chord diagram lost in JSON round trip:\n%s", out)
	}
}
