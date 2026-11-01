package setlist

import (
	"reflect"
	"testing"
)

func TestRoundTrip(t *testing.T) {
	src := "title: Spring Rehearsal 2026\ndate: 2026-04-10\nvenue: The Loft\n\n" +
		"[Set 1]\n- Mercy/banshee\n  note: capo 2, watch the bridge tempo\n- OtherLies/Bones\n\n" +
		"[Set 2]\n- Mercy/mercy\n- Trio/sunny\n"
	first := Parse(src)
	again := Parse(Write(first))
	if !reflect.DeepEqual(first, again) {
		t.Errorf("round-trip mismatch:\n%+v\nvs\n%+v", first, again)
	}
	if len(first.Sets) != 2 || len(first.Sets[0].Entries) != 2 {
		t.Fatalf("unexpected structure: %+v", first)
	}
	if first.Sets[0].Entries[0].Note != "capo 2, watch the bridge tempo" {
		t.Errorf("note not parsed: %+v", first.Sets[0].Entries[0])
	}
	if first.Get("venue") != "The Loft" {
		t.Errorf("venue = %q", first.Get("venue"))
	}
}

func TestVariantEntryRoundTrip(t *testing.T) {
	src := "title: Practice list\n\n[Set 1]\n- variant:42\n  note: slow it down\n- Mercy/banshee\n"
	first := Parse(src)
	again := Parse(Write(first))
	if !reflect.DeepEqual(first, again) {
		t.Errorf("round-trip mismatch:\n%+v\nvs\n%+v", first, again)
	}
	if len(first.Sets) != 1 || len(first.Sets[0].Entries) != 2 {
		t.Fatalf("unexpected structure: %+v", first)
	}
	e := first.Sets[0].Entries[0]
	if !e.IsVariant() || e.VariantID != 42 || e.Note != "slow it down" {
		t.Errorf("variant entry not parsed correctly: %+v", e)
	}
	if first.Sets[0].Entries[1].Book != "Mercy" || first.Sets[0].Entries[1].IsVariant() {
		t.Errorf("book/stem entry not parsed correctly: %+v", first.Sets[0].Entries[1])
	}
}
