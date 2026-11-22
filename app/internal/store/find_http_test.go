package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"testing"

	"app/internal/store"
)

// Finding and adding songs over HTTP (docs/ownership-model.md §1, §3,
// §6, §7): the badges per role, Create's rights, The Big Songbook's page
// with "follows the recording", the duplicates review and its merge.

type findAnswer struct {
	Works []struct {
		WorkID int64 `json:"workId"`
		Songs  []struct {
			ID       int64  `json:"id"`
			Badge    string `json:"badge"`
			Linkable bool   `json:"linkable"`
		} `json:"songs"`
	} `json:"works"`
}

func findAPI(t *testing.T, c client, query string) map[int64]string {
	t.Helper()
	w := c.do("GET", "/api/find?"+query, nil)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /api/find?%s: %d %s", query, w.Code, w.Body.String())
	}
	var a findAnswer
	if err := json.Unmarshal(w.Body.Bytes(), &a); err != nil {
		t.Fatal(err)
	}
	out := map[int64]string{}
	for _, wk := range a.Works {
		for _, s := range wk.Songs {
			out[s.ID] = s.Badge
		}
	}
	return out
}

// TestFindBadgesOverHTTP: each song says whose it is and who sees it, as
// the person sees it; a closed songbook's song isn't offered to another
// songbook; the catalogue page and the search palette say the same.
func TestFindBadgesOverHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	store.ExecForTest(t, st, `UPDATE bands SET music_type = 'original' WHERE id = $1`, f.Band)
	mine, err := st.CreateVariant(ctx, store.OwnerPerson, f.Guest, "Le Vent", "Someone", f.Guest)
	if err != nil {
		t.Fatal(err)
	}
	g := as(t, st, f.Guest)
	got := findAPI(t, g, "q=vent")
	if got[mine] != "Yours · private" || got[f.CatalogSong] != "The Big Songbook · public" {
		t.Errorf("guest's \"vent\": %v", got)
	}
	if got := findAPI(t, g, "q=night"); got[f.BandSong] != "Other Lies · original · private" {
		t.Errorf("the band's song for its guest: %q", got[f.BandSong])
	}
	if got := findAPI(t, as(t, st, f.Outsider), "q=night"); len(got) != 0 {
		t.Errorf("an outsider finds the band's private song: %v", got)
	}
	if err := st.SetSongPublic(ctx, f.Core, f.BandSong, true); err != nil {
		t.Fatal(err)
	}
	if got := findAPI(t, as(t, st, f.Outsider), "q=night"); got[f.BandSong] != "Other Lies · original · public" {
		t.Errorf("the band's public song for an outsider: %v", got)
	}
	if got := findAPI(t, g, "q=vent&scope=mine"); len(got) != 1 || got[mine] == "" {
		t.Errorf("Mine: %v", got)
	}
	html := g.page("/catalog?q=vent")
	if !strings.Contains(html, "Yours · private") || !strings.Contains(html, "2 versions") {
		t.Error("the catalogue doesn't show the badges, or the work's versions")
	}
	if !strings.Contains(g.page("/catalog?scope=bands"), "Night Drive") {
		t.Error("My bands doesn't list the band's song")
	}

	// A closed songbook's song: never offered to another songbook.
	staff, _ := st.CreatePerson(ctx, "staff", "Staff", "staff@example.com")
	community, err := st.CreateCommunity(ctx, "Florent", staff)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := st.AddCommunityMember(ctx, community, f.Guest, "member", staff); err != nil {
		t.Fatal(err)
	}
	closedSong, _ := st.CreateVariant(ctx, store.OwnerCommunity, community, "Leçon Vent", "", staff)
	open, _ := st.GetDefaultSongbook(ctx, "community", community)
	closed, _ := st.CreateSongbook(ctx, "community", community, "La méthode", false)
	_ = st.RemoveFromSongbook(ctx, open, closedSong)
	_ = st.AddToSongbook(ctx, closed, closedSong, staff)
	if err := st.SetSongbookClosed(ctx, closed, true); err != nil {
		t.Fatal(err)
	}
	if got := findAPI(t, g, "q=vent"); got[closedSong] != "Florent · closed" {
		t.Errorf("the closed songbook's song in the guest's search: %q", got[closedSong])
	}
	book, _ := st.GetDefaultSongbook(ctx, "band", f.Band)
	if got := findAPI(t, g, fmt.Sprintf("q=vent&songbook=%d", book)); got[closedSong] != "" || got[f.CatalogSong] == "" {
		t.Errorf("the Add dialog for the band's songbook: %v, want the catalogue's song, not the closed one", got)
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/api/find?songbook=%d", book), nil); w.Code != http.StatusForbidden {
		t.Errorf("an outsider's Add dialog for the band's songbook: %d", w.Code)
	}
}

// TestCreateOverHTTP: Create in "Goes into" — a guest's song is private,
// a core member's may be public; the targets say so.
func TestCreateOverHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	book, _ := st.GetDefaultSongbook(ctx, "band", f.Band)
	targets := func(c client) map[int64]bool {
		t.Helper()
		w := c.do("GET", fmt.Sprintf("/api/songbook-targets?keep=%d", book), nil)
		var out struct {
			Targets []struct {
				ID         int64 `json:"id"`
				CanPublish bool  `json:"canPublish"`
				Big        bool  `json:"big"`
			} `json:"targets"`
		}
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		m := map[int64]bool{}
		for _, x := range out.Targets {
			if x.Big {
				m[-1] = true
				continue
			}
			m[x.ID] = x.CanPublish
		}
		return m
	}
	g, c := as(t, st, f.Guest), as(t, st, f.Core)
	if pub, ok := targets(g)[book]; !ok || pub {
		t.Errorf("a guest's Goes into: listed %v, may publish %v", ok, pub)
	}
	if !targets(c)[book] {
		t.Error("a core member may not publish in the band's songbook")
	}
	if _, big := targets(c)[-1]; big {
		t.Error("The Big Songbook offered to a non-admin")
	}
	if w := g.do("POST", fmt.Sprintf("/api/songbooks/%d/songs", book), map[string]any{"title": "Mine", "public": true}); w.Code != http.StatusForbidden {
		t.Errorf("a guest's public song: %d", w.Code)
	}
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/songbooks/%d/songs", book), map[string]any{"title": "Mine"}); w.Code != http.StatusForbidden {
		t.Errorf("an outsider's song in the band's songbook: %d", w.Code)
	}
	id := g.variantID(g.do("POST", fmt.Sprintf("/api/songbooks/%d/songs", book), map[string]any{"title": "Guest Song", "artist": "Other Lies", "referenceUrl": "https://youtu.be/abc"}))
	if v, _ := st.GetVariant(ctx, id); v == nil || v.Public || v.OwnerBandID == nil || *v.OwnerBandID != f.Band {
		t.Errorf("the guest's new song: %+v", v)
	}
	pub := c.variantID(c.do("POST", fmt.Sprintf("/api/songbooks/%d/songs", book), map[string]any{"title": "Core Song", "public": true}))
	if v, _ := st.GetVariant(ctx, pub); v == nil || !v.Public {
		t.Errorf("the core member's public song: %+v", v)
	}
}

// TestBigSongbookAndDuplicatesOverHTTP: The Big Songbook lists every
// public song with "follows the recording" where its analysis says so;
// its staff review duplicates and merge them, a dry run first; the
// merged song's links open the kept one.
func TestBigSongbookAndDuplicatesOverHTTP(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := store.NewBandFixture(t, st)
	admin, _ := st.CreatePerson(ctx, "admin", "Admin", "admin@example.com")
	if err := st.SetAdmin(ctx, admin, true); err != nil {
		t.Fatal(err)
	}
	dup, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, "Le vent", "SOMEONE", admin)
	if err != nil {
		t.Fatal(err)
	}
	// Song 334's recording and charts (internal/chartalign/testdata): the
	// corrected chart follows it, the old one doesn't.
	rec, err := os.ReadFile("../chartalign/testdata/song334.json")
	if err != nil {
		t.Fatal(err)
	}
	for song, chart := range map[int64]string{f.CatalogSong: "song334-new.chart", dup: "song334-old.chart"} {
		body, err := os.ReadFile("../chartalign/testdata/" + chart)
		if err != nil {
			t.Fatal(err)
		}
		store.ExecForTest(t, st, `UPDATE songs SET body = $2 WHERE id = $1`, song, string(body))
		store.ExecForTest(t, st, `
			WITH a AS (INSERT INTO song_audio (variant_id, byte_size, sha256, data, category) VALUES ($1::bigint, 1, 'sha'||$1::text, '\x00', 'youtube') RETURNING id)
			INSERT INTO song_audio_analysis (song_audio_id, source_sha256, analyzer, duration_s, data) SELECT id, 'sha'||$1::text, 'test', 240, $2::jsonb FROM a`,
			song, string(rec))
	}

	html := as(t, st, f.Outsider).page("/big-songbook")
	if !strings.Contains(html, "Le Vent") || !strings.Contains(html, "2 versions") || strings.Contains(html, "Night Drive") {
		t.Error("The Big Songbook: not every public song by work, or a private one")
	}
	if !strings.Contains(html, "follows the recording") || !strings.Contains(html, "differs from the recording") {
		t.Error("no \"follows the recording\" / \"differs\" badges")
	}
	if strings.Contains(html, "duplicates to review") {
		t.Error("the duplicates link shown to someone not on the staff")
	}
	fits, _, err := st.RecordingFits(ctx, []int64{f.CatalogSong, dup})
	if err != nil || fits[f.CatalogSong].Fit != "follows" || fits[dup].Fit != "differs" {
		t.Errorf("the verdicts kept: %+v %v", fits, err)
	}
	if !strings.Contains(as(t, st, f.Guest).page("/big-songbook?from=bands"), "No songs match") {
		t.Error("From bands lists songs no band made public")
	}

	a := as(t, st, admin)
	if !strings.Contains(a.page("/big-songbook"), "1 duplicate to review") {
		t.Error("the staff aren't shown the duplicate to review")
	}
	dups := a.page("/big-songbook/duplicates")
	if !strings.Contains(dups, "same title and artist") || !strings.Contains(dups, fmt.Sprintf(`data-song-id="%d"`, dup)) {
		t.Error("the duplicates review doesn't list Le Vent's two songs")
	}
	if w := as(t, st, f.Core).do("GET", "/big-songbook/duplicates", nil); w.Code != http.StatusForbidden {
		t.Errorf("the duplicates review for a non-admin: %d", w.Code)
	}
	merge := map[string]any{"keep": f.CatalogSong, "drop": dup, "dryRun": true}
	if w := as(t, st, f.Core).do("POST", "/api/big-songbook/merge", merge); w.Code != http.StatusForbidden {
		t.Errorf("a non-admin's merge: %d", w.Code)
	}
	w := a.do("POST", "/api/big-songbook/merge", merge)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), "would") {
		t.Fatalf("the dry run: %d %s", w.Code, w.Body.String())
	}
	if _, err := st.GetVariant(ctx, dup); err != nil {
		t.Fatal("the dry run merged")
	}
	if w := a.do("POST", "/api/big-songbook/merge", map[string]any{"keep": f.CatalogSong, "drop": f.BandSong}); w.Code != http.StatusConflict {
		t.Errorf("merging a private song: %d", w.Code)
	}
	merge["dryRun"] = false
	if w := a.do("POST", "/api/big-songbook/merge", merge); w.Code != http.StatusOK {
		t.Fatalf("the merge: %d %s", w.Code, w.Body.String())
	}
	w = as(t, st, f.Outsider).do("GET", fmt.Sprintf("/song/%d", dup), nil)
	if w.Code != http.StatusMovedPermanently || w.Header().Get("Location") != fmt.Sprintf("/song/%d", f.CatalogSong) {
		t.Errorf("the merged song's link: %d → %q", w.Code, w.Header().Get("Location"))
	}
	if h := a.page(fmt.Sprintf("/variants/%d/history", f.CatalogSong)); !strings.Contains(h, fmt.Sprintf("Merged from song %d", dup)) {
		t.Error("the kept song's history doesn't list the merged one")
	}
}
