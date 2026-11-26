package store

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"
	"testing"

	"app/internal/setlist"
)

// albumsFixture: NewBandFixture's band (Core edits its songbook and
// publishes; Guest is a member who doesn't edit), two more of its songs,
// a set list playing them, and its folder with two masters.
type albumsFixture struct {
	BandFixture
	Song2, Song3 int64
	Master1      int64 // Albums/1 - Night/01 - Night Drive_90_ FR-ABC-26-00001.wav
	Other        int64 // a rehearsal, on no album
}

func newAlbumsFixture(t *testing.T, st *Store) albumsFixture {
	t.Helper()
	ctx := context.Background()
	f := albumsFixture{BandFixture: NewBandFixture(t, st)}
	var err error
	if f.Song2, err = st.CreateVariant(ctx, OwnerBand, f.Band, "Paper Moon", "Other Lies", f.Core); err != nil {
		t.Fatal(err)
	}
	if f.Song3, err = st.CreateVariant(ctx, OwnerBand, f.Band, "Ice Rose", "Other Lies", f.Core); err != nil {
		t.Fatal(err)
	}
	// The band's live edits: its chart's tempo.
	for id, tempo := range map[int64]string{f.BandSong: "112", f.Song2: "100"} {
		body, version, err := st.LoadVariantRaw(id)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.SaveVariantRaw(id, setHeader(body, "tempo", tempo), version, f.Core); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := st.db.Exec(ctx, `INSERT INTO band_recording_sources (id, band_id, provider, link, code, api_host) VALUES (77, $1, 'pcloud', 'https://x', 'C', 'api.pcloud.com')`, f.Band); err != nil {
		t.Fatal(err)
	}
	rec := func(fileID int64, kind, path, name string, pos int) int64 {
		var id int64
		if err := st.db.QueryRow(ctx, `
			INSERT INTO band_recordings (source_id, band_id, file_id, path, name, kind, position, title)
			VALUES (77, $1, $2, $3, $4, $5, $6, '') RETURNING id`, f.Band, fileID, path, name, kind, pos).Scan(&id); err != nil {
			t.Fatal(err)
		}
		return id
	}
	f.Master1 = rec(1, "album", "Albums/1 - Night", "01 Other lies-Night-Night Drive_90_Mix FR-ABC-26-00001.wav", 1)
	rec(2, "album", "Albums/1 - Night", "02 Other lies-Night-Skins_98_Mix FR-ABC-26-00002.wav", 2)
	f.Other = rec(3, "rehearsal", "Répétitions/2026-01-10", "Night Drive.mp3", 1)
	return f
}

func TestAlbumsCRUDAndRights(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAlbumsFixture(t, st)

	in := AlbumInput{Title: "Night", Kind: "album", ReleasedOn: "2026-04-10", Label: "Eyelash",
		Links: []AlbumLink{{URL: "https://otherlies.bandcamp.com/album/night"}},
		Tracks: []AlbumTrackInput{{SongID: f.Song2, Title: "Paper Moons"}, {Title: "Interlude", DurationS: 61}, {SongID: f.BandSong, RecordingID: f.Master1}}}
	if _, err := st.CreateAlbum(ctx, f.Guest, f.Band, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a guest makes an album: %v", err)
	}
	if _, err := st.CreateAlbum(ctx, f.Outsider, f.Band, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("an outsider makes an album: %v", err)
	}
	id, err := st.CreateAlbum(ctx, f.Core, f.Band, in)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.CreateAlbum(ctx, f.Core, f.Band, AlbumInput{Title: "night"}); !errors.Is(err, ErrConflict) {
		t.Fatalf("the same title twice: %v", err)
	}
	a, err := st.GetAlbum(ctx, id)
	if err != nil {
		t.Fatal(err)
	}
	if a.ReleaseYear != 2026 || a.Label != "Eyelash" || len(a.Links) != 1 || a.Links[0].Kind != "bandcamp" || a.Links[0].Label() != "Bandcamp" {
		t.Fatalf("album: %+v", a)
	}
	var got []string
	for _, tr := range a.Tracks {
		got = append(got, tr.Title)
		if tr.Position != len(got) {
			t.Fatalf("track %q at %d", tr.Title, tr.Position)
		}
	}
	if strings.Join(got, ",") != "Paper Moons,Interlude,Night Drive" || a.Tracks[0].SongTitle != "Paper Moon" || a.Tracks[2].RecordingID == nil {
		t.Fatalf("tracks: %v %+v", got, a.Tracks)
	}

	// Reordered: the positions follow.
	in.Tracks = []AlbumTrackInput{in.Tracks[2], in.Tracks[0]}
	if err := st.SaveAlbum(ctx, f.Guest, id, in); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a guest saves: %v", err)
	}
	if err := st.SaveAlbum(ctx, f.Core, id, in); err != nil {
		t.Fatal(err)
	}
	a, _ = st.GetAlbum(ctx, id)
	if len(a.Tracks) != 2 || *a.Tracks[0].SongID != f.BandSong || a.Tracks[1].Position != 2 {
		t.Fatalf("reordered: %+v", a.Tracks)
	}
	// Not the band's song, a live version: refused.
	if err := st.SaveAlbum(ctx, f.Core, id, AlbumInput{Title: "Night", Tracks: []AlbumTrackInput{{SongID: f.CatalogSong + 1000}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("another's song: %v", err)
	}
	live, err := st.MakeLiveVersion(ctx, f.Core, f.Song3, LiveVersionOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := st.SaveAlbum(ctx, f.Core, id, AlbumInput{Title: "Night", Tracks: []AlbumTrackInput{{SongID: live}}}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a live version as a track: %v", err)
	}

	// Who sees it: the members; anyone signed in once public.
	for who, want := range map[int64]AlbumRights{f.Core: {See: true, Edit: true, Publish: true}, f.Guest: {See: true}, f.Former: {}, f.Outsider: {}} {
		r, err := st.AlbumRightsFor(ctx, who, a)
		if err != nil || r != want {
			t.Fatalf("person %d: %+v, want %+v (%v)", who, r, want, err)
		}
	}
	if _, err := st.SetAlbumPublic(ctx, f.Guest, id, true, true); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a guest publishes: %v", err)
	}
	if n, err := st.SetAlbumPublic(ctx, f.Core, id, true, true); err != nil || n != 2 {
		t.Fatalf("public with its songs: %d %v", n, err)
	}
	a, _ = st.GetAlbum(ctx, id)
	if r, _ := st.AlbumRightsFor(ctx, f.Outsider, a); !r.See || r.Edit {
		t.Fatalf("outsider on a public album: %+v", r)
	}
	var livePublic bool
	if err := st.db.QueryRow(ctx, `SELECT public FROM songs WHERE id = $1`, live).Scan(&livePublic); err != nil || livePublic {
		t.Fatalf("the live version went public: %v %v", livePublic, err)
	}
	if err := st.DeleteAlbum(ctx, f.Guest, id); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a guest deletes: %v", err)
	}
	if err := st.DeleteAlbum(ctx, f.Core, id); err != nil {
		t.Fatal(err)
	}
	if _, err := st.GetAlbum(ctx, id); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted: %v", err)
	}
}

// The master of a public album's track is heard by everyone signed in;
// the band's other recordings, and a private album's masters, by its
// members only.
func TestAlbumMasterStreaming(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAlbumsFixture(t, st)
	id, err := st.CreateAlbum(ctx, f.Core, f.Band, AlbumInput{Title: "Night", Tracks: []AlbumTrackInput{{SongID: f.BandSong, RecordingID: f.Master1}}})
	if err != nil {
		t.Fatal(err)
	}
	check := func(person, rec int64, want bool) {
		t.Helper()
		if got := st.CanHearRecording(ctx, person, rec); got != want {
			t.Fatalf("person %d hears %d: %v, want %v", person, rec, got, want)
		}
		_, err := st.RecordingFileFor(ctx, person, rec)
		if want != (err == nil) || (!want && !errors.Is(err, ErrForbidden)) {
			t.Fatalf("RecordingFileFor(%d, %d): %v", person, rec, err)
		}
	}
	check(f.Core, f.Master1, true)
	check(f.Guest, f.Master1, true)
	check(f.Outsider, f.Master1, false)
	check(f.Former, f.Master1, false)
	if _, err := st.SetAlbumPublic(ctx, f.Core, id, true, false); err != nil {
		t.Fatal(err)
	}
	check(f.Outsider, f.Master1, true)
	check(f.Outsider, f.Other, false)
	check(0, f.Master1, false)
}

// The live version: made from the song as it is (chart, tempo, sync), the
// set list lines (their notes, their click-track notes) and the
// songbook's key moved to it; the album song keeps its chart.
func TestMakeLiveVersion(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAlbumsFixture(t, st)
	song := f.BandSong
	var audioID int64
	if err := st.db.QueryRow(ctx, `
		INSERT INTO song_audio (variant_id, byte_size, sha256, data, category) VALUES ($1, 3, 'x', '\x010203', 'youtube') RETURNING id`, song).Scan(&audioID); err != nil {
		t.Fatal(err)
	}
	if _, err := st.db.Exec(ctx, `INSERT INTO song_audio_markers (song_audio_id, section_index, time_seconds) VALUES ($1, 0, 1.5), ($1, 1, 20.25)`, audioID); err != nil {
		t.Fatal(err)
	}
	sb, _ := st.GetDefaultSongbook(ctx, "band", f.Band)
	if _, err := st.db.Exec(ctx, `UPDATE songbook_entries SET semitones = 2 WHERE songbook_id = $1 AND variant_id = $2`, sb, song); err != nil {
		t.Fatal(err)
	}
	l := mustSetlist(t, st, f.SetlistID)
	l.Sets[0].Entries[0].Note = "the long intro"
	if _, err := st.SaveSetlistByID(ctx, f.SetlistID, l, "Friday", "", "", f.Core); err != nil {
		t.Fatal(err)
	}
	if _, err := st.SetClicktrackNote(ctx, f.SetlistID, song, "count four", f.Core); err != nil {
		t.Fatal(err)
	}
	before, _, _ := st.LoadVariantRaw(song)

	if _, err := st.MakeLiveVersion(ctx, f.Guest, song, LiveVersionOptions{MoveSetlists: true}); !errors.Is(err, ErrForbidden) {
		t.Fatalf("a guest: %v", err)
	}
	live, err := st.MakeLiveVersion(ctx, f.Core, song, LiveVersionOptions{MoveSetlists: true})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.MakeLiveVersion(ctx, f.Core, song, LiveVersionOptions{}); !errors.Is(err, ErrConflict) {
		t.Fatalf("a second one: %v", err)
	}
	if _, err := st.MakeLiveVersion(ctx, f.Core, live, LiveVersionOptions{}); !errors.Is(err, ErrInvalid) {
		t.Fatalf("of the live version: %v", err)
	}
	body, _, _ := st.LoadVariantRaw(live)
	after, _, _ := st.LoadVariantRaw(song)
	if body != before || after != before || headerValue(body, "tempo") != "112" {
		t.Fatalf("charts: live %q, album %q", body, after)
	}
	var isLive bool
	var name string
	var from int64
	if err := st.db.QueryRow(ctx, `SELECT live, version_name, forked_from_id FROM songs WHERE id = $1`, live).Scan(&isLive, &name, &from); err != nil || !isLive || name != "Live" || from != song {
		t.Fatalf("live %v %q %d %v", isLive, name, from, err)
	}
	var markers int
	if err := st.db.QueryRow(ctx, `SELECT count(*) FROM song_audio_markers m JOIN song_audio a ON a.id = m.song_audio_id WHERE a.variant_id = $1`, live).Scan(&markers); err != nil || markers != 2 {
		t.Fatalf("the sync: %d markers (%v)", markers, err)
	}
	var semitones int
	if err := st.db.QueryRow(ctx, `SELECT semitones FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, sb, live).Scan(&semitones); err != nil || semitones != 2 {
		t.Fatalf("the songbook's key: %d %v", semitones, err)
	}
	l = mustSetlist(t, st, f.SetlistID)
	if e := l.Sets[0].Entries[0]; e.VariantID != live || e.Note != "the long intro" {
		t.Fatalf("set list line: %+v", e)
	}
	var note string
	if err := st.db.QueryRow(ctx, `SELECT note FROM setlist_clicktrack_notes WHERE setlist_id = $1 AND variant_id = $2`, f.SetlistID, live).Scan(&note); err != nil || note != "count four" {
		t.Fatalf("click-track note: %q %v", note, err)
	}
	if m, _ := st.LiveVersionsOf(ctx, []int64{song, f.Song2}); m[song] != live || len(m) != 1 {
		t.Fatalf("LiveVersionsOf: %v", m)
	}
	if m, _ := st.LiveOf(ctx, []int64{song, live}); m[live] != song || len(m) != 1 {
		t.Fatalf("LiveOf: %v", m)
	}

	// The setting: new lines pick the live version once it's on.
	if m, err := st.LiveForNewLines(ctx, f.Band, []int64{song}); err != nil || len(m) != 0 {
		t.Fatalf("setting off: %v %v", m, err)
	}
	if err := st.SetBandSetlistVersions(ctx, f.Band, "sometimes"); !errors.Is(err, ErrInvalid) {
		t.Fatalf("a bad value: %v", err)
	}
	if err := st.SetBandSetlistVersions(ctx, f.Band, "live"); err != nil {
		t.Fatal(err)
	}
	if m, err := st.LiveForNewLines(ctx, f.Band, []int64{song, f.Song2}); err != nil || m[song] != live || len(m) != 1 {
		t.Fatalf("setting on: %v %v", m, err)
	}

	// A set list made before: Use the live versions here.
	band := f.Band
	other, err := st.CreateSetlist(ctx, "band", "Saturday", "", nil, &band, nil, f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.SaveSetlistByID(ctx, other, &setlist.Setlist{Sets: []setlist.Set{{Label: "Set 1", Entries: []setlist.Entry{{VariantID: song, Note: "loud"}, {VariantID: f.Song2}}}}}, "Saturday", "", "", f.Core); err != nil {
		t.Fatal(err)
	}
	if n, err := st.UseLiveVersions(ctx, f.Core, other); err != nil || n != 1 {
		t.Fatalf("UseLiveVersions: %d %v", n, err)
	}
	l = mustSetlist(t, st, other)
	if l.Sets[0].Entries[0].VariantID != live || l.Sets[0].Entries[0].Note != "loud" || l.Sets[0].Entries[1].VariantID != f.Song2 {
		t.Fatalf("moved: %+v", l.Sets[0].Entries)
	}
}

func TestMasterFacts(t *testing.T) {
	for name, want := range map[string][3]any{
		"03 Other lies-UltraBlanc-Bones_90_Mix.v5 FR-9W1-25-34056.wav": {3, 90, "FR-9W1-25-34056"},
		"Other Lies - Beyond Fur - 01 - The Incense of Valletta.wav":   {1, 0, ""},
		"Skins.wav": {0, 0, ""},
	} {
		p, tempo, isrc := MasterFacts(name)
		if p != want[0] || tempo != want[1] || isrc != want[2] {
			t.Errorf("%s: %d %d %q", name, p, tempo, isrc)
		}
	}
	if got := recordingTitleOf("03 Other lies-UltraBlanc-Bones_90_Mix.v5 FR-9W1-25-34056.wav"); got != "Bones" {
		t.Errorf("title: %q", got)
	}
	body := "title: Bones\nartist: Other Lies\nkey:    D\ntempo:  120\n\n[Verse]\n| D |\n"
	if got := setHeader(body, "tempo", "90"); got != strings.Replace(body, "120", "90", 1) {
		t.Errorf("setHeader: %q", got)
	}
	if got := setHeader("title: X\n\n[Verse]\n", "tempo", "90"); got != "title: X\ntempo: 90\n\n[Verse]\n" {
		t.Errorf("setHeader added: %q", got)
	}
}

// MapAlbums (cmd/albums): the dry run changes nothing and says what
// --apply does; --apply makes the albums, renames, makes the live
// versions from the songs as they are, gives the album songs the masters'
// tempos, moves the set lists, sets the band's setting; a second run has
// nothing more to do.
func TestMapAlbums(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	f := newAlbumsFixture(t, st)
	raw := `{"band": ` + itoa(f.Band) + `, "expectBand": "Other Lies", "liveVersions": "setlists", "setlistVersions": "live",
	  "skipMasters": ["Skins"],
	  "albums": [{"title": "Night", "kind": "album", "releasedOn": "2026-04-10", "folder": "Albums/1 - Night",
	    "links": ["https://otherlies.bandcamp.com/album/night"],
	    "tracks": [{"song": ` + itoa(f.BandSong) + `, "songTitle": "Night Drive", "title": "Night Drives", "rename": true, "duration": "2:23"},
	               {"song": ` + itoa(f.Song2) + `, "songTitle": "Paper Moon", "duration": "3:10", "tempo": 96}]},
	   {"title": "Night Drives", "kind": "single", "releasedOn": "2025-11-21", "tracks": [{"song": ` + itoa(f.BandSong) + `}]}]}`
	var m AlbumsMapping
	if err := json.Unmarshal([]byte(raw), &m); err != nil {
		t.Fatal(err)
	}
	count := func(sql string) int {
		var n int
		if err := st.db.QueryRow(ctx, sql).Scan(&n); err != nil {
			t.Fatal(err)
		}
		return n
	}
	rep, err := st.MapAlbums(ctx, m, AlbumsOptions{ActorID: f.Core})
	if err != nil {
		t.Fatal(err)
	}
	if count(`SELECT count(*) FROM albums`) != 0 || count(`SELECT count(*) FROM songs WHERE live`) != 0 {
		t.Fatal("the dry run wrote")
	}
	out := strings.Join(rep.Lines, "\n")
	for _, want := range []string{`create album "Night"`, "master 2", "left out", `rename song`, `"Night Drives"`, "live version of", "set lists use: song → live", "90 bpm"} {
		if !strings.Contains(out, want) {
			t.Fatalf("report lacks %q:\n%s", want, out)
		}
	}
	if rep.Live != 1 || rep.Renamed != 1 || rep.Albums != 2 || rep.SetlistLines != 1 {
		t.Fatalf("report: %+v", rep)
	}
	// A copy under other names stops, unless told to go on.
	m2 := m
	m2.ExpectBand = "Mercy"
	if _, err := st.MapAlbums(ctx, m2, AlbumsOptions{}); err == nil {
		t.Fatal("another band's mapping went through")
	}

	if _, err := st.MapAlbums(ctx, m, AlbumsOptions{Apply: true, ActorID: f.Core}); err != nil {
		t.Fatal(err)
	}
	var title, body string
	if err := st.db.QueryRow(ctx, `SELECT title, body FROM songs WHERE id = $1`, f.BandSong).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	var live int64
	var liveBody string
	if err := st.db.QueryRow(ctx, `SELECT id, body FROM songs WHERE forked_from_id = $1 AND live`, f.BandSong).Scan(&live, &liveBody); err != nil {
		t.Fatal(err)
	}
	if title != "Night Drives" || headerValue(body, "title") != "Night Drives" || headerValue(body, "tempo") != "90" || headerValue(liveBody, "tempo") != "112" || headerValue(liveBody, "title") != "Night Drives" {
		t.Fatalf("album song %q tempo %s, live tempo %s", title, headerValue(body, "tempo"), headerValue(liveBody, "tempo"))
	}
	if ids := SetlistVariantIDs(mustSetlist(t, st, f.SetlistID)); ids[0] != live {
		t.Fatalf("set list: %v", ids)
	}
	if v, _ := st.BandSetlistVersions(ctx, f.Band); v != "live" {
		t.Fatalf("setting: %s", v)
	}
	albums, err := st.BandAlbums(ctx, f.Band)
	if err != nil || len(albums) != 2 || albums[0].Title != "Night" || albums[1].Kind != "single" {
		t.Fatalf("albums: %v %v", albums, err)
	}
	night := albums[0]
	if len(night.Tracks) != 2 || night.Tracks[0].RecordingID == nil || *night.Tracks[0].RecordingID != f.Master1 || night.Tracks[0].ISRC != "FR-ABC-26-00001" ||
		*night.Tracks[0].Tempo != 90 || night.Tracks[0].LiveID != live || night.Tracks[1].RecordingID != nil || *night.Tracks[1].Tempo != 96 {
		t.Fatalf("tracks: %+v", night.Tracks)
	}
	if as, _ := st.AlbumsOfSong(ctx, live); len(as) != 2 || as[0].Title != "Night" || as[0].Position != 1 || as[1].Kind != "single" {
		t.Fatalf("AlbumsOfSong(live): %+v", as)
	}

	rep, err = st.MapAlbums(ctx, m, AlbumsOptions{Apply: true, ActorID: f.Core})
	if err != nil {
		t.Fatal(err)
	}
	if rep.Live != 0 || rep.Renamed != 0 || rep.Tempos != 0 || count(`SELECT count(*) FROM albums`) != 2 || count(`SELECT count(*) FROM songs WHERE live`) != 1 {
		t.Fatalf("second run: %+v", rep)
	}
}

func itoa(n int64) string { return strconv.FormatInt(n, 10) }
