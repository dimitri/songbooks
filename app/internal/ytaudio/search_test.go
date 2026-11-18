package ytaudio

import "testing"

// Titles and channels as YouTube listed them for real searches: the
// artist's own upload must win over fan channels, live takes and covers.
func TestParseSearchRanksTheArtistsUpload(t *testing.T) {
	out := []byte(`{"entries":[
		{"id":"aaaaaaaaaaa","title":"Gary Moore - Still Got The Blues [HD]","channel":"Gary Moore Society","duration":249},
		{"id":"bbbbbbbbbbb","title":"Still Got The Blues (Live from the Blues Alive Tour)","channel":"GaryMooreOfficial","duration":380},
		{"id":"ccccccccccc","title":"Still Got The Blues","channel":"GaryMooreOfficial","duration":250},
		{"id":"ddddddddddd","title":"Still Got The Blues - guitar lesson","channel":"Some Teacher","duration":900},
		{"id":"eeeeeeeeeee","title":"Still Got The Blues (cover)","channel":"Someone","duration":260}
	]}`)
	list, err := parseSearch(out, "Still Got The Blues", "Gary Moore")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "ccccccccccc" || !list[0].Official {
		t.Fatalf("best = %+v", list[0])
	}
	if list[1].ID != "bbbbbbbbbbb" || list[1].Note != "a live version" {
		t.Fatalf("second = %+v", list[1])
	}
	if list[len(list)-1].Note == "" {
		t.Fatalf("last should carry a note: %+v", list[len(list)-1])
	}
}

func TestParseSearchAccentsAndTopicChannels(t *testing.T) {
	out := []byte(`{"entries":[
		{"id":"fffffffffff","title":"Pour que tu m'aimes encore (Paroles)","channel":"Lyrics FR","duration":250},
		{"id":"ggggggggggg","title":"Céline Dion - Pour que tu m'aimes encore (Vidéo officielle remasterisée en HD)","channel":"Céline Dion","duration":250},
		{"id":"hhhhhhhhhhh","title":"Pour que tu m'aimes encore","channel":"Celine Dion - Topic","duration":253}
	]}`)
	list, err := parseSearch(out, "Pour que tu m'aimes encore", "Céline Dion")
	if err != nil {
		t.Fatal(err)
	}
	if list[0].ID != "ggggggggggg" || !list[1].Official || list[2].ID != "fffffffffff" {
		t.Fatalf("order = %s %s %s", list[0].ID, list[1].ID, list[2].ID)
	}
}

func TestContainsWord(t *testing.T) {
	if containsWord("i deliver", "live") || !containsWord("one live take", "live") || !containsWord("live", "live") {
		t.Fatal("word boundaries")
	}
}
