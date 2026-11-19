package main

import (
	"math"
	"strconv"
	"strings"
	"testing"

	"app/internal/brand"
)

func TestPersonNamesDistinct(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < len(firstNames)*len(lastNames); i++ {
		n := PersonName(i)
		if j, ok := seen[n]; ok {
			t.Fatalf("PersonName(%d) = PersonName(%d) = %q", i, j, n)
		}
		seen[n] = i
		if n == lucasName || n == coachName {
			t.Fatalf("PersonName(%d) is a reserved name %q", i, n)
		}
	}
	if PersonName(42) != PersonName(42) || PersonName(1) == PersonName(2) {
		t.Fatal("PersonName is not deterministic")
	}
}

func TestSongTitlesDistinct(t *testing.T) {
	seen := map[string]int{}
	for i := 0; i < 1600; i++ {
		s := SongTitle(i)
		if j, ok := seen[s]; ok {
			t.Fatalf("SongTitle(%d) = SongTitle(%d) = %q", i, j, s)
		}
		seen[s] = i
	}
	for i, want := range []string{SongTitle(0), SongTitle(1), SongTitle(2), SongTitle(3)} {
		if got := SongTitle(i); got != want {
			t.Fatalf("SongTitle(%d) changed: %q then %q", i, want, got)
		}
	}
	// The four shapes take turns.
	if !strings.Contains(SongTitle(1), " to ") || !strings.Contains(SongTitle(2), " at ") {
		t.Fatalf("unexpected shapes: %q, %q", SongTitle(1), SongTitle(2))
	}
}

func TestBandNamesDistinct(t *testing.T) {
	seen := map[string]bool{}
	for i := range bandNames {
		n := BandName(i)
		if seen[n] {
			t.Fatalf("band name %q twice", n)
		}
		seen[n] = true
	}
	if BandName(len(bandNames)) != BandName(0) {
		t.Fatal("BandName should wrap around")
	}
}

func TestCommunityNames(t *testing.T) {
	for from, to := range knownCommunities {
		if got := CommunityName(from, 3); got != to {
			t.Errorf("CommunityName(%q) = %q, want %q", from, got, to)
		}
		if got := CommunityName(to, 3); got != to {
			t.Errorf("a renamed community should keep its name: %q became %q", to, got)
		}
	}
	if a, b := CommunityName("Some Club", 9), CommunityName("Some Club", 9); a != b || a == "Some Club" {
		t.Errorf("an unknown community should get a stable invented name, got %q and %q", a, b)
	}
}

func TestInitials(t *testing.T) {
	for in, want := range map[string]string{
		"The Paper Lanterns": "PL", "Velvet Hour": "VH", "Les Fanfarons": "FA", "Nord Express": "NE",
		"Les Oiseaux de Nuit": "ON",
	} {
		if got := Initials(in); got != want {
			t.Errorf("Initials(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLessonTitles(t *testing.T) {
	if LessonTitle(2) != "Shuffle en la" || LessonTitle(1+len(lessonTitles)) != LessonTitle(1) || LessonTitle(0) != LessonTitle(1) {
		t.Fatal("LessonTitle")
	}
	if MethodName(0) != "Méthode Blues, volume 1" || MethodName(1) != "Méthode Blues, volume 2" {
		t.Fatal("MethodName")
	}
}

// Looks: no logo, no website, survive brand.Clean, and readable.
func TestLooks(t *testing.T) {
	var looks []brand.Brand
	for name := range namedLooks {
		looks = append(looks, Look(name, 0))
	}
	for i := 0; i < len(palettes); i++ {
		looks = append(looks, Look(BandName(i), i))
	}
	for _, b := range looks {
		if b.LogoURL != "" || b.Website != "" {
			t.Errorf("%s: a demo look has no logo and no website", b.Mark)
		}
		if c := brand.Clean(b); c != b {
			t.Errorf("%s: brand.Clean changes the look: %+v", b.Mark, c)
		}
		if n := len([]rune(b.Mark)); n != 2 {
			t.Errorf("mark %q: want two letters", b.Mark)
		}
		if b.Tagline == "" {
			t.Errorf("%s: no tagline", b.Mark)
		}
		if r := contrast(b.Text, b.Background); r < 4.5 {
			t.Errorf("%s: text on background %.1f:1", b.Mark, r)
		}
		if r := math.Max(contrast(b.Accent, "#ffffff"), contrast(b.Accent, "#000000")); r < 4.5 {
			t.Errorf("%s: accent button %.1f:1", b.Mark, r)
		}
		if r := contrast(b.Accent2, b.Header); r < 4.5 {
			t.Errorf("%s: tagline on header %.1f:1", b.Mark, r)
		}
	}
}

func contrast(a, b string) float64 {
	la, lb := lum(a), lum(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

func lum(hex string) float64 {
	ch := func(s string) float64 {
		v, _ := strconv.ParseUint(s, 16, 8)
		c := float64(v) / 255
		if c <= 0.03928 {
			return c / 12.92
		}
		return math.Pow((c+0.055)/1.055, 2.4)
	}
	h := strings.TrimPrefix(hex, "#")
	return 0.2126*ch(h[0:2]) + 0.7152*ch(h[2:4]) + 0.0722*ch(h[4:6])
}
