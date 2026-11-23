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

// TestSongProposalFlow: a student proposes a song of their own to the
// coach (from the song's page or the Songbooks section), with a note and a
// key; the coach is told, opens and plays it while it waits, finds it in
// the Requests list told apart from the asks, and takes it (the
// community's arrangement of the student's private song, in the
// repertoire and the student's songbook, in a key) or declines it with a
// note; the student is told either way. Nobody else sees any of it; a
// band's private song can't be proposed; the coach proposes nothing.
func TestSongProposalFlow(t *testing.T) {
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
	coach, zoe, max := person("coachuser", "Cora Coach"), person("zoeuser", "Zoé Holloway"), person("maxuser", "Max Other")
	c, err := st.CreateCommunity(ctx, "Voice", coach)
	must(err)
	must(st.SetCommunityKind(ctx, c, "coaching"))
	for _, p := range []int64{zoe, max} {
		_, err := st.AddCommunityMember(ctx, c, p, "member", coach)
		must(err)
	}
	rep, err := st.GetDefaultSongbook(ctx, "community", c)
	must(err)
	plain, err := st.CreateVariant(ctx, store.OwnerCommunity, c, "Plain", "", coach)
	must(err)
	must(st.AddToSongbook(ctx, rep, plain, coach))

	own, err := st.CreateVariant(ctx, store.OwnerPerson, zoe, "Own Song", "Zoé", zoe)
	must(err)
	_, err = st.SaveVariantRaw(own, "title: Own Song\nartist: Zoé\nkey: D\n\n[V]\n[D]la [A]la\n", "", zoe)
	must(err)
	pub, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Public Song", "Someone", coach)
	must(err)
	band, err := st.CreateBand(ctx, "Zoé's band", zoe)
	must(err)
	bandSong, err := st.CreateVariant(ctx, store.OwnerBand, band, "Band Song", "", zoe)
	must(err)
	mine, err := st.GetDefaultSongbook(ctx, "person", zoe)
	must(err)
	for _, v := range []int64{own, pub, bandSong} {
		must(st.AddToSongbook(ctx, mine, v, zoe))
	}
	// Zoé's voice: comfortable from D3 to D4.
	lo, hi := 48, 64
	must(st.SetStudentRange(ctx, c, zoe, store.VoiceRange{Register: "chest", Low: &lo, High: &hi}, coach))
	tl, th := 50, 62
	must(st.SetStudentVoiceAssessment(ctx, c, zoe, store.VoiceAssessment{TessituraLow: &tl, TessituraHigh: &th}, coach))
	// Her song's melody and the repertoire's Plain's, A3 to E5, found
	// from their recordings.
	melody := func(song int64, sha string) {
		t.Helper()
		aid, err := st.SetSongAudio(ctx, song, store.AudioYouTube, []byte("mp3"+sha), "audio/mpeg", sha, "", coach)
		must(err)
		must(st.QueueStemJob(ctx, aid))
		job, err := st.ClaimStemJob(ctx, "test")
		must(err)
		_, err = st.SaveStems(ctx, *job, "audio/mpeg", map[string][]byte{"vocals": []byte("v" + sha)})
		must(err)
		_, err = findMelodyRange(ctx, st, func(context.Context, []byte) (audioanalysis.MelodyRange, bool, error) {
			return audioanalysis.MelodyRange{Low: 57, High: 76, Voiced: 30}, true, nil
		})
		must(err)
	}
	melody(own, "sha1")
	melody(plain, "sha2")
	// Plain is in Zoé's songbook with the coach, a semitone up.
	zoeBook, err := st.StudentSongbook(ctx, c, zoe, "Zoé’s songs", coach)
	must(err)
	must(st.AddToSongbook(ctx, zoeBook, plain, coach))
	must(st.SetEntrySemitones(ctx, zoeBook, plain, 1))

	tokens := map[int64]string{}
	for _, p := range []int64{coach, zoe, max} {
		tok, err := st.CreateSession(ctx, p)
		must(err)
		tokens[p] = tok
	}
	do := func(who int64, method, path, body string) (int, string) {
		t.Helper()
		r := httptest.NewRequest(method, path, strings.NewReader(body))
		if body != "" {
			r.Header.Set("Content-Type", "application/json")
		}
		r.Header.Set("Accept-Language", "en")
		r.AddCookie(&http.Cookie{Name: sessionCookie, Value: tokens[who]})
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code, w.Body.String()
	}
	notes := func(p int64, kind string) []store.Notification {
		t.Helper()
		all, err := st.ListNotifications(ctx, p, false)
		must(err)
		var out []store.Notification
		for _, n := range all {
			if n.Kind == kind {
				out = append(out, n)
			}
		}
		return out
	}
	page := fmt.Sprintf("/communities/%d/songbooks", c)
	proposals := fmt.Sprintf("/api/communities/%d/song-proposals", c)
	song := func(id int64) string { return fmt.Sprintf("/song/%d", id) }

	// Zoé: the Songbooks section offers to propose; the song's page too;
	// her own songbook's songs, the band's not proposable.
	_, body := do(zoe, "GET", page, "")
	for _, want := range []string{`id="proposals"`, "Your proposals to Cora", fmt.Sprintf(`data-my-songbook="%d"`, mine), "propose-song.js"} {
		if !strings.Contains(body, want) {
			t.Errorf("Zoé's Songbooks section: no %q", want)
		}
	}
	if _, body := do(zoe, "GET", song(own), ""); !strings.Contains(body, fmt.Sprintf(`data-propose-community="%d"`, c)) || !strings.Contains(body, "Propose to Cora…") {
		t.Error("the song's page doesn't offer to propose it")
	}
	if _, body := do(zoe, "GET", song(bandSong), ""); strings.Contains(body, "data-propose-community") {
		t.Error("the song's page offers to propose a band's private song")
	}
	if _, body := do(coach, "GET", song(plain), ""); strings.Contains(body, "data-propose-community") {
		t.Error("the coach is offered to propose")
	}
	code, body := do(zoe, "GET", proposals+"/songs", "")
	if code != http.StatusOK {
		t.Fatalf("Zoé's songs: %d", code)
	}
	var list struct{ Songs []proposableSong }
	must(json.Unmarshal([]byte(body), &list))
	state := map[int64]string{}
	for _, s := range list.Songs {
		state[s.ID] = s.State
	}
	if len(list.Songs) != 3 || state[own] != "" || state[pub] != "" || state[bandSong] != "no" {
		t.Errorf("Zoé's songs to propose: %+v", list.Songs)
	}
	if code, _ := do(coach, "GET", proposals+"/songs", ""); code != http.StatusForbidden {
		t.Errorf("the coach's songs to propose: %d", code)
	}

	// Before it's proposed the coach can't open her song.
	if code, _ := do(coach, "GET", song(own), ""); code != http.StatusForbidden {
		t.Fatalf("the coach opens Zoé's private song: %d", code)
	}
	if code, _ := do(zoe, "POST", proposals, fmt.Sprintf(`{"variantId":%d}`, bandSong)); code != http.StatusForbidden {
		t.Errorf("proposing a band's private song: %d", code)
	}
	if code, _ := do(coach, "POST", proposals, fmt.Sprintf(`{"variantId":%d}`, pub)); code != http.StatusForbidden {
		t.Errorf("the coach proposing: %d", code)
	}
	if code, b := do(zoe, "POST", proposals, fmt.Sprintf(`{"variantId":%d,"note":"I sing it in the car","semitones":2}`, own)); code != http.StatusOK {
		t.Fatalf("proposing: %d %s", code, b)
	}
	if code, _ := do(zoe, "POST", proposals, fmt.Sprintf(`{"variantId":%d,"note":"I sing it in the car","semitones":2}`, own)); code != http.StatusOK {
		t.Fatalf("proposing again: %d", code)
	}
	told := notes(coach, "song_proposal")
	if len(told) != 1 || !strings.Contains(told[0].Body, "Zoé Holloway") || !strings.Contains(told[0].Body, "Own Song") {
		t.Fatalf("the coach's notifications: %+v", told)
	}
	pending, err := st.PendingSongRequests(ctx, c)
	must(err)
	if len(pending) != 1 || !pending[0].IsProposal() {
		t.Fatalf("pending: %+v", pending)
	}
	req := pending[0].ID

	// While it waits: the coach opens and plays it, sees her range over
	// it; Max sees nothing.
	if code, _ := do(coach, "GET", song(own), ""); code != http.StatusOK {
		t.Errorf("the coach opening the proposed song: %d", code)
	}
	if code, _ := do(coach, "GET", fmt.Sprintf("/api/songs/%d/voice-strip?community=%d&person=%d", own, c, zoe), ""); code != http.StatusOK {
		t.Errorf("the coach's strip over the proposed song: %d", code)
	}
	if code, _ := do(max, "GET", song(own), ""); code != http.StatusForbidden {
		t.Errorf("another student opens the proposed song: %d", code)
	}
	_, body = do(coach, "GET", page, "")
	i := strings.Index(body, fmt.Sprintf(`id="request-%d"`, req))
	if i < 0 {
		t.Fatal("the coach's Requests list doesn't show the proposal")
	}
	r := body[i:]
	r = r[:strings.Index(r, "</li>")]
	for _, want := range []string{"proposed by the student", "I sing it in the car", "would like it in E", `data-semitones="2"`, "Take it",
		"E (&#43;2)", "Their own song: taking it makes the community’s arrangement of it"} {
		if !strings.Contains(r, want) {
			t.Errorf("the proposal's row: no %q in %s", want, r)
		}
	}
	if _, body := do(max, "GET", page, ""); strings.Contains(body, "Own Song") {
		t.Error("another student's page shows the proposal")
	}
	add := fmt.Sprintf("/api/communities/%d/song-requests/%d/add", c, req)
	if code, _ := do(zoe, "POST", add, `{"semitones":1}`); code != http.StatusForbidden {
		t.Errorf("the student takes her own proposal: %d", code)
	}

	// Taken at +1: the community's arrangement, in the repertoire and in
	// her songbook; she's told; the coach's sight of hers goes.
	code, body = do(coach, "POST", add, `{"semitones":1}`)
	if code != http.StatusOK {
		t.Fatalf("taking it: %d %s", code, body)
	}
	var took struct {
		SongbookID int64 `json:"songbookId"`
		VariantID  int64 `json:"variantId"`
		Copied     bool  `json:"copied"`
	}
	must(json.Unmarshal([]byte(body), &took))
	if !took.Copied || took.VariantID == own {
		t.Fatalf("taken: %+v", took)
	}
	if n, _ := st.EntrySemitones(ctx, took.SongbookID, took.VariantID); n != 1 {
		t.Errorf("her key: %d", n)
	}
	if got := notes(zoe, "song_proposal_accepted"); len(got) != 1 || !strings.Contains(got[0].Link, fmt.Sprintf("/song/%d?songbook=%d", took.VariantID, took.SongbookID)) {
		t.Errorf("Zoé's notification: %+v", got)
	}
	if told := notes(coach, "song_proposal"); !told[0].IsRead() {
		t.Error("the coach's notification isn't read")
	}
	if code, _ := do(coach, "GET", song(own), ""); code != http.StatusForbidden {
		t.Errorf("the coach still opens Zoé's own song: %d", code)
	}
	if code, _ := do(zoe, "GET", song(took.VariantID)+fmt.Sprintf("?songbook=%d", took.SongbookID), ""); code != http.StatusOK {
		t.Errorf("Zoé opening the community's arrangement: %d", code)
	}
	if ok, _ := st.SongbookHasVariant(ctx, mine, own); !ok {
		t.Error("her own song left her songbook")
	}

	// Her Songbooks section: Plain's melody at her key, an indication
	// only (no verdict on her voice: that's the coach's); the proposal
	// taken.
	_, body = do(zoe, "GET", page, "")
	for _, want := range []string{"Taken: in your songbook", `aria-label="Range B♭3–F5"`} {
		if !strings.Contains(body, want) {
			t.Errorf("Zoé's Songbooks section after: no %q", want)
		}
	}
	noVerdict := func(where, body string) {
		t.Helper()
		for _, not := range []string{"15 high", "14 high", " low<", ">fits<", "row-voice-fit", "comfortable zone", "data-zone"} {
			if strings.Contains(body, not) {
				t.Errorf("%s: a verdict on the voice, %q", where, not)
			}
		}
	}
	noVerdict("Zoé's Songbooks section", body)
	// The coach's songbook: the melody in the song's own key on the row,
	// at Zoé's key in her chip, for the filter by her; the community's new
	// arrangement (its recording not analysed yet) says nothing.
	_, body = do(coach, "GET", page, "")
	noVerdict("the coach's songbook", body)
	rowOf := func(title string) string {
		t.Helper()
		i := strings.Index(body, fmt.Sprintf(`data-title="%s"`, title))
		if i < 0 {
			t.Fatalf("no row for %s", title)
		}
		r := body[i:]
		return r[:strings.Index(r, `class="cbk-assign"`)]
	}
	if row := rowOf("Plain"); !strings.Contains(row, `aria-label="Range A3–E5"`) || !strings.Contains(row, `<template class="cbk-chip-voice">`) || !strings.Contains(row, `aria-label="Range B♭3–F5"`) {
		t.Errorf("the coach's row: %s", row)
	}
	if row := rowOf("Own Song"); strings.Contains(row, "row-voice") || !strings.Contains(row, fmt.Sprintf(`data-person="%d"`, zoe)) {
		t.Errorf("the taken song's row: %s", row)
	}
	// Her own songbook: the range at its key there (the song's own); the
	// band's song (no melody known) nothing.
	_, body = do(zoe, "GET", fmt.Sprintf("/songbooks/%d", mine), "")
	if !strings.Contains(body, `aria-label="Range A3–E5"`) {
		t.Error("her own songbook doesn't show the melody")
	}
	noVerdict("her own songbook", body)
	_, body = do(zoe, "GET", fmt.Sprintf("/songbooks/%d", zoeBook), "")
	if !strings.Contains(body, `aria-label="Range B♭3–F5"`) {
		t.Error("her songbook with the coach doesn't show the melody at her key")
	}
	noVerdict("her songbook with the coach", body)
	if _, body := do(max, "GET", fmt.Sprintf("/songbooks/%d", took.SongbookID), ""); strings.Contains(body, "B♭3–F5") {
		t.Error("another student sees Zoé's songbook")
	}

	// Declined with a note: she's told why; nothing moves.
	if code, _ := do(zoe, "POST", proposals, fmt.Sprintf(`{"variantId":%d}`, pub)); code != http.StatusOK {
		t.Fatalf("proposing the public song: %d", code)
	}
	pending, _ = st.PendingSongRequests(ctx, c)
	decline := fmt.Sprintf("/api/communities/%d/song-requests/%d/decline", c, pending[0].ID)
	if code, _ := do(coach, "POST", decline, `{"reply":"Let’s keep it for next term"}`); code != http.StatusNoContent {
		t.Fatalf("declining: %d", code)
	}
	if got := notes(zoe, "song_proposal_declined"); len(got) != 1 || !strings.Contains(got[0].Body, "Let’s keep it for next term") {
		t.Errorf("Zoé's notification of the no: %+v", got)
	}
	if ok, _ := st.RepertoireHas(ctx, c, pub); ok {
		t.Error("a declined song entered the repertoire")
	}
	_, body = do(zoe, "GET", page, "")
	if !strings.Contains(body, "Not this time") || !strings.Contains(body, "Let’s keep it for next term") {
		t.Error("Zoé's proposals don't say the no and its note")
	}
}
