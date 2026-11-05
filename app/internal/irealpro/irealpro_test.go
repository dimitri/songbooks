package irealpro

import "testing"

// realForumTuneURL is one real "irealb://" single-tune link, fetched
// live from an actual iReal Pro forum thread during this session (a
// Pop/RnB library post), used here to validate the unscrambling cipher
// and field parsing against real data rather than only synthetic
// input. It carries chord symbols and bar structure only, the same
// scope UG chord-grid fixtures elsewhere in this repo already use.
const realForumTuneURL = `irealb://ABC=The Jackson 5 (Mizell-Perren-Richards-Gordy)==RnB=Ab==1r34LbKcu7rQyXZ4Ab DLbA/bD bAZLQyXr|QyXZL x ZLbA/bZ x L4TA*{bD bAZ[*BSZLbD bA|ZLQyX|QryXZL x ZLbD bA x LZLQyX|bD bAAb  sQyX|rQyXZLQ lcQKyXbAl1N44T|,bD }|N243TZL |r ZLZAb ZL x ZL n l44T,|bDs  bA43TZLbD r| L x ZL x ZLDbLZ Q{Y QyXZ  x >aodC la .S.D<ZL xAb Db bAZLx >edDbLZTa taepeR<|QyXblA44T|,bDs  bA43nd Fa bAZL  } =Pop-RnB=93=1`

func TestParseURLRealForumTune(t *testing.T) {
	tunes, err := ParseURL(realForumTuneURL)
	if err != nil {
		t.Fatalf("ParseURL: %v", err)
	}
	if len(tunes) != 1 {
		t.Fatalf("got %d tunes, want 1", len(tunes))
	}
	tune := tunes[0]

	// The real field-of-interest check: this specific forum thread's
	// links carry an unexplained "ABC=" leading field (see parseTune's
	// doc comment) — the point of this test is confirming the
	// backward-from-chords heuristic still recovers the real title/
	// style/key rather than misreading "ABC" as the title.
	if tune.Title != "The Jackson 5 (Mizell-Perren-Richards-Gordy)" {
		t.Errorf("Title = %q, want %q", tune.Title, "The Jackson 5 (Mizell-Perren-Richards-Gordy)")
	}
	if tune.Style != "RnB" {
		t.Errorf("Style = %q, want %q", tune.Style, "RnB")
	}
	if tune.Key != "Ab" {
		t.Errorf("Key = %q, want %q", tune.Key, "Ab")
	}
	if len(tune.Measures) == 0 {
		t.Fatal("got no measures")
	}
	// Spot-check a few early measures against the cleaned chord string
	// by eye (Ab, Db/Ab, Db/Ab...) — real chord symbols, not garbage.
	t.Logf("measures: %v", tune.Measures)
	for _, m := range tune.Measures[:4] {
		if m == "" {
			t.Errorf("unexpected empty measure among the first 4: %v", tune.Measures[:4])
		}
	}
}

func TestUnscramble50RoundTrip(t *testing.T) {
	// unscramble50 is its own inverse (a pure swap, applying it twice on
	// the same positions returns the original) — a property check that
	// doesn't depend on knowing real scrambled content.
	block := "01234567890123456789012345678901234567890123456789"
	once := unscramble50(block)
	twice := unscramble50(once)
	if twice != block {
		t.Errorf("unscramble50 is not self-inverse: got %q after two passes, want %q", twice, block)
	}
	if once == block {
		t.Error("unscramble50 did not change a block with distinct characters")
	}
}

func TestFillSingleDoubleRepeats(t *testing.T) {
	measures := []string{"C", "x", "G", "Am", "r"}
	got := fillSingleDoubleRepeats(measures)
	want := []string{"C", "C", "G", "Am", "G", "Am"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("index %d: got %q, want %q", i, got[i], want[i])
		}
	}
}
