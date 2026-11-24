package store

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestSongNews: "Changes in your songs" — who sees what. Ann has the
// catalogue's song in her songbook and is in Mercy, whose songbook holds
// Mercy's song; Bob edits the catalogue's song, Carl (Mercy) Mercy's
// song. Ann sees both, newest first, with the summary, who, why she has
// it; nobody sees their own changes; a former member, an outsider, a
// change before she had the song, a pending catalogue edit (until it is
// approved), a song she can't open: nothing. Marked seen, the list is
// empty until the next change.
func TestSongNews(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, name, strings.ToLower(name)+"@example.com")
		must(err)
		return id
	}
	ann, bob, carl, fred, olga, admin := person("Ann"), person("Bob"), person("Carl"), person("Fred"), person("Olga"), person("Admin")
	must(st.SetAdmin(ctx, admin, true))

	cat, err := st.CreateVariant(ctx, OwnerCatalog, 0, "Le Vent", "Someone", admin)
	must(err)
	const v1 = "title: Le Vent\nartist: Someone\n\n[Verse]\ngrid: | Am | G | F | E7 |\n"
	_, err = st.SaveVariantRaw(cat, v1, "", admin)
	must(err)
	// A change before Ann had it: not hers to hear of.
	_, err = st.SaveSongEdit(ctx, SongSave{SongID: cat, Title: "Le Vent", Artist: "Someone", Body: strings.Replace(v1, "| E7 |", "| E |", 1), ActorID: bob, Summary: "before"})
	must(err)
	time.Sleep(10 * time.Millisecond)
	annBook, err := st.GetDefaultSongbook(ctx, "person", ann)
	must(err)
	must(st.AddToSongbook(ctx, annBook, cat, ann))

	mercy, err := st.CreateBand(ctx, "Mercy", carl)
	must(err)
	for _, p := range []int64{ann, fred} {
		_, err := st.AddBandMember(ctx, mercy, p, "core", carl)
		must(err)
	}
	must(st.RemoveBandMember(ctx, mercy, fred, carl))
	bandSong, err := st.CreateVariant(ctx, OwnerBand, mercy, "Night Drive", "Mercy", carl)
	must(err)
	_, err = st.SaveVariantRaw(bandSong, "title: Night Drive\n\n[Verse]\ngrid: | C | G |\n", "", carl)
	must(err)
	mercyBook, err := st.GetDefaultSongbook(ctx, "band", mercy)
	must(err)
	must(st.AddToSongbook(ctx, mercyBook, bandSong, carl))

	// A private song of Olga's that Ann's songbook holds (it can't
	// happen through the app: put there by hand), changed by Olga.
	olgaSong, err := st.CreateVariant(ctx, OwnerPerson, olga, "Secret", "Olga", olga)
	must(err)
	_, err = st.db.Exec(ctx, `INSERT INTO songbook_entries (songbook_id, variant_id) VALUES ($1, $2)`, annBook, olgaSong)
	must(err)
	_, err = st.SaveVariantRaw(olgaSong, "title: Secret\n\n[V]\n[C]la [G]la\n", "", olga)
	must(err)

	// Bob fixes a chord of the catalogue's song; Carl edits Mercy's; Ann
	// her own catalogue edit (not news to her).
	body := func(id int64) string {
		b, _, err := st.LoadVariantRaw(id)
		must(err)
		return b
	}
	_, err = st.SaveSongEdit(ctx, SongSave{SongID: cat, Title: "Le Vent", Artist: "Someone", Body: strings.Replace(body(cat), "| F |", "| Fmaj7 |", 1), ActorID: bob, Summary: "the F is a Fmaj7"})
	must(err)
	time.Sleep(10 * time.Millisecond)
	_, err = st.SaveVariantRaw(bandSong, strings.Replace(body(bandSong), "| C | G |", "| C | G | Am |", 1), "", carl)
	must(err)

	news := func(p int64) []SongNews {
		t.Helper()
		n, err := st.SongNewsFor(ctx, p, 0)
		must(err)
		return n
	}
	got := news(ann)
	if len(got) != 2 {
		t.Fatalf("Ann's news: %+v", got)
	}
	if n := got[0]; n.SongID != bandSong || n.Via != "band" || n.BandName != "Mercy" || n.ActorName != "Carl" || n.Summary != "" || n.Changes != 2 {
		t.Errorf("Ann's first news (Mercy's song, the latest; its first chart too, since she was a member): %+v", n)
	}
	if n := got[1]; n.SongID != cat || n.Via != "songbook" || n.ActorName != "Bob" || n.Summary != "the F is a Fmaj7" || n.Changes != 1 || !n.Catalogue {
		t.Errorf("Ann's second news (the catalogue's song, Bob's fix only): %+v", n)
	}
	for who, p := range map[string]int64{"Bob": bob, "Fred": fred, "Olga": olga, "Admin": admin} {
		for _, n := range news(p) {
			if n.SongID == cat || n.SongID == bandSong {
				t.Errorf("%s has news of song %d: %+v", who, n.SongID, n)
			}
		}
	}
	if n := news(carl); len(n) != 0 {
		t.Errorf("Carl, who made the change and doesn't have the catalogue's song: %+v", n)
	}

	// A pending catalogue edit: no news until it is approved.
	cur := body(cat)
	var version int
	must(st.db.QueryRow(ctx, `SELECT version FROM songs WHERE id = $1`, cat).Scan(&version))
	edit, err := st.HoldCatalogueEdit(ctx, CatalogueEditInput{SongID: cat, AuthorID: bob, BaseVersion: version, BaseBody: cur,
		Title: "Le Vent", Artist: "Someone", Body: strings.Replace(cur, "| G |", "| G7 |", 1), Summary: "G7", Reasons: []string{"newcomer"}})
	must(err)
	if n := news(ann); n[1].Changes != 1 || n[1].Summary != "the F is a Fmaj7" {
		t.Errorf("a pending edit is news: %+v", n[1])
	}

	// Seen: nothing left; the edit approved is news again, alone.
	got = news(ann)
	must(st.SeeSongNews(ctx, ann, got[0].At))
	if n := news(ann); len(n) != 0 {
		t.Errorf("news after seeing it: %+v", n)
	}
	time.Sleep(10 * time.Millisecond)
	must(st.ApproveCatalogueEdit(ctx, edit, admin, ""))
	got = news(ann)
	if len(got) != 1 || got[0].SongID != cat || got[0].Summary != "G7" || got[0].ActorName != "Bob" || got[0].Changes != 1 {
		t.Errorf("the approved edit's news: %+v", got)
	}
	// Seen up to an earlier time: never back.
	must(st.SeeSongNews(ctx, ann, got[0].At))
	must(st.SeeSongNews(ctx, ann, got[0].At.Add(-time.Hour)))
	if n := news(ann); len(n) != 0 {
		t.Errorf("seen moved back: %+v", n)
	}
}
