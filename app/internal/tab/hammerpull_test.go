package tab

import (
	"os"
	"testing"
)

func TestParseScore_HammerPull_AlwaysFree(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/always-free/verse.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, bar := range riff.Bars {
		for _, b := range bar.Beats {
			if b.HammerPull {
				found++
			}
		}
	}
	// "g,( a,) a, a, a,( g,)" — two slurs in the real file.
	if found != 2 {
		t.Errorf("found %d HammerPull beats, want 2", found)
	}
}

func TestParseScore_HammerPull_BadSpell(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/bad-spell/intro.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, bar := range riff.Bars {
		for _, b := range bar.Beats {
			if b.HammerPull {
				found++
			}
		}
	}
	// "bes16( c'4)" — one slur.
	if found != 1 {
		t.Errorf("found %d HammerPull beats, want 1", found)
	}
}

func TestParseScore_Slide_WhiskeyBeerAndWine(t *testing.T) {
	data, err := os.ReadFile("/Users/dim/dev/songbooks/Mercy/whiskey-beer-and-wine/intro.ly")
	if err != nil {
		t.Skip("legacy corpus not available:", err)
	}
	riff, err := ParseScore(string(data))
	if err != nil {
		t.Fatal(err)
	}
	found := 0
	for _, bar := range riff.Bars {
		for _, b := range bar.Beats {
			if b.HasSlideFrom {
				found++
				t.Logf("slide from fret %d into a note", b.SlideFromFret)
			}
		}
	}
	// "\grace f16( \glissando g8)" appears twice in the real file.
	if found != 2 {
		t.Errorf("found %d HasSlideFrom beats, want 2", found)
	}
}
