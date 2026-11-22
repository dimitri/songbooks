package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"app/internal/audioanalysis"
	"app/internal/store"
)

// TestSpanOn: a melody's place on the strip (C2 to C7, as the student
// page's chart), moved by the key being tried; cut at the strip's ends;
// nothing when it's off the strip.
func TestSpanOn(t *testing.T) {
	near := func(a, b float64) bool { return math.Abs(a-b) < 0.01 }
	// A3–E5 (57–76) as recorded, then 2 down: G3–D5 (55–74).
	s, ok := spanOn(57, 76, 0)
	if !ok || !near(s.Left, 35) || !near(s.Width, 20*100.0/61) || s.Clipped {
		t.Errorf("as recorded: %+v %v", s, ok)
	}
	s, ok = spanOn(57, 76, -2)
	if !ok || !near(s.Left, (55-36)*100.0/60) || !near(s.Width, 20*100.0/61) {
		t.Errorf("two down: %+v %v", s, ok)
	}
	// The same width in every key: it slides, it doesn't stretch.
	a, _ := spanOn(57, 76, 5)
	b, _ := spanOn(57, 76, -7)
	if !near(a.Width, b.Width) || !near(a.Left-b.Left, 12*100.0/60) {
		t.Errorf("sliding: %+v %+v", a, b)
	}
	// Cut at C2: from the strip's start, the rest of it.
	s, ok = spanOn(30, 40, 0)
	if !ok || s.Left != 0 || !near(s.Width, 5*100.0/61) || !s.Clipped {
		t.Errorf("cut low: %+v %v", s, ok)
	}
	s, ok = spanOn(90, 100, 0)
	if !ok || !s.Clipped || !near(s.Left+s.Width*61/60, 100+100.0/60) {
		t.Errorf("cut high: %+v %v", s, ok)
	}
	if _, ok := spanOn(20, 30, 0); ok {
		t.Error("a melody below the strip is on it")
	}
	if s, ok := spanOn(76, 57, 0); !ok || !near(s.Left, 35) {
		t.Errorf("ends swapped: %+v", s)
	}
}

// TestMelodyRangeCache: each recording's range is found once, from its
// vocals part; again when the recording is split again; a part that
// can't be read is kept as "no range" and not tried again; a song's range
// is its reference recording's, then its band's.
func TestMelodyRangeCache(t *testing.T) {
	st, _ := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	coach, err := st.CreatePerson(ctx, "coach", "Cora Coach", "coach@example.com")
	must(err)
	song, err := st.CreateVariant(ctx, store.OwnerPerson, coach, "Song", "", coach)
	must(err)
	split := func(category store.AudioCategory, sha string, vocals string) int64 {
		t.Helper()
		id, err := st.SetSongAudio(ctx, song, category, []byte("mp3-"+sha), "audio/mpeg", sha, "", coach)
		must(err)
		must(st.QueueStemJob(ctx, id))
		job, err := st.ClaimStemJob(ctx, "test")
		must(err)
		if job == nil || job.SongAudioID != id {
			t.Fatalf("claimed %+v, want recording %d", job, id)
		}
		if ok, err := st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte(vocals), "no_vocals": []byte("b")}); err != nil || !ok {
			t.Fatalf("saving the parts: %v %v", ok, err)
		}
		return id
	}
	var calls []string
	ranges := map[string]audioanalysis.MelodyRange{"v1": {Low: 57, High: 76, Voiced: 60}, "v2": {Low: 55, High: 72, Voiced: 60}, "band": {Low: 50, High: 60, Voiced: 60}}
	analyze := func(_ context.Context, vocals []byte) (audioanalysis.MelodyRange, bool, error) {
		calls = append(calls, string(vocals))
		if string(vocals) == "unreadable" {
			return audioanalysis.MelodyRange{}, false, errors.New("ffmpeg: no audio")
		}
		r, ok := ranges[string(vocals)]
		return r, ok, nil
	}
	drain := func() int {
		t.Helper()
		n := 0
		for {
			did, err := findMelodyRange(ctx, st, analyze)
			must(err)
			if !did {
				return n
			}
			n++
		}
	}
	got := func() string {
		t.Helper()
		lo, hi, known, err := st.SongMelodyRange(ctx, song)
		must(err)
		if !known {
			return "unknown"
		}
		return fmt.Sprintf("%d-%d", lo, hi)
	}

	if got() != "unknown" {
		t.Fatal("a range before any split")
	}
	split(store.AudioBand, "band1", "band")
	if n := drain(); n != 1 || got() != "50-60" {
		t.Fatalf("the band's recording: %d found, range %s", n, got())
	}
	ref := split(store.AudioYouTube, "ref1", "v1")
	if n := drain(); n != 1 || got() != "57-76" {
		t.Fatalf("the reference recording, first: %d found, range %s", n, got())
	}
	if n := drain(); n != 0 || len(calls) != 2 {
		t.Fatalf("found again with nothing new: %d, calls %v", n, calls)
	}
	// Split again (a new vocals part): found again.
	must(st.QueueStemJob(ctx, ref))
	job, err := st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v2")})
	must(err)
	if n := drain(); n != 1 || got() != "55-72" {
		t.Fatalf("split again: %d found, range %s", n, got())
	}
	// A new recording, not split yet: its old range isn't its own; the
	// band's counts until it is.
	_, err = st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("new"), "audio/mpeg", "ref2", "", coach)
	must(err)
	if got() != "50-60" {
		t.Fatalf("a replaced recording keeps its old range: %s", got())
	}
	// Unreadable: kept as no range, not tried again.
	split(store.AudioYouTube, "ref3", "unreadable")
	before := len(calls)
	if n := drain(); n != 1 || got() != "50-60" {
		t.Fatalf("unreadable: %d, %s", n, got())
	}
	if n := drain(); n != 0 || len(calls) != before+1 {
		t.Fatalf("unreadable tried again: %v", calls[before:])
	}
}

// TestVoiceStripAccess: a student sees their own strip (and the song's
// melody over it), never another's; the coach sees any student's; the
// song must be the community's.
func TestVoiceStripAccess(t *testing.T) {
	st, h := newTestServer(t)
	ctx := context.Background()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	person := func(u, name string) int64 {
		t.Helper()
		id, err := st.CreatePerson(ctx, u, name, u+"@example.com")
		must(err)
		return id
	}
	coach, stan, olga, outsider := person("coachuser", "Cora Coach"), person("stanuser", "Stan Student"), person("olgauser", "Olga Other"), person("outuser", "Otto Out")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	for _, p := range []int64{stan, olga} {
		_, err := st.AddCommunityMember(ctx, c, p, "member", coach)
		must(err)
	}
	song, err := st.CreateVariant(ctx, store.OwnerCommunity, c, "Song", "", coach)
	must(err)
	elsewhere, err := st.CreateVariant(ctx, store.OwnerPerson, outsider, "Theirs", "", outsider)
	must(err)
	lo, hi := 48, 64
	must(st.SetStudentRange(ctx, c, stan, store.VoiceRange{Register: "chest", Low: &lo, High: &hi}, coach))
	tl, th := 50, 62
	must(st.SetStudentVoiceAssessment(ctx, c, stan, store.VoiceAssessment{TessituraLow: &tl, TessituraHigh: &th}, coach))
	ol, oh := 60, 79
	must(st.SetStudentRange(ctx, c, olga, store.VoiceRange{Register: "head", Low: &ol, High: &oh}, coach))
	// The song's melody, found from its recording.
	id, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("mp3"), "audio/mpeg", "sha", "", coach)
	must(err)
	must(st.QueueStemJob(ctx, id))
	job, err := st.ClaimStemJob(ctx, "test")
	must(err)
	_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v")})
	must(err)
	_, err = findMelodyRange(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyRange, bool, error) {
		return audioanalysis.MelodyRange{Low: 57, High: 76, Voiced: 30}, true, nil
	})
	must(err)

	tokens := map[int64]string{}
	for _, p := range []int64{coach, stan, olga, outsider} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	get := func(who int64, variant int64, query string) (int, voiceStrip) {
		t.Helper()
		r := httptest.NewRequest("GET", fmt.Sprintf("/api/songs/%d/voice-strip?community=%d%s", variant, c, query), nil)
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		var d voiceStrip
		if w.Code == http.StatusOK {
			must(json.Unmarshal(w.Body.Bytes(), &d))
		}
		return w.Code, d
	}
	registers := func(d voiceStrip) string {
		var out []string
		for _, r := range d.Registers {
			if r.Width > 0 {
				out = append(out, r.Register)
			}
		}
		return strings.Join(out, ",")
	}
	code, d := get(stan, song, "")
	if code != http.StatusOK || registers(d) != "chest" || d.Comfort == nil || len(d.Melody) == 0 {
		t.Fatalf("Stan's own: %d %+v", code, d)
	}
	if m0, m2 := d.Melody["0"], d.Melody["-2"]; m0.Left <= m2.Left || len(d.Scale) != 6 {
		t.Errorf("the melody doesn't slide down with the key: %+v %+v", m0, m2)
	}
	if code, _ := get(stan, song, fmt.Sprintf("&person=%d", olga)); code != http.StatusForbidden {
		t.Errorf("Stan asks for Olga's strip: %d", code)
	}
	if code, d := get(olga, song, ""); code != http.StatusOK || registers(d) != "head" || d.Comfort != nil {
		t.Errorf("Olga's own: %d %+v", code, d)
	}
	if code, d := get(coach, song, fmt.Sprintf("&person=%d", stan)); code != http.StatusOK || registers(d) != "chest" {
		t.Errorf("the coach, Stan's: %d %+v", code, d)
	}
	if code, _ := get(coach, song, ""); code != http.StatusNotFound {
		t.Errorf("the coach without a student: %d", code)
	}
	if code, _ := get(coach, song, fmt.Sprintf("&person=%d", outsider)); code != http.StatusNotFound {
		t.Errorf("the coach, someone not a student: %d", code)
	}
	if code, _ := get(outsider, song, ""); code != http.StatusNotFound {
		t.Errorf("someone outside the community: %d", code)
	}
	if code, _ := get(stan, elsewhere, ""); code != http.StatusNotFound {
		t.Errorf("a song from elsewhere: %d", code)
	}
}
