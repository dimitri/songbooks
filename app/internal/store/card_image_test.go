package store

import (
	"context"
	"reflect"
	"testing"

	"app/internal/brand"
	"app/internal/covers"
)

func coverSong(work int64, title, artist, source, image string) SongCover {
	sc := SongCover{WorkID: work, Title: title, Artist: artist, Looked: true}
	if image != "" {
		sc.Cover = &covers.Cover{Source: source, Image: image}
	}
	return sc
}

func works(ms []MosaicCover) []int64 {
	var out []int64
	for _, m := range ms {
		out = append(out, m.WorkID)
	}
	return out
}

// TestCoverChoices: album covers before video stills, each image once,
// then by title (case aside): the same order whatever the songbook's.
func TestCoverChoices(t *testing.T) {
	songs := []SongCover{
		coverSong(1, "zebra", "A", "itunes", "z.jpg"),
		coverSong(2, "Still", "B", "youtube", "still.jpg"),
		coverSong(3, "No cover", "C", "", ""),
		coverSong(4, "Alpha", "D", "deezer", "a.jpg"),
		coverSong(5, "Beta", "D", "itunes", "a.jpg"), // the same album as Alpha
		coverSong(6, "beta", "E", "itunes", "b.jpg"),
	}
	got := works(CoverChoices(songs))
	if want := []int64{4, 6, 1, 2}; !reflect.DeepEqual(got, want) {
		t.Fatalf("CoverChoices: %v, want %v", got, want)
	}
	// Shuffled, the same.
	rev := make([]SongCover, len(songs))
	for i, s := range songs {
		rev[len(songs)-1-i] = s
	}
	if got2 := works(CoverChoices(rev)); !reflect.DeepEqual(got2, []int64{4, 6, 1, 2}) {
		t.Fatalf("reversed: %v", got2)
	}
}

// TestCardMosaic: a full square, 2×2 on a card, 3×3 for what's picked,
// the picked order kept and covers no longer there skipped.
func TestCardMosaic(t *testing.T) {
	var choices []MosaicCover
	for i := int64(1); i <= 10; i++ {
		choices = append(choices, MosaicCover{WorkID: i, Cover: covers.Cover{Image: "x"}})
	}
	for _, c := range []struct {
		have, max int
		choice    CardImage
		want      []int64
	}{
		{10, 4, CardImage{}, []int64{1, 2, 3, 4}},
		{10, 9, CardImage{}, []int64{1, 2, 3, 4, 5, 6, 7, 8, 9}},
		{3, 4, CardImage{}, []int64{1}},
		{0, 4, CardImage{}, nil},
		{10, 4, CardImage{Mode: CardCoverMosaic}, []int64{1, 2, 3, 4}},
		{10, 4, CardImage{Mode: CardPicked, Works: []int64{7, 3, 99, 5, 1}}, []int64{7, 3, 5, 1}},
		{10, 4, CardImage{Mode: CardPicked, Works: []int64{9, 8, 7, 6, 5, 4, 3, 2, 1}}, []int64{9, 8, 7, 6, 5, 4, 3, 2, 1}},
		{10, 4, CardImage{Mode: CardPicked, Works: []int64{2, 99}}, []int64{2}},
	} {
		got := works(CardMosaic(choices[:c.have], c.choice, c.max))
		if !reflect.DeepEqual(got, c.want) {
			t.Errorf("CardMosaic(%d covers, %+v, %d) = %v, want %v", c.have, c.choice, c.max, got, c.want)
		}
	}
}

func TestCardImageClean(t *testing.T) {
	for _, c := range []struct{ in, want CardImage }{
		{CardImage{Mode: "bogus", Works: []int64{1}}, CardImage{}},
		{CardImage{Mode: CardLogo, Works: []int64{1}}, CardImage{Mode: CardLogo}},
		{CardImage{Mode: CardPicked, Works: []int64{3, 3, 0, -1, 1, 2, 4, 5, 6, 7, 8, 9, 10}}, CardImage{Mode: CardPicked, Works: []int64{3, 1, 2, 4, 5, 6, 7, 8, 9}}},
	} {
		if got := c.in.Clean(); !reflect.DeepEqual(got, c.want) {
			t.Errorf("Clean(%+v) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

// TestCardImageBesideBrand: the card image choice is kept beside the
// brand, saving the look (or resetting it) keeps it, and the brand
// doesn't see it.
func TestCardImageBesideBrand(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	pid, err := st.CreatePerson(ctx, "core", "Core", "core@example.com")
	if err != nil {
		t.Fatal(err)
	}
	bid, err := st.CreateBand(ctx, "The Band", pid)
	if err != nil {
		t.Fatal(err)
	}
	if c, err := st.GetCardImage(ctx, "band", bid); err != nil || !reflect.DeepEqual(c, CardImage{}) {
		t.Fatalf("a new band's choice: %+v, %v", c, err)
	}
	want := CardImage{Mode: CardPicked, Works: []int64{4, 2}}
	if err := st.SetCardImage(ctx, "band", bid, want); err != nil {
		t.Fatal(err)
	}
	if err := st.SetBrand(ctx, "band", bid, brand.Brand{Preset: "other-lies", Tagline: "Night"}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.GetCardImage(ctx, "band", bid); !reflect.DeepEqual(c, want) {
		t.Fatalf("after saving the look: %+v, want %+v", c, want)
	}
	b, _, err := st.GetBrand(ctx, "band", bid)
	if err != nil || b.Preset != "other-lies" || b.Tagline != "Night" {
		t.Fatalf("the brand: %+v, %v", b, err)
	}
	if err := st.SetBrand(ctx, "band", bid, brand.Brand{}); err != nil {
		t.Fatal(err)
	}
	if c, _ := st.GetCardImage(ctx, "band", bid); !reflect.DeepEqual(c, want) {
		t.Fatalf("after resetting the look: %+v", c)
	}
	if b, _, _ := st.GetBrand(ctx, "band", bid); !b.IsZero() {
		t.Fatalf("a reset brand isn't the app's look: %+v", b)
	}
	if err := st.SetCardImage(ctx, "band", bid+100, want); err != ErrNotFound {
		t.Fatalf("no such band: %v", err)
	}
}
