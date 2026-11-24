package pdfnative

import (
	"reflect"
	"testing"

	"app/internal/tab"
)

func riffCover(n, covered int) []*tab.Bar {
	out := make([]*tab.Bar, n)
	for i := 0; i < covered; i++ {
		out[i] = &tab.Bar{}
	}
	return out
}

// The PDF cuts a section's rows at its riff's ends as the web chart does:
// a 4-bar riff over a 12-bar blues is one tab row and two grid rows; a
// 3-bar riff over a 7-bar grid one tab row of 3 and a grid row of 4; a
// riff covering the whole section, or none of it, leaves the rows be.
func TestSplitRiffRows(t *testing.T) {
	for _, c := range []struct {
		name     string
		rows     []barRow
		riff     []*tab.Bar
		explicit bool
		pickups  int
		want     []barRow
	}{
		{"12-bar blues, 4-bar riff", []barRow{{4, 4}, {4, 4}, {4, 4}}, riffCover(12, 4), false, 0, []barRow{{4, 4}, {4, 4}, {4, 4}}},
		{"7 bars, 3-bar riff", []barRow{{4, 4}, {3, 4}}, riffCover(7, 3), false, 0, []barRow{{3, 4}, {4, 4}}},
		{"8 bars, 6-bar riff", []barRow{{4, 4}, {4, 4}}, riffCover(8, 6), false, 0, []barRow{{4, 4}, {2, 4}, {2, 4}}},
		{"bars: 6, 3-bar riff", []barRow{{6, 6}, {2, 6}}, riffCover(8, 3), true, 0, []barRow{{3, 6}, {3, 6}, {2, 6}}},
		{"all riff", []barRow{{4, 4}, {4, 4}}, riffCover(8, 8), false, 0, []barRow{{4, 4}, {4, 4}}},
		{"no riff", []barRow{{4, 4}}, riffCover(4, 0), false, 0, []barRow{{4, 4}}},
	} {
		if got := splitRiffRows(c.rows, c.riff, c.explicit, c.pickups); !reflect.DeepEqual(got, c.want) {
			t.Errorf("%s: %v, want %v", c.name, got, c.want)
		}
	}
	// A pickup and a 3-bar riff: the riff row holds the pickup + 2 bars
	// (the pickup is the riff's first bar), the other 6 bars make grid
	// rows of 4 + 2 (a short last row).
	got := splitRiffRows([]barRow{{5, 4}, {4, 4}}, riffCover(9, 3), false, 1)
	want := []barRow{{3, 4}, {4, 4}, {2, 4}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("pickup: %v, want %v", got, want)
	}
}
