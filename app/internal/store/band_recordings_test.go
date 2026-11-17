package store

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestParseRecording(t *testing.T) {
	type want struct {
		ok                 bool
		kind, folder, file string
		pos, take          int
		title              string
	}
	cases := map[string]want{
		"Repetes/2026-03-15|04 - Ill Play The Blues for You - 2026-03-15.mp3":           {true, "rehearsal", "2026-03-15", "2026-03-15", 4, 0, "Ill Play The Blues for You"},
		"Repetes/Archives/2024/2024-02-22|03 - Colors - Take 1 - 2024-02-22.mp3":        {true, "rehearsal", "2024-02-22", "2024-02-22", 3, 1, "Colors"},
		"Repetes/Archives 2023-2024/2024-02-26|01 - Take 1 - Kawazaki - 2024-02-24.mp3": {true, "rehearsal", "2024-02-26", "2024-02-24", 1, 1, "Kawazaki"},
		"Concerts/2025-06-14|05 - Falling In Love Again - 2025-06-14.mp3":               {true, "concert", "2025-06-14", "2025-06-14", 5, 0, "Falling In Love Again"},
		"Concerts/2024-03-16 - MJC Fresnes|Concert.mp3":                                 {true, "concert", "2024-03-16", "", 0, 0, "Concert"},
		"Set List|02 - Too Much of a Woman (feat. Jackie Venson).mp3":                   {true, "setlist", "", "", 2, 0, "Too Much of a Woman (feat. Jackie Venson)"},
		"Repetes/2025-02-03/01 - In Candeseza - 2025-02-03|Basse.wav":                   {false, "", "", "", 0, 0, ""},
		"Enregistrement/STEMs/MERCY MERCY MERCY/Audio Files|Bass_01.wav":                {false, "", "", "", 0, 0, ""},
		"Repetes/2026-03-15|notes.pdf":                                                  {false, "", "", "", 0, 0, ""},
	}
	for in, w := range cases {
		dir, name, _ := strings.Cut(in, "|")
		p, ok := parseRecording(strings.Split(dir, "/"), name, "Other Lies", builtins)
		if ok != w.ok {
			t.Errorf("%s: ok %v, want %v", in, ok, w.ok)
			continue
		}
		if !ok {
			continue
		}
		pos, take := 0, 0
		if p.Position != nil {
			pos = *p.Position
		}
		if p.Take != nil {
			take = *p.Take
		}
		if p.Kind != w.kind || p.FolderDate != w.folder || p.FileDate != w.file || pos != w.pos || take != w.take || p.Title != w.title {
			t.Errorf("%s: %+v (pos %d take %d)", in, p, pos, take)
		}
	}
}

func TestMatchBandSong(t *testing.T) {
	songs := []bandSong{{ID: 1, Title: "I'll Play the Blues for You"}, {ID: 2, Title: "Kawasaki"}, {ID: 3, Title: "Who's Been Talking"}, {ID: 4, Title: "Colors"}}
	for i := range songs {
		songs[i].Key = songKey(songs[i].Title)
	}
	for title, want := range map[string]int64{"Ill Play The Blues for You": 1, "Kawazaki": 2, "Whos Been Talkin": 3, "Colors": 4, "Green Onions": 0} {
		got := matchBandSong(title, songs)
		if (got == nil && want != 0) || (got != nil && *got != want) {
			t.Errorf("%s: %v, want %d", title, got, want)
		}
	}
}

func TestBandRecordings(t *testing.T) {
	st := newTestStore(t)
	ctx := context.Background()
	me, err := st.CreatePerson(ctx, "drummer", "Drummer", "drummer@example.com")
	if err != nil {
		t.Fatal(err)
	}
	band, err := st.CreateBand(ctx, "Other Lies", me)
	if err != nil {
		t.Fatal(err)
	}
	kawasaki, _ := st.CreateBandVariant(ctx, band, "Kawasaki", "", me)
	voiles, _ := st.CreateBandVariant(ctx, band, "Voiles", "", me)
	listing := `{"result":0,"metadata":{"name":"Other Lies","isfolder":true,"contents":[
	 {"name":"Repetes","isfolder":true,"contents":[
	  {"name":"2024-02-26","isfolder":true,"contents":[
	   {"name":"01 - Take 1 - Kawazaki - 2024-02-24.mp3","fileid":11,"size":100,"created":"Mon, 26 Feb 2024 21:00:00 +0000"},
	   {"name":"02 - Voiles - 2024-02-26.mp3","fileid":12,"size":100}]},
	  {"name":"2024-12-09","isfolder":true,"contents":[
	   {"name":"01 - Voiles - 2014-12-09.mp3","fileid":13,"size":100}]}]},
	 {"name":"Concerts","isfolder":true,"contents":[
	  {"name":"2024-05-26","isfolder":true,"contents":[
	   {"name":"01 - Voiles - 2025-05-26.mp3","fileid":21,"size":100},
	   {"name":"02 - Kawasaki - 2025-05-26.mp3","fileid":22,"size":100}]}]}]}}`
	pcloudGet = func(ctx context.Context, u string) (*http.Response, error) {
		return &http.Response{StatusCode: 200, Body: io.NopCloser(strings.NewReader(listing))}, nil
	}
	if _, err := st.SetBandRecordingSource(ctx, band, "https://e.pcloud.link/publink/show?code=abc", me); err != nil {
		t.Fatal(err)
	}
	day := func(s string) time.Time { d, _ := time.Parse("2006-01-02", s); return d }
	recs, err := st.SessionRecordings(ctx, band, me, "rehearsal", day("2024-02-26"))
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 2 || recs[0].VariantID == nil || *recs[0].VariantID != kawasaki || recs[1].Title != "Voiles" {
		t.Fatalf("session: %+v", recs)
	}

	// The concert's folder says 2024, its files 2025: a question, answered
	// by a correction.
	qs, err := st.ListDateQuestions(ctx, band)
	if err != nil {
		t.Fatal(err)
	}
	if len(qs) != 3 { // 2024-02-26 (one file says 24th), 2024-12-09 (2014), 2024-05-26 (2025)
		t.Fatalf("questions: %+v", qs)
	}
	if err := st.SetRecordingFix(ctx, band, "date", "2024-05-26", "2025-05-26", me); err != nil {
		t.Fatal(err)
	}
	sessions, _ := st.ListRecordingSessions(ctx, band, "concert")
	if len(sessions) != 1 || sessions[0].Date.Format("2006-01-02") != "2025-05-26" || sessions[0].Recordings != 2 {
		t.Errorf("concert after the correction: %+v", sessions)
	}
	if qs, _ = st.ListDateQuestions(ctx, band); len(qs) != 2 {
		t.Errorf("questions left: %+v", qs)
	}

	// Stars, by halves; the best first for the song; the band's choice.
	if err := st.RateRecording(ctx, band, recs[1].ID, me, 4.5); err != nil {
		t.Fatal(err)
	}
	if err := st.RateRecording(ctx, band, recs[1].ID, me, 4.2); err == nil {
		t.Error("4.2 stars accepted")
	}
	songRecs, _ := st.SongRecordings(ctx, band, me, voiles)
	if len(songRecs) != 3 || songRecs[0].ID != recs[1].ID || songRecs[0].MyStars != 4.5 {
		t.Errorf("Voiles' recordings: %+v", songRecs)
	}
	if err := st.SetSongRecording(ctx, band, voiles, recs[1].ID, me); err != nil {
		t.Fatal(err)
	}
	if err := st.SetSongRecording(ctx, band, kawasaki, recs[1].ID, me); err == nil {
		t.Error("a Voiles recording chosen for Kawasaki")
	}
	if c, err := st.ChosenSongRecording(ctx, band, voiles); err != nil || c == nil || c.FileID != 12 || c.Code != "abc" || c.APIHost != "eapi.pcloud.com" {
		t.Errorf("chosen: %+v %v", c, err)
	}

	stats, _ := st.BandPracticeStats(ctx, band)
	if len(stats) != 2 || stats[0].Title != "Voiles" || stats[0].Sessions != 2 || stats[0].Concerts != 1 || !stats[0].Chosen || stats[1].Sessions != 1 {
		t.Errorf("stats: %+v", stats)
	}
	// A title fix sticks to a song.
	if err := st.SetRecordingFix(ctx, band, "title", "Kawazaki", "999999", me); err == nil {
		t.Error("a title fixed to a song not the band's")
	}
}
