package importer

import "testing"

// "Pour que tu m'aimes encore" by "Céline Dion": GProTab.net finds it only
// without the apostrophe, and lists the artist as "Dion celine".
func TestGPSearchQueryDropsApostrophes(t *testing.T) {
	for in, want := range map[string]string{
		"Pour que tu m'aimes encore": "Pour que tu m aimes encore",
		"L’aventurier":               "L aventurier",
		"  Café  ":                   "Café",
	} {
		if got := gpSearchQuery(in); got != want {
			t.Errorf("gpSearchQuery(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMatchesGPQueryAccentsAndApostrophes(t *testing.T) {
	r := GPSearchResult{ArtistName: "Dion celine", SongName: "Pour que tu m aimes encore"}
	if !matchesGPQuery("Pour que tu m'aimes encore", "Céline Dion", r) {
		t.Errorf("an accented artist and an apostrophe in the title should match %+v", r)
	}
	if matchesGPQuery("Pour que tu m'aimes encore", "Jean-Jacques Goldman", r) {
		t.Errorf("another artist shouldn't match %+v", r)
	}
}

func TestNormalizeWordsFoldsAccents(t *testing.T) {
	got := normalizeWords("Céline — Où ça? C'est l’œuvre")
	want := []string{"celine", "ou", "ca", "c", "est", "l", "œuvre"}
	if len(got) != len(want) {
		t.Fatalf("normalizeWords = %q, want %q", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("normalizeWords = %q, want %q", got, want)
		}
	}
}
