package httpapi

import (
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

// TestRowRange: a songbook's rows draw a song's melody as small staves:
// its range as one bar (two whole notes) when only the range is known;
// once the melody is analysed, the bars where it first reaches its
// lowest and its highest note (played order: a repeat's later pass, a
// later bar, lose), fetched as the row comes into view, moved by the
// row's key, with the syllables under them and the words for a screen
// reader. Only who sees the song gets them; a song with no melody, none.
func TestRowRange(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	owner, err := st.CreatePerson(ctx, "Ra Owner", "ra@example.com")
	must(err)
	stranger, err := st.CreatePerson(ctx, "Stan Ger", "stan@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Tune", "", owner)
	must(err)
	// 120 bpm, 2 s a bar: a verse of 4 bars, a chorus of 2 played twice.
	body := "title: Tune\nkey: G\ntempo: 120\ntime: 4/4\n\n[Verse]\ngrid: | G | C | D | G |\nwalking in the rain\nunder the sky\n\n[Chorus]\ngrid: |: C | G :|\nhold me now darling\nnever let me go\n"
	_, err = st.SaveVariantByID(ctx, song, "Tune", "", "", body, "", owner)
	must(err)
	plain, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Plain", "", owner)
	must(err)
	_, err = st.SaveVariantByID(ctx, plain, "Plain", "", "", "title: Plain\nkey: C\n\n[Verse]\ngrid: | C | G |\n", "", owner)
	must(err)
	bare, err := st.CreateVariant(ctx, store.OwnerPerson, owner, "Bare", "", owner)
	must(err)

	split := func(id int64, sha string) int64 {
		t.Helper()
		a, err := st.SetSongAudio(ctx, id, store.AudioYouTube, []byte("mp3"+sha), "audio/mpeg", sha, "", owner)
		must(err)
		must(st.QueueStemJob(ctx, a))
		job, err := st.ClaimStemJob(ctx, "test")
		must(err)
		_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v" + sha)})
		must(err)
		return a
	}
	// Tune's voice: bar 2 (the verse's 2nd line) has the lowest note D4
	// first, bar 5 (the chorus's 1st pass) the highest E5 first; bar 6
	// (its 2nd pass) has both again, later.
	audio := split(song, "s1")
	var notes []audioanalysis.SungNote
	sing := func(t0 float64, pitches ...float64) {
		for i, p := range pitches {
			notes = append(notes, audioanalysis.SungNote{Start: t0 + 0.5*float64(i), End: t0 + 0.5*float64(i) + 0.45, Pitch: p, Conf: 0.9})
		}
	}
	sing(10, 67, 69, 71, 72, 74)
	sing(14, 62, 72, 71, 69)
	sing(18, 67, 67, 69, 71, 76)
	sing(22, 62, 69, 76, 66, 67)
	_, err = findMelodyNotes(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyNotes, bool, error) {
		return audioanalysis.MelodyNotes{Notes: notes, Voiced: 9}, true, nil
	})
	must(err)
	must(st.ReplaceAudioMarkers(ctx, audio, []store.AudioMarker{{SectionIndex: 0, TimeSeconds: 10}, {SectionIndex: 1, TimeSeconds: 18}}, owner))
	_, err = findMelodyRange(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyRange, bool, error) {
		return audioanalysis.MelodyRange{Low: 62, High: 76, Voiced: 9}, true, nil
	})
	must(err)
	// Plain: its range only (A3 to C5).
	split(plain, "s2")
	_, err = findMelodyRange(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyRange, bool, error) {
		return audioanalysis.MelodyRange{Low: 57, High: 72, Voiced: 9}, true, nil
	})
	must(err)

	tokens := map[int64]string{}
	for _, p := range []int64{owner, stranger} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	call := func(who int64, path string) (int, string) {
		t.Helper()
		r := httptest.NewRequest("GET", path, nil)
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	bars := func(k int) rowRangeResponse {
		t.Helper()
		code, out := call(owner, fmt.Sprintf("/api/songs/%d/row-range?k=%d", song, k))
		if code != http.StatusOK {
			t.Fatalf("row range at %d: %d %s", k, code, out)
		}
		var d rowRangeResponse
		must(json.Unmarshal([]byte(out), &d))
		return d
	}
	d := bars(0)
	if d.Low != 62 || d.High != 76 || len(d.Bars) != 2 || d.Bars[0].Bar != 2 || d.Bars[0].Kind != "low" || d.Bars[1].Bar != 5 || d.Bars[1].Kind != "high" {
		t.Fatalf("the bars: %+v", d)
	}
	if d.Label != "Range D4–E5; lowest in bar 3, highest in bar 6" {
		t.Errorf("label: %q", d.Label)
	}
	for _, b := range d.Bars {
		if !strings.Contains(b.SVG, `class="rr-hi"`) || !strings.Contains(b.SVG, "<svg") || !strings.Contains(b.SVG, "rr-lyrics") {
			t.Errorf("bar %d's drawing: %s", b.Bar, b.SVG)
		}
	}
	// In the row's key, two semitones down: the same bars, the notes moved.
	if d := bars(-2); d.Low != 60 || d.High != 74 || d.Bars[0].Bar != 2 || !strings.HasPrefix(d.Label, "Range C4–D5;") {
		t.Errorf("down 2: %+v", d)
	}
	if again := bars(0); again.Label != d.Label {
		t.Errorf("cached: %q", again.Label)
	}
	if code, _ := call(stranger, fmt.Sprintf("/api/songs/%d/row-range", song)); code != http.StatusForbidden {
		t.Errorf("someone who can't see the song: %d", code)
	}
	for _, id := range []int64{plain, bare} {
		if code, _ := call(owner, fmt.Sprintf("/api/songs/%d/row-range", id)); code != http.StatusNoContent {
			t.Errorf("song %d with no melody analysed: %d", id, code)
		}
	}

	// The owner's songbook: Tune's row fetches its bars (its range drawn
	// till then); Plain's draws its range as one bar; Bare's nothing.
	mine, err := st.GetDefaultSongbook(ctx, "person", owner)
	must(err)
	_, page := call(owner, fmt.Sprintf("/songbooks/%d", mine))
	row := func(id int64) string {
		t.Helper()
		i := strings.Index(page, fmt.Sprintf(`data-variant-id="%d"`, id))
		if i < 0 {
			t.Fatalf("no row for %d", id)
		}
		r := page[i:]
		return r[:strings.Index(r, "</li>")]
	}
	if r := row(song); !strings.Contains(r, fmt.Sprintf(`data-row-range="/api/songs/%d/row-range?k=0"`, song)) || !strings.Contains(r, `aria-label="Range D4–E5"`) {
		t.Errorf("Tune's row: %s", r)
	}
	if r := row(plain); strings.Contains(r, "data-row-range") || !strings.Contains(r, `aria-label="Range A3–C5"`) || strings.Count(r, "<ellipse") != 2 {
		t.Errorf("Plain's row: %s", r)
	}
	if r := row(bare); strings.Contains(r, "row-voice") {
		t.Errorf("Bare's row: %s", r)
	}
	if !strings.Contains(page, "row-range.js") {
		t.Error("the songbook page doesn't load row-range.js")
	}
}
