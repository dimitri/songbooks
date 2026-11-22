package store

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"testing"

	"app/internal/setlist"
)

// Set list items in a table (migration 0115, docs/ownership-model.md §4):
// the migration's parser agrees with internal/setlist's, the migration
// moves real-shaped bodies into rows and back to the same text, a row
// picks its version, and the history still reverts.

// realShapedBodies are set list bodies as staging has them (headers,
// sections, notes, an empty set, a blank line here and there), plus the
// edge cases the parser has to agree on. %[1]d, %[2]d, %[3]d are songs;
// 999999999 is a song that's gone.
var realShapedBodies = []string{
	"title: Concert · La Péniche Rouge\ndate: 2026-12-05\nvenue: La Péniche Rouge\n\n[Set 1]\n- variant:%[1]d\n- variant:%[2]d\n  note: capo 2, watch the bridge tempo\n\n[Set 2]\n- variant:%[3]d\n\n[Rappel]\n- variant:%[1]d\n",
	"title: Set court, 30 minutes\n\n[Set 1]\n- variant:%[2]d\n- variant:%[3]d\n",
	"[Set 1]\n- variant:%[1]d\n\n[Encore]\n",
	"# a comment\ntitle:   spaced out  \nvenue: Le Quai 9\nunknown: ignored\ndate: 2026-11-07\ntitle: the last title wins\n\n[Set 1]\r\n- variant: %[1]d \r\n  note:  slow it down  \r\n- variant:abc\n- variant:-4\n- variant:0\n  note: stays on the song before\n- variant:999999999\n  note: a song that's gone\n- Mercy/le-vent\n  note: a book/stem line's note\n\n[]\n- variant:%[2]d\nnote: no indent\n- variant:%[3]d\n# note: a comment, not a note\n[Set 3]\n",
	"",
	"title: no sets at all\n",
	"[Set 1]\n  - variant:%[1]d\n\t[Set 2]\t\n- variant:+%[2]d\n",
}

// parsedRows is setlist.Parse's reading of body as setlist_body_rows'
// rows: the header (title, date, venue), then a section row per set and a
// row per entry, a book/stem entry as 'other'.
func parsedRows(body string) []string {
	l := setlist.Parse(body)
	var out []string
	for _, k := range setlist.MetaKeys {
		for _, f := range l.Meta {
			if f.Key == k {
				out = append(out, fmt.Sprintf("%s %q", k, f.Value))
			}
		}
	}
	for _, s := range l.Sets {
		out = append(out, fmt.Sprintf("section %q", s.Label))
		for _, e := range s.Entries {
			if e.IsVariant() {
				out = append(out, fmt.Sprintf("song %d %q", e.VariantID, e.Note))
			} else {
				out = append(out, fmt.Sprintf("other %q %q", e.Book+"/"+e.Stem, e.Note))
			}
		}
	}
	return out
}

func TestSetlistBodyRowsMatchesParse(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	for i, tmpl := range realShapedBodies {
		body := fmt.Sprintf(tmpl, 11, 22, 33)
		rows, err := st.db.Query(ctx, `SELECT kind, label, variant_id, note FROM setlist_body_rows($1) ORDER BY pos`, body)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for rows.Next() {
			var kind string
			var label *string
			var id *int64
			var note string
			if err := rows.Scan(&kind, &label, &id, &note); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "song":
				got = append(got, fmt.Sprintf("song %d %q", *id, note))
			case "other":
				got = append(got, fmt.Sprintf("other %q %q", *label, note))
			case "section":
				got = append(got, fmt.Sprintf("section %q", *label))
			default:
				got = append(got, fmt.Sprintf("%s %q", kind, *label))
			}
		}
		rows.Close()
		if want := parsedRows(body); !reflect.DeepEqual(got, want) {
			t.Errorf("body %d:\n got %q\nwant %q", i, got, want)
		}
	}
}

// migration0115Moves is the part of migration 0115 that moves the bodies
// into rows: from its INSERT up to the body column's drop.
func migration0115Moves(t *testing.T) string {
	t.Helper()
	raw, err := migrationsFS.ReadFile("migrations/0115_setlist_items.sql")
	if err != nil {
		t.Fatal(err)
	}
	sql := string(raw)
	from := strings.Index(sql, "INSERT INTO setlist_items")
	to := strings.Index(sql, "ALTER TABLE setlists DROP COLUMN body")
	if from < 0 || to < from {
		t.Fatal("migration 0115's moves not found")
	}
	return sql[from:to]
}

// stripIDs is l without its rows' ids, to compare with a parsed one.
func stripIDs(l *setlist.Setlist) *setlist.Setlist {
	out := &setlist.Setlist{Meta: l.Meta}
	for _, s := range l.Sets {
		set := setlist.Set{Label: s.Label}
		for _, e := range s.Entries {
			e.ID = 0
			set.Entries = append(set.Entries, e)
		}
		out.Sets = append(out.Sets, set)
	}
	return out
}

// TestSetlistMigrationRoundTrip runs migration 0115's moves on real-shaped
// bodies (the body column put back for it, in a transaction rolled back
// after): each set list reads back as its body did — its header in the
// columns, its sets (an empty one too) and songs in order with their
// notes — but for the lines it can't hold (a deleted song's, a book/stem
// line); its text form parses back to the same set list.
func TestSetlistMigrationRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	p, err := st.CreatePerson(ctx, "mover", "Mover", "mover@example.com")
	if err != nil {
		t.Fatal(err)
	}
	band, err := st.CreateBand(ctx, "Mercy", p)
	if err != nil {
		t.Fatal(err)
	}
	var songs []any
	for _, title := range []string{"Bad Spell", "Le Vent", "Blue"} {
		id, err := st.CreateVariant(ctx, OwnerBand, band, title, "Mercy", p)
		if err != nil {
			t.Fatal(err)
		}
		songs = append(songs, id)
	}
	var ids []int64
	var bodies []string
	for i, tmpl := range realShapedBodies {
		id, err := st.CreateSetlist(ctx, "band", fmt.Sprintf("List %d", i), "", nil, &band, nil, p)
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
		bodies = append(bodies, fmt.Sprintf(tmpl, songs...))
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `ALTER TABLE setlists ADD COLUMN body text NOT NULL DEFAULT ''`); err != nil {
		t.Fatal(err)
	}
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE setlists SET body = $2, date = '', venue = '' WHERE id = $1`, id, bodies[i]); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := tx.Exec(ctx, migration0115Moves(t)); err != nil {
		t.Fatal(err)
	}

	for i, id := range ids {
		got, err := loadSetlist(ctx, tx, id)
		if err != nil {
			t.Fatal(err)
		}
		want := setlist.Parse(bodies[i])
		// What a set list can't hold: a gone song's line, a book/stem one.
		for si := range want.Sets {
			kept := want.Sets[si].Entries[:0]
			for _, e := range want.Sets[si].Entries {
				if e.IsVariant() && e.VariantID != 999999999 {
					kept = append(kept, e)
				}
			}
			want.Sets[si].Entries = kept
		}
		// The header: in title, date, venue order, the title the set
		// list's own when the body has none.
		meta := []setlist.MetaField{}
		title := want.Get("title")
		if title == "" {
			title = fmt.Sprintf("List %d", i)
		}
		for _, f := range []setlist.MetaField{{Key: "title", Value: title}, {Key: "date", Value: want.Get("date")}, {Key: "venue", Value: want.Get("venue")}} {
			if f.Value != "" {
				meta = append(meta, f)
			}
		}
		want.Meta = meta
		if len(want.Sets) == 0 {
			want.Sets = nil
		}
		if g := stripIDs(got); !reflect.DeepEqual(g, want) {
			t.Errorf("set list %d:\n got %+v\nwant %+v", i, g, want)
		}
		// Its text form reads back the same.
		if again := setlist.Parse(setlist.Write(got)); !reflect.DeepEqual(again, stripIDs(got)) {
			t.Errorf("set list %d's text doesn't read back:\n%s", i, setlist.Write(got))
		}
	}
	var gone int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM setlist_items WHERE variant_id = 999999999`).Scan(&gone); err != nil || gone != 0 {
		t.Errorf("a gone song's line: %d rows, %v", gone, err)
	}
}

// TestSetlistSaveRoundTrip: what's saved is what's read — sets (an empty
// one too), songs (twice is fine), notes, the date and venue — and the
// text a revision keeps reverts to it.
func TestSetlistSaveRoundTrip(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	l := &setlist.Setlist{
		Meta: []setlist.MetaField{{Key: "title", Value: "Saturday"}, {Key: "date", Value: "2026-12-05"}, {Key: "venue", Value: "Le Quai 9"}},
		Sets: []setlist.Set{
			{Label: "Set 1", Entries: []setlist.Entry{{VariantID: f.CatalogSong, Note: "capo 2"}, {VariantID: f.BandSong}}},
			{Label: "Set 2"},
			{Label: "Encore", Entries: []setlist.Entry{{VariantID: f.CatalogSong}}},
		},
	}
	meta, err := st.GetSetlistMeta(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, l, "Saturday", "bring the capo", strconv.Itoa(meta.Version), f.Core); err != nil {
		t.Fatal(err)
	}
	got, meta, _, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stripIDs(got), l) || meta.Title != "Saturday" || meta.Notes != "bring the capo" {
		t.Fatalf("read back:\n got %+v (%q, %q)\nwant %+v", stripIDs(got), meta.Title, meta.Notes, l)
	}
	for _, s := range got.Sets {
		for _, e := range s.Entries {
			if e.ID == 0 {
				t.Error("a song read without its row's id")
			}
		}
	}
	// A stale version is refused.
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, l, "Saturday", "", strconv.Itoa(meta.Version-1), f.Core); !errors.Is(err, ErrConflict) {
		t.Errorf("a stale save: %v", err)
	}

	// Empty it, then bring the version above back from the history.
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, &setlist.Setlist{}, "Saturday", "", "", f.Core); err != nil {
		t.Fatal(err)
	}
	revs, err := st.ListRevisions(ctx, "setlist", strconv.FormatInt(f.SetlistID, 10))
	if err != nil || len(revs) == 0 {
		t.Fatalf("history: %d, %v", len(revs), err)
	}
	if err := st.RevertRevision(ctx, revs[0].ID, f.Core); err != nil {
		t.Fatal(err)
	}
	back, _, _, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stripIDs(back), l) {
		t.Errorf("reverted:\n got %+v\nwant %+v", stripIDs(back), l)
	}

	// A revision recorded before migration 0115: the body as it was.
	old := fmt.Sprintf("title: Friday\n\n[Set 1]\n- variant:%d\n  note: old note\n- variant:999999999\n", f.BandSong)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(f.SetlistID, 10), &f.Core, SetlistSnapshot{
		Exists: true, Scope: "band", Title: "Friday", Body: old, OwnerBandID: &f.Band,
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	revs, _ = st.ListRevisions(ctx, "setlist", strconv.FormatInt(f.SetlistID, 10))
	if err := st.RevertRevision(ctx, revs[0].ID, f.Core); err != nil {
		t.Fatal(err)
	}
	back, meta, _, err = st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	want := &setlist.Setlist{
		Meta: []setlist.MetaField{{Key: "title", Value: "Friday"}},
		Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: f.BandSong, Note: "old note"}}}},
	}
	if !reflect.DeepEqual(stripIDs(back), want) || meta.Title != "Friday" {
		t.Errorf("an old body reverted:\n got %+v\nwant %+v", stripIDs(back), want)
	}
}

// TestSetlistPicksAVersion: the band's songbook has two versions of a
// song ("Night Drive", "Night Drive — psychedelic"): the set list's row
// offers both, plays the one picked (a revision kept, its click-track
// note along); a song of another work, a stale version, a heading row are
// refused. Making the band's version of a linked song puts it in the
// band's set lists.
func TestSetlistPicksAVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := NewBandFixture(t, st)
	book, err := st.GetDefaultSongbook(ctx, "band", f.Band)
	if err != nil {
		t.Fatal(err)
	}
	if err := st.AddToSongbook(ctx, book, f.BandSong, f.Core); err != nil {
		t.Fatal(err)
	}
	psych, err := st.MakeVersion(ctx, f.Guest, f.BandSong, BandOwner(f.Band), book, "psychedelic")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetClicktrackNote(ctx, f.SetlistID, f.BandSong, "count in slow", f.Core); err != nil {
		t.Fatal(err)
	}
	l, meta, version, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	if ids := SetlistVariantIDs(l); !reflect.DeepEqual(ids, []int64{f.BandSong, f.CatalogSong}) {
		t.Fatalf("a new version changed the set list: %v", ids)
	}
	row := l.Sets[0].Entries[0].ID
	choices, err := st.SetlistVersionChoices(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, v := range choices[row] {
		got = append(got, v.Title+"|"+v.VersionName)
	}
	if !reflect.DeepEqual(got, []string{"Night Drive|", "Night Drive|psychedelic"}) || len(choices) != 1 {
		t.Fatalf("choices: %v (rows %d)", got, len(choices))
	}

	if _, err := st.ChooseSetlistVersion(ctx, f.Core, f.SetlistID, row, f.CatalogSong, version); !errors.Is(err, ErrInvalid) {
		t.Errorf("another work's song: %v", err)
	}
	if _, err := st.ChooseSetlistVersion(ctx, f.Core, f.SetlistID, row, psych, strconv.Itoa(meta.Version-1)); !errors.Is(err, ErrConflict) {
		t.Errorf("a stale pick: %v", err)
	}
	newVersion, err := st.ChooseSetlistVersion(ctx, f.Core, f.SetlistID, row, psych, version)
	if err != nil {
		t.Fatal(err)
	}
	if newVersion == version {
		t.Error("the set list's version didn't move")
	}
	l, _, _, _ = st.LoadSetlistByID(ctx, f.SetlistID)
	if ids := SetlistVariantIDs(l); !reflect.DeepEqual(ids, []int64{psych, f.CatalogSong}) || l.Sets[0].Entries[0].ID != row {
		t.Errorf("after the pick: %v", ids)
	}
	notes, _ := st.ListClicktrackNotes(ctx, f.SetlistID)
	if notes[psych] != "count in slow" || notes[f.BandSong] != "" {
		t.Errorf("click-track notes: %v", notes)
	}
	revs, _ := st.ListRevisions(ctx, "setlist", strconv.FormatInt(f.SetlistID, 10))
	if len(revs) == 0 || !strings.Contains(string(revs[0].Snapshot), fmt.Sprintf("variant:%d", f.BandSong)) {
		t.Error("no revision of the set list before the pick")
	}
	if err := st.RevertRevision(ctx, revs[0].ID, f.Core); err != nil {
		t.Fatal(err)
	}
	if l, _, _, _ = st.LoadSetlistByID(ctx, f.SetlistID); SetlistVariantIDs(l)[0] != f.BandSong {
		t.Error("reverting the pick")
	}

	// The band's version of the linked catalogue song takes its place in
	// the band's set lists.
	mine, err := st.MakeVersion(ctx, f.Core, f.CatalogSong, BandOwner(f.Band), 0, "")
	if err != nil {
		t.Fatal(err)
	}
	if l, _, _, _ = st.LoadSetlistByID(ctx, f.SetlistID); !reflect.DeepEqual(SetlistVariantIDs(l), []int64{f.BandSong, mine}) {
		t.Errorf("the band's version in its set list: %v", SetlistVariantIDs(l))
	}
	holds, err := st.BandsHolding(ctx, f.Core, f.CatalogSong)
	if err != nil || len(holds) != 0 {
		t.Errorf("the band still holds the catalogue song: %+v, %v", holds, err)
	}
}
