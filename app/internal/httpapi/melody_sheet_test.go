package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/audioanalysis"
	"app/internal/store"
)

// TestMelodySheet: the Melody view's life: no recording, not split, its
// notes being found (the background job, once per split), no sync yet,
// then the score with its lyrics laid; MusicXML; an editor's correction
// of a line (kept with the line's text), refused to a viewer who isn't an
// editor and to a stranger.
func TestMelodySheet(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "Mel Owner", "mel@example.com")
	must(err)
	stranger, err := st.CreatePerson(ctx, "Stan Ger", "stan@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Tune", "", owner)
	must(err)
	// 120 bpm, 2 s a bar; a verse of 4 bars (two lines), a chorus of
	// "|: … :|" two bars played twice (one line a pass).
	body := "title: Tune\nkey: G\ntempo: 120\ntime: 4/4\n\n[Verse]\ngrid: | G | C | D | G |\nwalking in the rain\nunder the sky\n\n[Chorus]\ngrid: |: C | G :|\nhold me now darling\nnever let me go\n"
	_, err = st.SaveVariantByID(ctx, song, "Tune", "", "", body, "", owner)
	must(err)
	tokens := map[int64]string{}
	for _, p := range []int64{owner, stranger} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	call := func(who int64, method, path string, in any) (int, []byte) {
		t.Helper()
		var b bytes.Buffer
		if in != nil {
			must(json.NewEncoder(&b).Encode(in))
		}
		r := httptest.NewRequest(method, path, &b)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.Bytes()
	}
	get := func() melodyResponse {
		t.Helper()
		code, out := call(owner, "GET", fmt.Sprintf("/api/songs/%d/melody", song), nil)
		if code != http.StatusOK {
			t.Fatalf("melody: %d %s", code, out)
		}
		var d melodyResponse
		must(json.Unmarshal(out, &d))
		return d
	}
	if d := get(); d.State != "none" {
		t.Fatalf("no recording: %q", d.State)
	}
	audio, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("mp3"), "audio/mpeg", "sha1", "", owner)
	must(err)
	if d := get(); d.State != "split" {
		t.Fatalf("not split: %q", d.State)
	}
	must(st.QueueStemJob(ctx, audio))
	job, err := st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v1")})
	must(err)
	if d := get(); d.State != "notes" {
		t.Fatalf("notes not found yet: %q", d.State)
	}
	// The voice: the verse's two lines from bar 0 (5 and 4 syllables),
	// the chorus's from bar 4 and bar 6 (its second pass), 0.5 s a note.
	var notes []audioanalysis.SungNote
	sing := func(t0 float64, pitches ...float64) {
		for i, p := range pitches {
			notes = append(notes, audioanalysis.SungNote{Start: t0 + 0.5*float64(i), End: t0 + 0.5*float64(i) + 0.45, Pitch: p, Conf: 0.9})
		}
	}
	sing(10, 67, 69, 71, 72, 74)
	sing(14, 74, 72, 71, 69)
	sing(18, 67, 67, 69, 71, 72)
	sing(22, 71, 69, 67, 66, 67)
	calls := 0
	analyze := func(_ context.Context, vocals []byte) (audioanalysis.MelodyNotes, bool, error) {
		calls++
		return audioanalysis.MelodyNotes{Notes: notes, Voiced: 9}, string(vocals) == "v1", nil
	}
	did, err := findMelodyNotes(ctx, st, analyze)
	must(err)
	if !did {
		t.Fatal("no job for the split recording")
	}
	if did, _ := findMelodyNotes(ctx, st, analyze); did || calls != 1 {
		t.Fatalf("found again with nothing new: %v, %d calls", did, calls)
	}
	if d := get(); d.State != "sync" {
		t.Fatalf("no sync: %q", d.State)
	}
	// Synced: the verse at 10 s, the chorus at 18 s.
	must(st.ReplaceAudioMarkers(ctx, audio, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 10}, {SectionIndex: 1, TimeSeconds: 18}}, owner))
	d := get()
	if d.State != "ready" || d.SyncSource != "markers" || d.Notes != len(notes) {
		t.Fatalf("ready: %+v", d)
	}
	if fmt.Sprint(d.Bars) != "[0 1 2 3 4 5 6 7]" {
		t.Errorf("played bars %v, want 0…7 (the chorus played twice)", d.Bars)
	}
	for _, want := range []string{`\ks (g)`, `\section ("Chorus")`, "G4.4", `\lyrics ("wal- king in the rain un- der the sky hold me now dar- ling ne- ver let me go")`} {
		if !strings.Contains(d.AlphaTex, want) {
			t.Errorf("no %q in\n%s", want, d.AlphaTex)
		}
	}
	bars := map[string]int{}
	for _, l := range d.Lines {
		if !l.Matched {
			t.Errorf("line %s not placed", l.Key)
		}
		bars[l.Key] = l.Bar
	}
	if bars["0:0"] != 0 || bars["0:1"] != 2 || bars["1:0"] != 4 || bars["1:1"] != 6 {
		t.Errorf("lines' bars %v", bars)
	}
	// Transposed: up a tone, in A.
	code, out := call(owner, "GET", fmt.Sprintf("/api/songs/%d/melody?transpose=2", song), nil)
	if code != http.StatusOK || !bytes.Contains(out, []byte(`\\ks (a)`)) {
		t.Errorf("transposed: %d %s", code, out)
	}
	code, out = call(owner, "GET", fmt.Sprintf("/api/songs/%d/melody.musicxml", song), nil)
	if code != http.StatusOK || !bytes.Contains(out, []byte("<rehearsal>Chorus</rehearsal>")) || !bytes.Contains(out, []byte("<text>wal</text>")) {
		t.Errorf("MusicXML: %d %.300s", code, out)
	}
	// A correction: the chorus's second line a note later.
	code, out = call(owner, "PUT", fmt.Sprintf("/api/songs/%d/melody/lyrics-shift", song), map[string]int{"section": 1, "line": 1, "shift": 1})
	if code != http.StatusOK || !bytes.Contains(out, []byte(`"shift":1`)) || !bytes.Contains(out, []byte("dar- ling _ ne- ver let me+go")) {
		t.Fatalf("shift: %d %s", code, out)
	}
	// The line changed: its correction no longer applies.
	_, err = st.SaveVariantByID(ctx, song, "Tune", "", "", strings.Replace(body, "never let me go", "never let go", 1), "", owner)
	must(err)
	for _, l := range get().Lines {
		if l.Shift != 0 {
			t.Errorf("a stale correction applies to %s", l.Key)
		}
	}
	if code, _ := call(stranger, "PUT", fmt.Sprintf("/api/songs/%d/melody/lyrics-shift", song), map[string]int{"section": 1, "line": 1, "shift": 1}); code != http.StatusForbidden {
		t.Errorf("a stranger's correction: %d", code)
	}
	if code, _ := call(stranger, "GET", fmt.Sprintf("/api/songs/%d/melody", song), nil); code != http.StatusForbidden {
		t.Errorf("a stranger reads a private song's melody: %d", code)
	}
	// Split again: the notes are found again.
	must(st.QueueStemJob(ctx, audio))
	job, err = st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v2")})
	must(err)
	if did, _ := findMelodyNotes(ctx, st, analyze); !did || calls != 2 {
		t.Fatalf("split again: %v, %d calls", did, calls)
	}
	if d := get(); d.State != "unsung" {
		t.Fatalf("too little singing: %q", d.State)
	}
}

// TestMelodyLyricsOnTheChart: lines held to the bars their chords are
// played in (a song with a lead bar, 97 bpm, chords over the words): the
// second line, sung with a pickup right after the first line's held
// note and melisma, still starts on its own bar — the "line 2 drifts a
// bar" report.
func TestMelodyLyricsOnTheChart(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "Cha Owner", "cha@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Night", "", owner)
	must(err)
	body := "title: Night\nkey: G\ntempo: 97\ntime: 4/4\nlead: | G |\n\n[Verse]\ngrid: | G | G | Em | Em | C | D | G | G |\nWhen the [G]night has come and the land is [Em]dark\nand the [C]moon is the [D]only light we'll [G]see\n"
	_, err = st.SaveVariantByID(ctx, song, "Night", "", "", body, "", owner)
	must(err)
	audio, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("mp3"), "audio/mpeg", "sha1", "", owner)
	must(err)
	must(st.QueueStemJob(ctx, audio))
	job, err := st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v")})
	must(err)
	// The verse from 5 s (played bar 1, after the lead's bar 0).
	bar := 4 * 60 / 97.0
	e := bar / 8
	T := func(b int) float64 { return 5 + float64(b-1)*bar }
	var notes []audioanalysis.SungNote
	sing := func(ts ...float64) {
		for i, t0 := range ts {
			end := t0 + e*0.9
			if i+1 < len(ts) {
				end = ts[i+1] - 0.03
			}
			notes = append(notes, audioanalysis.SungNote{Start: t0, End: end, Pitch: 67, Conf: 0.9})
		}
	}
	// When the | night has come and the land is | dark (held) melisma, and the | moon is the | on-ly light we'll | see
	sing(T(1)-2*e, T(1)-e, T(1), T(1)+2*e, T(1)+4*e, T(1)+6*e, T(2), T(2)+2*e, T(2)+4*e, T(3), T(4), T(5)-2*e, T(5)-e, T(5), T(5)+2*e, T(5)+4*e, T(6), T(6)+2*e, T(6)+4*e, T(6)+6*e, T(7))
	notes[len(notes)-1].End = T(8) - 0.1
	_, err = findMelodyNotes(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyNotes, bool, error) {
		return audioanalysis.MelodyNotes{Notes: notes}, true, nil
	})
	must(err)
	must(st.ReplaceAudioMarkers(ctx, audio, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 5}}, owner))
	tok, err := st.CreateSession(ctx, owner)
	must(err)
	r := httptest.NewRequest("GET", fmt.Sprintf("/api/songs/%d/melody", song), nil)
	r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tok})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, r)
	var d melodyResponse
	must(json.Unmarshal(w.Body.Bytes(), &d))
	if d.State != "ready" || len(d.Lines) != 2 {
		t.Fatalf("melody: %d %s", w.Code, w.Body.String())
	}
	for i, want := range []int{1, 5} {
		l := d.Lines[i]
		if !l.Anchored || l.ChartBar != want || l.AnchorBar != want || l.Conf < 0.8 {
			t.Errorf("line %d: %+v, want its chord on bar %d", i, l, want)
		}
	}
	if !strings.Contains(d.AlphaTex, `\lyrics ("When the night has come and the land is dark _ and the moon is the on- ly light we'll see")`) {
		t.Errorf("lyrics:\n%s", d.AlphaTex)
	}
}
