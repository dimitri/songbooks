package pdfnative

import (
	"reflect"
	"testing"
)

// TestPhraseRows: a section with no "bars:" line prints in 4-bar
// phrases, a lone last bar joining the row before it — the same rows as
// the web Song view (static/js/bar-rows.js).
func TestPhraseRows(t *testing.T) {
	want := map[int][]int{
		0: nil, 1: {1}, 2: {2}, 3: {3}, 4: {4}, 5: {5}, 6: {4, 2}, 7: {4, 3}, 8: {4, 4},
		9: {4, 5}, 10: {4, 4, 2}, 11: {4, 4, 3}, 12: {4, 4, 4}, 13: {4, 4, 5},
		14: {4, 4, 4, 2}, 15: {4, 4, 4, 3}, 16: {4, 4, 4, 4}, 17: {4, 4, 4, 5},
	}
	for n, w := range want {
		if got := phraseRows(n); !reflect.DeepEqual(got, w) {
			t.Errorf("phraseRows(%d) = %v, want %v", n, got, w)
		}
	}
}
