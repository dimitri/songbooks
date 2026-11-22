package store

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"app/internal/setlist"
)

// ownershipFixture is staging's situation in small (docs/ownership-model.md
// §9): a catalogue; Mercy with one unchanged bandsongs copy (in its
// songbook with a key, in a set list, a recording matched to it, a
// member's visit) and one changed since; What's Up with The Final
// Countdown (the catalogue has its own, linked by Bands Camp) and What's
// Up (the catalogue has its own too, no decision); Chloé's coaching
// community with a chart of a catalogue song (in a student's songbook,
// with the student's key) and one the catalogue lacks; Florent's paid
// songbook (not shared) and an open one; a person's own song.
type ownershipFixture struct {
	admin, mercyCore, student, alice                 int64
	mercy, whatsUp, bandsCamp, chloe, florent        int64
	catLeVent, catBlue, catTFC, catWhatsUp, catHotel int64
	copyUnchanged, copyChanged                       int64
	wuTFC, wuWhatsUp                                 int64
	chloeHotel, chloeNew                             int64
	florentSong, florentOpenSong, aliceSong          int64
	mercyBook, bandsCampBook, studentBook            int64
	florentPaid, florentOpen                         int64
	mercySetlist, recording                          int64
}

func newOwnershipFixture(t *testing.T, st *Store) ownershipFixture {
	t.Helper()
	ctx := context.Background()
	var f ownershipFixture
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	id := func(v int64, err error) int64 {
		t.Helper()
		must(err)
		return v
	}
	person := func(name string) int64 {
		return id(st.CreatePerson(ctx, name, strings.ToUpper(name[:1])+name[1:], name+"@example.com"))
	}
	f.admin, f.mercyCore, f.student, f.alice = person("admin"), person("mercycore"), person("student"), person("alice")
	must(st.SetAdmin(ctx, f.admin, true))

	// The catalogue (ownerless, visibility 'global', as before the model).
	cat := func(title, artist string) int64 {
		return id(st.CreateVariant(ctx, OwnerCatalog, 0, title, artist, f.admin))
	}
	f.catLeVent, f.catBlue = cat("Le Vent", "Someone"), cat("Blue", "Somebody")
	f.catTFC, f.catWhatsUp, f.catHotel = cat("The Final Countdown", "Europe"), cat("What's Up", "4 Non Blondes"), cat("Hotel California", "Eagles")

	// Mercy: bandsongs copies of two catalogue songs, one changed since.
	f.mercy = id(st.CreateBand(ctx, "Mercy", f.mercyCore))
	f.mercyBook = id(st.GetDefaultSongbook(ctx, "band", f.mercy))
	must(st.AddBandCatalogEntry(ctx, f.mercy, f.catLeVent, f.mercyCore))
	must(st.AddBandCatalogEntry(ctx, f.mercy, f.catBlue, f.mercyCore))
	must(st.SetEntrySemitones(ctx, f.mercyBook, f.catLeVent, -2))
	band := f.mercy
	f.mercySetlist = id(st.CreateSetlist(ctx, "band", "Friday", "", nil, &band, nil, f.mercyCore))
	l := &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: f.catLeVent}, {VariantID: f.catBlue}}}}}
	_, err := st.SaveSetlistByID(ctx, f.mercySetlist, l, "Friday", "", "", f.mercyCore)
	must(err)
	var created bool
	f.copyUnchanged, created, err = st.AdoptIntoBand(ctx, f.mercy, f.catLeVent, 0)
	must(err)
	if !created {
		t.Fatal("no copy made")
	}
	f.copyChanged, _, err = st.AdoptIntoBand(ctx, f.mercy, f.catBlue, 0)
	must(err)
	_, version, err := st.LoadVariantRaw(f.copyChanged)
	must(err)
	_, err = st.SaveVariantRaw(f.copyChanged, "title: Blue\nartist: Somebody\n\n[Verse]\nAm G\n", version, f.mercyCore)
	must(err)
	var srcID int64
	must(st.db.QueryRow(ctx, `INSERT INTO band_recording_sources (band_id, provider, link, code, api_host) VALUES ($1, 'pcloud', 'l', 'c', 'h') RETURNING id`, f.mercy).Scan(&srcID))
	must(st.db.QueryRow(ctx, `
		INSERT INTO band_recordings (source_id, band_id, file_id, path, name, kind, variant_id)
		VALUES ($1, $2, 1, '/a', 'a.mp3', 'rehearsal', $3) RETURNING id`, srcID, f.mercy, f.copyUnchanged).Scan(&f.recording))
	_, err = st.db.Exec(ctx, `INSERT INTO song_visits (person_id, song_id) VALUES ($1, $2)`, f.mercyCore, f.copyUnchanged)
	must(err)

	// What's Up: its own charts of two catalogue songs.
	f.whatsUp = id(st.CreateBand(ctx, "What's Up", f.mercyCore))
	f.wuTFC = id(st.CreateVariant(ctx, OwnerBand, f.whatsUp, "The Final Countdown", "Europe", f.mercyCore))
	f.wuWhatsUp = id(st.CreateVariant(ctx, OwnerBand, f.whatsUp, "What's Up", "4 Non Blondes", f.mercyCore))

	// Bands Camp links the catalogue's The Final Countdown.
	f.bandsCamp = id(st.CreateCommunity(ctx, "Bands Camp", f.admin))
	f.bandsCampBook = id(st.GetDefaultSongbook(ctx, "community", f.bandsCamp))
	must(st.AddToSongbook(ctx, f.bandsCampBook, f.catTFC, f.admin))

	// Chloé: her chart of Hotel California (a copy of the catalogue's,
	// changed, with its own recording) in a student's songbook in the
	// student's key; a song the catalogue lacks.
	f.chloe = id(st.CreateCommunity(ctx, "Chloé Paul", f.admin))
	_, err = st.AddCommunityMember(ctx, f.chloe, f.student, "member", f.admin)
	must(err)
	f.chloeHotel = id(st.ForkVariant(ctx, f.catHotel, f.admin, "community", f.chloe))
	_, version, err = st.LoadVariantRaw(f.chloeHotel)
	must(err)
	_, err = st.SaveVariantRaw(f.chloeHotel, "title: Hotel California\nartist: Eagles\n\n[Verse]\nBm F#\n", version, f.admin)
	must(err)
	_, err = st.db.Exec(ctx, `
		INSERT INTO song_audio (variant_id, byte_size, sha256, data, category) VALUES ($1, 3, 'abc', '\x010203', 'youtube')`, f.chloeHotel)
	must(err)
	f.chloeNew = id(st.CreateCommunityVariant(ctx, f.chloe, "Pour que tu m'aimes encore", "Céline Dion", f.admin))
	f.studentBook = id(st.StudentSongbook(ctx, f.chloe, f.student, "Student's songs", f.admin))
	must(st.AddToSongbook(ctx, f.studentBook, f.chloeHotel, f.admin))
	must(st.SetEntrySemitones(ctx, f.studentBook, f.chloeHotel, 3))

	// Florent: a paid songbook (not shared) and an open one.
	f.florent = id(st.CreateCommunity(ctx, "Florent Passamonti", f.admin))
	f.florentPaid = id(st.CreateSongbook(ctx, "community", f.florent, "La méthode", false))
	must(st.SetSongbookShared(ctx, f.florentPaid, false))
	f.florentOpen = id(st.GetDefaultSongbook(ctx, "community", f.florent))
	f.florentSong = id(st.CreateCommunityVariant(ctx, f.florent, "Leçon 1", "Florent", f.admin))
	must(st.RemoveFromSongbook(ctx, f.florentOpen, f.florentSong))
	must(st.AddToSongbook(ctx, f.florentPaid, f.florentSong, f.admin))
	f.florentOpenSong = id(st.CreateCommunityVariant(ctx, f.florent, "Jam 1", "Florent", f.admin))

	f.aliceSong = id(st.CreateVariant(ctx, OwnerPerson, f.alice, "Mine", "Alice", f.alice))
	return f
}

func testOwnershipOptions(apply bool) OwnershipOptions {
	o := DefaultOwnershipOptions()
	o.Apply = apply
	return o
}

type songRow struct {
	exists         bool
	ownerKind      string
	ownerCommunity int64
	ownerBand      int64
	public         bool
	body           string
}

func loadSongRow(t *testing.T, st *Store, id int64) songRow {
	t.Helper()
	var r songRow
	err := st.db.QueryRow(context.Background(), `
		SELECT true, coalesce(owner_kind, ''), coalesce(owner_community_id, 0), coalesce(owner_band_id, 0), public, body
		FROM songs WHERE id = $1`, id).Scan(&r.exists, &r.ownerKind, &r.ownerCommunity, &r.ownerBand, &r.public, &r.body)
	if err != nil && !strings.Contains(err.Error(), "no rows") {
		t.Fatal(err)
	}
	return r
}

func entryOf(t *testing.T, st *Store, songbookID, variantID int64) (int, bool) {
	t.Helper()
	semis, in, err := st.SongbookEntryKey(context.Background(), songbookID, variantID)
	if err != nil {
		t.Fatal(err)
	}
	return semis, in
}

// TestMapOwnershipDryRunChangesNothing: without Apply the mapping reports
// the work and leaves every row as it was.
func TestMapOwnershipDryRunChangesNothing(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newOwnershipFixture(t, st)
	songs := countSongs(t, st)

	r, err := st.MapOwnership(ctx, testOwnershipOptions(false))
	if err != nil {
		t.Fatal(err)
	}
	if r.Nothing() || r.CatalogueGiven != 5 || len(r.BandCopies) != 1 || r.BandCopies[0].Removed != 1 {
		t.Fatalf("dry run report: given %d, copies %+v", r.CatalogueGiven, r.BandCopies)
	}
	if n := countSongs(t, st); n != songs {
		t.Errorf("%d songs after a dry run, %d before", n, songs)
	}
	if row := loadSongRow(t, st, f.catLeVent); row.ownerKind != "" || row.public {
		t.Errorf("the dry run gave the catalogue's song an owner: %+v", row)
	}
	if _, in := entryOf(t, st, f.mercyBook, f.copyUnchanged); !in {
		t.Error("the dry run moved Mercy's entry")
	}
}

// TestMapOwnership: each row of §9's table, then a second run with
// nothing to do.
func TestMapOwnership(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newOwnershipFixture(t, st)

	r, err := st.MapOwnership(ctx, testOwnershipOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	big := r.BigSongbookID
	var kind, name string
	if err := st.db.QueryRow(ctx, `SELECT kind, name FROM communities WHERE id = $1`, big).Scan(&kind, &name); err != nil || kind != "global" || name != "The Big Songbook" {
		t.Fatalf("The Big Songbook: %q %q %v", kind, name, err)
	}

	// global → The Big Songbook, public.
	for _, id := range []int64{f.catLeVent, f.catBlue, f.catWhatsUp} {
		if row := loadSongRow(t, st, id); row.ownerKind != "community" || row.ownerCommunity != big || !row.public {
			t.Errorf("catalogue song %d: %+v", id, row)
		}
	}
	// user → the person, private.
	if row := loadSongRow(t, st, f.aliceSong); row.ownerKind != "person" || row.public {
		t.Errorf("Alice's song: %+v", row)
	}

	// The unchanged copy goes; Mercy links the catalogue's song again,
	// in its key, in its set list, its recording and the visit with it.
	if row := loadSongRow(t, st, f.copyUnchanged); row.exists {
		t.Error("Mercy's unchanged copy is still there")
	}
	if semis, in := entryOf(t, st, f.mercyBook, f.catLeVent); !in || semis != -2 {
		t.Errorf("Mercy's songbook: catalogue song in %v, key %d (want -2)", in, semis)
	}
	sl, _, _, err := st.LoadSetlistByID(ctx, f.mercySetlist)
	if err != nil {
		t.Fatal(err)
	}
	if ids := SetlistVariantIDs(sl); len(ids) != 2 || ids[0] != f.catLeVent || ids[1] != f.copyChanged {
		t.Errorf("Mercy's set list: %v, want [%d %d]", ids, f.catLeVent, f.copyChanged)
	}
	revs, err := st.ListRevisions(ctx, "setlist", strconv.FormatInt(f.mercySetlist, 10))
	if err != nil || len(revs) == 0 {
		t.Errorf("no set list revision for the change: %v", err)
	}
	var recVariant int64
	if err := st.db.QueryRow(ctx, `SELECT variant_id FROM band_recordings WHERE id = $1`, f.recording).Scan(&recVariant); err != nil || recVariant != f.catLeVent {
		t.Errorf("Mercy's recording matched to %d, want %d (%v)", recVariant, f.catLeVent, err)
	}
	var visits int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM song_visits WHERE person_id = $1 AND song_id = $2`, f.mercyCore, f.catLeVent).Scan(&visits); err != nil || visits != 1 {
		t.Errorf("the visit: %d (%v)", visits, err)
	}
	// The changed copy stays: Mercy's version, private.
	if row := loadSongRow(t, st, f.copyChanged); row.ownerBand != f.mercy || row.public {
		t.Errorf("Mercy's changed copy: %+v", row)
	}

	// What's Up's songs move to the catalogue; The Final Countdown: the
	// catalogue's merged into What's Up's, Bands Camp linking it.
	for _, id := range []int64{f.wuTFC, f.wuWhatsUp} {
		if row := loadSongRow(t, st, id); row.ownerCommunity != big || row.ownerBand != 0 || !row.public {
			t.Errorf("What's Up's song %d: %+v", id, row)
		}
	}
	if row := loadSongRow(t, st, f.catTFC); row.exists {
		t.Error("the catalogue's The Final Countdown is still there")
	}
	if _, in := entryOf(t, st, f.bandsCampBook, f.wuTFC); !in {
		t.Error("Bands Camp doesn't link What's Up's The Final Countdown")
	}
	if _, in := entryOf(t, st, id64(t, st, "band", f.whatsUp), f.wuTFC); !in {
		t.Error("What's Up's songbook lost its song")
	}
	// What's Up — 4 Non Blondes: no decision, both kept, reported.
	if row := loadSongRow(t, st, f.catWhatsUp); !row.exists {
		t.Error("the catalogue's What's Up went without a decision")
	}
	if len(r.Duplicates) != 1 || !strings.Contains(r.Duplicates[0], "What's Up — 4 Non Blondes") {
		t.Errorf("duplicates: %q", r.Duplicates)
	}

	// Chloé's chart replaces the catalogue's (the old one in history), the
	// student's entry follows with the student's key, her copy goes; the
	// song the catalogue lacked moves into it.
	hotel := loadSongRow(t, st, f.catHotel)
	if !strings.Contains(hotel.body, "Bm F#") || !hotel.public || hotel.ownerCommunity != big {
		t.Errorf("the catalogue's Hotel California: %+v", hotel)
	}
	revs, err = st.ListRevisions(ctx, "variant", strconv.FormatInt(f.catHotel, 10))
	if err != nil || len(revs) != 1 || strings.Contains(string(revs[0].Snapshot), "Bm F#") {
		t.Errorf("Hotel California's history: %d revisions (%v)", len(revs), err)
	}
	if row := loadSongRow(t, st, f.chloeHotel); row.exists {
		t.Error("Chloé's copy is still there")
	}
	if semis, in := entryOf(t, st, f.studentBook, f.catHotel); !in || semis != 3 {
		t.Errorf("the student's songbook: catalogue song in %v, key %d (want 3)", in, semis)
	}
	var audio int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM song_audio WHERE variant_id = $1 AND category = 'youtube'`, f.catHotel).Scan(&audio); err != nil || audio != 1 {
		t.Errorf("Chloé's recording on the catalogue's song: %d (%v)", audio, err)
	}
	if row := loadSongRow(t, st, f.chloeNew); row.ownerCommunity != big || !row.public {
		t.Errorf("Chloé's song the catalogue lacked: %+v", row)
	}

	// Florent: the paid songbook closed, its song private, linked nowhere
	// else; the open one neither closed nor public.
	var closed, public bool
	if err := st.db.QueryRow(ctx, `SELECT closed, public FROM songbooks WHERE id = $1`, f.florentPaid).Scan(&closed, &public); err != nil || !closed || public {
		t.Errorf("Florent's paid songbook: closed %v, public %v (%v)", closed, public, err)
	}
	if err := st.db.QueryRow(ctx, `SELECT closed, public FROM songbooks WHERE id = $1`, f.florentOpen).Scan(&closed, &public); err != nil || closed || public {
		t.Errorf("Florent's open songbook: closed %v, public %v (%v)", closed, public, err)
	}
	for _, id := range []int64{f.florentSong, f.florentOpenSong} {
		if row := loadSongRow(t, st, id); row.ownerCommunity != f.florent || row.public {
			t.Errorf("Florent's song %d: %+v", id, row)
		}
	}
	var elsewhere int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM songbook_entries WHERE variant_id = $1 AND songbook_id <> $2`, f.florentSong, f.florentPaid).Scan(&elsewhere); err != nil || elsewhere != 0 {
		t.Errorf("the paid songbook's song is in %d other songbooks (%v)", elsewhere, err)
	}
	var copies int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM songs WHERE forked_from_id = $1`, f.florentSong).Scan(&copies); err != nil || copies != 0 {
		t.Errorf("the paid songbook's song has %d copies (%v)", copies, err)
	}

	// Nothing to do a second time.
	again, err := st.MapOwnership(ctx, testOwnershipOptions(true))
	if err != nil {
		t.Fatal(err)
	}
	if !again.Nothing() {
		t.Errorf("a second run had something to do: %+v", again)
	}
}

// id64 is ownerKind's default songbook.
func id64(t *testing.T, st *Store, ownerKind string, ownerID int64) int64 {
	t.Helper()
	id, err := st.GetDefaultSongbook(context.Background(), ownerKind, ownerID)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// TestSongIdentity: case, accents, a leading "The", "&" and punctuation
// don't make two songs different.
func TestSongIdentity(t *testing.T) {
	for _, c := range [][4]string{
		{"Still got The Blues", "Gary Moore", "still got the blues", "gary moore"},
		{"The Boxer", "Simon & Garfunkel", "Boxer", "Simon and Garfunkel"},
		{"Les Dingues et les paumés", "Hubert-Félix Thiéfaine", "les dingues et les paumes", "hubert felix thiefaine"},
	} {
		if a, b := songIdentity(c[0], c[1]), songIdentity(c[2], c[3]); a != b {
			t.Errorf("%q ≠ %q", a, b)
		}
	}
	if songIdentity("Wonderwall", "Oasis") == songIdentity("Wonderwall", "Ryan Adams") {
		t.Error("two artists' versions are the same song")
	}
}
