package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"

	"app/internal/httpapi"
	"app/internal/store"
)

// A band's votes over HTTP (docs/band-votes.md): stars and no thanks,
// the averages and the sorting, a vote changed and taken back, the
// no-thanks rules, who votes (core members, never a guest), and a song
// proposed through the new song-adding flow.

type votesBand struct {
	store.BandFixture
	Cores []int64 // f.Core first, then two more core members
}

func newVotesBand(t *testing.T, st *store.Store) votesBand {
	t.Helper()
	ctx := context.Background()
	f := votesBand{BandFixture: store.NewBandFixture(t, st)}
	f.Cores = []int64{f.Core}
	for _, name := range []string{"bea", "cyd"} {
		id, err := st.CreatePerson(ctx, name, strings.ToUpper(name[:1])+name[1:], name+"@example.com")
		if err != nil {
			t.Fatal(err)
		}
		if _, err := st.AddBandMember(ctx, f.Band, id, "core", f.Core); err != nil {
			t.Fatal(err)
		}
		f.Cores = append(f.Cores, id)
	}
	return f
}

func (f votesBand) propose(t *testing.T, st *store.Store, title string) int64 {
	t.Helper()
	ctx := context.Background()
	song, err := st.CreateVariant(ctx, store.OwnerCatalog, 0, title, "Someone", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	id, err := st.CreateProposal(ctx, f.Band, f.Core, "new_song", song, nil)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

type voteAnswer struct {
	Status string              `json:"status"`
	Tally  store.ProposalTally `json:"tally"`
}

func vote(t *testing.T, st *store.Store, f votesBand, person, proposal int64, body map[string]any, want int) voteAnswer {
	t.Helper()
	w := as(t, st, person).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/votes", f.Band, proposal), body)
	if w.Code != want {
		t.Fatalf("vote %v by %d: %d %s, want %d", body, person, w.Code, w.Body.String(), want)
	}
	var a voteAnswer
	_ = json.Unmarshal(w.Body.Bytes(), &a)
	return a
}

func TestStarVotes(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	a, b, c := f.propose(t, st, "Alpha"), f.propose(t, st, "Bravo"), f.propose(t, st, "Charlie")

	// Stars, 1 to 4; the answer is the tally as the voter sees it.
	ans := vote(t, st, f, f.Cores[0], a, map[string]any{"stars": 4}, http.StatusOK)
	if ans.Status != "open" || ans.Tally.Ratings != 1 || ans.Tally.Avg != 4 || ans.Tally.Mine == nil || ans.Tally.Mine.Stars != 4 {
		t.Errorf("after a 4: %+v", ans)
	}
	if ans.Tally.Voted != 1 || ans.Tally.Members != 3 {
		t.Errorf("voted %d/%d, want 1/3: the core members", ans.Tally.Voted, ans.Tally.Members)
	}
	vote(t, st, f, f.Cores[1], a, map[string]any{"stars": 2}, http.StatusOK)
	vote(t, st, f, f.Cores[0], b, map[string]any{"stars": 4}, http.StatusOK)
	vote(t, st, f, f.Cores[0], c, map[string]any{"noThanks": true}, http.StatusOK)
	// 5 stars, 0 stars, both at once: refused.
	for _, bad := range []map[string]any{{"stars": 5}, {"stars": 0}, {"stars": 3, "noThanks": true}, {}} {
		vote(t, st, f, f.Cores[2], b, bad, http.StatusBadRequest)
	}

	// A guest doesn't vote; someone outside the band neither.
	w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/votes", f.Band, b), map[string]any{"stars": 3})
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "doesn’t vote") {
		t.Errorf("a guest's vote: %d %s, want 403 saying their role doesn't vote", w.Code, w.Body.String())
	}
	if _, err := st.CastVote(ctx, b, f.Guest, store.Vote{Stars: 3}); err != store.ErrGuestNoVote {
		t.Errorf("a guest's vote in the store: %v", err)
	}
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/votes", f.Band, b), map[string]any{"stars": 3}); w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Errorf("an outsider's vote: %d", w.Code)
	}

	// Sorted by the average: Bravo (4), Alpha (3), Charlie (no stars, a
	// veto: after); by the most no thanks: Charlie first. Internal lists
	// (no viewer) see everything.
	order := func(sortBy string, viewer int64) string {
		list, err := st.ListProposals(ctx, f.Band, store.ProposalFilter{Status: "open", Sort: sortBy, Viewer: viewer})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range list {
			h := ""
			if p.Hidden {
				h = "?"
			}
			out = append(out, fmt.Sprintf("%s%s:%s:%d", h, p.TargetTitle, p.AvgText(), p.NoThanks))
		}
		return strings.Join(out, " ")
	}
	if got := order("stars", 0); got != "Bravo:4.0:0 Alpha:3.0:0 Charlie::1" {
		t.Errorf("by stars: %s", got)
	}
	if got := order("no_thanks", 0); !strings.HasPrefix(got, "Charlie::1") {
		t.Errorf("by no thanks: %s", got)
	}
	if got := order("newest", 0); got != "Charlie::1 Bravo:4.0:0 Alpha:3.0:0" {
		t.Errorf("newest: %s", got)
	}
	// Cyd voted on none: everything hidden, newest first, nothing told by
	// the order. Bea voted on Alpha only: Charlie and Bravo hidden, first.
	if got := order("stars", f.Cores[2]); got != "?Charlie::0 ?Bravo::0 ?Alpha::0" {
		t.Errorf("by stars, for someone who voted on none: %s", got)
	}
	if got := order("stars", f.Cores[1]); got != "?Charlie::0 ?Bravo::0 Alpha:3.0:0" {
		t.Errorf("by stars, for Bea: %s", got)
	}
	list, _ := st.ListProposals(ctx, f.Band, store.ProposalFilter{Sort: "stars"})
	if list[1].Positive != 1 || list[1].Negative != 1 {
		t.Errorf("Alpha's split: %d for, %d against", list[1].Positive, list[1].Negative)
	}

	// The votes stay open: nothing decided by them.
	for _, id := range []int64{a, b, c} {
		if p, _ := st.GetProposal(ctx, id); p.Status != "open" {
			t.Errorf("proposal %d decided by the votes: %s", id, p.Status)
		}
	}

	// A vote changed, then taken back: not voted, never 0 stars.
	ans = vote(t, st, f, f.Cores[1], a, map[string]any{"stars": 3}, http.StatusOK)
	if ans.Tally.AvgText != "3.5" {
		t.Errorf("changed to 3: average %q, want 3.5", ans.Tally.AvgText)
	}
	ans = vote(t, st, f, f.Cores[1], a, map[string]any{"clear": true}, http.StatusOK)
	if !ans.Tally.Hidden || ans.Tally.Ratings != 0 || ans.Tally.Mine != nil || ans.Tally.Voted != 1 || ans.Tally.Members != 3 || len(ans.Tally.NotVoted) != 0 {
		t.Errorf("taken back: the others' votes hidden again, only 1/3 voted: %+v", ans.Tally)
	}
	history, err := st.ProposalVoteLog(ctx, a)
	if err != nil || len(history) != 4 {
		t.Errorf("history: %d lines (%v), want 4: every vote cast, changed or taken back", len(history), err)
	}

	// The tally answers as the page follows it; the To vote page shows
	// the cards sorted, the voted count, and no control for a guest.
	if w := as(t, st, f.Core).do("GET", fmt.Sprintf("/api/bands/%d/proposals/%d/tally", f.Band, b), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"voted":1`) || strings.Contains(w.Body.String(), `"hidden"`) {
		t.Errorf("tally, for one who voted: %d %s", w.Code, w.Body.String())
	}
	// Before voting: only n/m, in the tally, the page, the player's data,
	// the proposal's history; a guest too, until it's decided.
	for _, who := range []int64{f.Cores[2], f.Guest} {
		w := as(t, st, who).do("GET", fmt.Sprintf("/api/bands/%d/proposals/%d/tally", f.Band, b), nil)
		body := w.Body.String()
		if !strings.Contains(body, `"hidden":true`) || !strings.Contains(body, `"voted":1,"members":3`) || strings.Contains(body, "Core") || strings.Contains(body, `"avg":4`) {
			t.Errorf("tally before voting, for %d: %s", who, body)
		}
		for _, path := range []string{"/bands/%d/catalog/vote", "/bands/%d/catalog/vote/listen", "/bands/%d/catalog/discover"} {
			page := as(t, st, who).page(fmt.Sprintf(path, f.Band))
			if strings.Contains(page, "&#34;avgText&#34;:&#34;4.0&#34;") || strings.Contains(page, `"avgText":"4.0"`) || strings.Contains(page, `\u0022avgText\u0022:\u00224.0\u0022`) {
				t.Errorf("%s shows Bravo's average to %d before voting", path, who)
			}
		}
	}
	if hist := as(t, st, f.Cores[2]).page(fmt.Sprintf("/bands/%d/proposals/%d", f.Band, b)); strings.Contains(hist, "proposal-history-who") {
		t.Error("the history of the others' votes shows before voting")
	}
	if hist := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/proposals/%d", f.Band, b)); !strings.Contains(hist, "proposal-history-who") {
		t.Error("the history doesn't show once voted")
	}
	page := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/vote", f.Band))
	if !(strings.Index(page, ">Bravo<") < strings.Index(page, ">Alpha<") && strings.Index(page, ">Alpha<") < strings.Index(page, ">Charlie<")) {
		t.Error("the To vote page isn't sorted by stars, the vetoed one last")
	}
	if !strings.Contains(page, "data-ctl") {
		t.Error("no vote control for a core member")
	}
	guestPage := as(t, st, f.Guest).page(fmt.Sprintf("/bands/%d/catalog/vote", f.Band))
	if strings.Contains(guestPage, "data-ctl") || !strings.Contains(guestPage, "guest member") {
		t.Error("a guest is offered the vote")
	}
	as(t, st, f.Guest).page(fmt.Sprintf("/bands/%d/catalog/vote/listen", f.Band))
	as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/discover", f.Band))
	as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/proposals/%d", f.Band, a))
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/bands/%d/catalog/vote", f.Band), nil); w.Code == http.StatusOK {
		t.Error("someone outside the band sees its proposals")
	}
}

// TestVotesAreIndicative: the votes decide nothing — every core member
// voted, for or against, the proposal stays open — and a core member
// adds a song, or turns it down, whatever they say, a veto included.
func TestVotesAreIndicative(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	status := func(id int64) string {
		p, err := st.GetProposal(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		return p.Status
	}

	// Every core member loves it: still open, for a core member to add.
	loved := f.propose(t, st, "Loved")
	for _, c := range f.Cores {
		if a := vote(t, st, f, c, loved, map[string]any{"stars": 4}, http.StatusOK); a.Status != "open" {
			t.Fatalf("decided by the votes: %q", a.Status)
		}
	}
	if a := vote(t, st, f, f.Cores[0], loved, map[string]any{"stars": 4}, http.StatusOK); !a.Tally.Liked || a.Tally.Vetoed {
		t.Errorf("the band likes it, says the tally: %+v", a.Tally)
	}

	// Everyone says no thanks: still open.
	vetoed := f.propose(t, st, "Vetoed")
	for _, c := range f.Cores {
		vote(t, st, f, c, vetoed, map[string]any{"noThanks": true}, http.StatusOK)
	}
	if status(vetoed) != "open" {
		t.Errorf("turned down by the no thanks: %s", status(vetoed))
	}

	// A veto standing doesn't stop a core member adding it.
	held := f.propose(t, st, "Held")
	vote(t, st, f, f.Cores[0], held, map[string]any{"stars": 4}, http.StatusOK)
	if a := vote(t, st, f, f.Cores[1], held, map[string]any{"noThanks": true}, http.StatusOK); !a.Tally.Vetoed || a.Tally.Liked {
		t.Errorf("a veto: %+v", a.Tally)
	}
	w := as(t, st, f.Cores[2]).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/accept", f.Band, held), nil)
	if w.Code != http.StatusOK || status(held) != "accepted" {
		t.Errorf("added by hand, a veto standing, by one who didn't vote: %d %s", w.Code, w.Body.String())
	}
	if entries, _ := st.ListBandCatalog(ctx, f.Band); !hasSong(entries, mustTarget(t, st, held)) {
		t.Error("added, but not in the songbook")
	}
	vote(t, st, f, f.Cores[2], held, map[string]any{"stars": 1}, http.StatusConflict) // decided: no more votes
	// Decided: everyone sees everything, a guest too.
	tw := as(t, st, f.Guest).do("GET", fmt.Sprintf("/api/bands/%d/proposals/%d/tally", f.Band, held), nil)
	if strings.Contains(tw.Body.String(), `"hidden":true`) || !strings.Contains(tw.Body.String(), `"vetoed":true`) {
		t.Errorf("a decided proposal, for a guest: %s", tw.Body.String())
	}

	// Loved, turned down by hand all the same.
	if w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/reject", f.Band, loved), nil); w.Code != http.StatusOK || status(loved) != "rejected" {
		t.Errorf("turned down by hand: %d %s", w.Code, status(loved))
	}

	// Any member decides, a guest too; someone outside the band doesn't.
	hand := f.propose(t, st, "By hand")
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/accept", f.Band, hand), nil); w.Code == http.StatusOK {
		t.Error("someone outside the band added it")
	}
	if w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/accept", f.Band, hand), nil); w.Code != http.StatusOK || status(hand) != "accepted" {
		t.Errorf("a guest adds it: %d %s", w.Code, w.Body.String())
	}
	down := f.propose(t, st, "Down")
	if w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/reject", f.Band, down), nil); w.Code != http.StatusOK || status(down) != "rejected" {
		t.Errorf("a guest turns it down: %d %s", w.Code, w.Body.String())
	}
	if ok, _ := st.MayDecideProposal(ctx, f.Band, f.Guest); !ok {
		t.Error("MayDecideProposal: a guest")
	}
	if ok, _ := st.MayVote(ctx, f.Band, f.Guest); ok {
		t.Error("MayVote: a guest")
	}
	// The setting that made a no thanks decide is gone.
	if w := as(t, st, f.Core).do("PUT", fmt.Sprintf("/api/bands/%d/proposal-rule", f.Band), map[string]any{"rule": "veto"}); w.Code == http.StatusOK {
		t.Error("the no-thanks rule still taken")
	}
}

func mustTarget(t *testing.T, st *store.Store, proposal int64) int64 {
	t.Helper()
	p, err := st.GetProposal(context.Background(), proposal)
	if err != nil || p.TargetVariantID == nil {
		t.Fatal(err)
	}
	return *p.TargetVariantID
}

func hasSong(entries []store.BandCatalogEntry, song int64) bool {
	for _, e := range entries {
		if e.VariantID == song {
			return true
		}
	}
	return false
}

// TestProposalThroughNewFlow: a song typed (or pasted) is made for the
// band with its chart fetched, fixed in the check step, proposed for a
// set list, voted in — then it's in the songbook and the set list.
func TestProposalThroughNewFlow(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	httpapi.ProposalDraft = func(ctx context.Context, _ *store.Store, title, artist string) (*httpapi.ProposalDraftResult, error) {
		return &httpapi.ProposalDraftResult{
			Text: "title: x\ntime: 3/4\n\n[Intro]\ngrid: | G | C |\n\n[Verse]\ngrid: | G | C | G | D |\nla la\nlo lo\n\n[Chorus]\ngrid: | C | D |\nyeah\n",
			URL:  "https://tabs.ultimate-guitar.com/tab/1", Notes: []string{"iReal Pro grids used in 0 sections"},
		}, nil
	}
	defer func() { httpapi.ProposalDraft = nil }()
	c := as(t, st, f.Cores[1])

	// Typed: nothing found, a new song to make.
	w := c.do("GET", fmt.Sprintf("/api/bands/%d/proposal-lookup?q=%s", f.Band, "Brand%20New%20%E2%80%94%20Somebody"), nil)
	var look struct {
		Title, Artist string
		Match         *store.SongMatch
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &look) != nil || look.Title != "Brand New" || look.Artist != "Somebody" || look.Match != nil {
		t.Fatalf("lookup: %d %s", w.Code, w.Body.String())
	}
	// The catalogue's song is found, and said to be in the songbook.
	w = c.do("GET", fmt.Sprintf("/api/bands/%d/proposal-lookup?q=%s", f.Band, "Le%20Vent%20%E2%80%94%20Someone"), nil)
	if !strings.Contains(w.Body.String(), `"inRepertoire":true`) {
		t.Errorf("the band's song: %s", w.Body.String())
	}

	// Made, its chart fetched: the band's, in no songbook yet.
	if w := c.do("POST", fmt.Sprintf("/api/bands/%d/proposal-drafts", f.Band), map[string]any{"title": " "}); w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), `"field":"title"`) {
		t.Errorf("no title: %d %s", w.Code, w.Body.String())
	}
	w = c.do("POST", fmt.Sprintf("/api/bands/%d/proposal-drafts", f.Band), map[string]any{"title": "Brand New", "artist": "Somebody"})
	var d struct {
		SongID   int64  `json:"songId"`
		Version  string `json:"version"`
		Sections []struct {
			Label, Grid string
			Lines       int
		} `json:"sections"`
		Source struct {
			Lyrics bool
			Grids  int
			IReal  string
		} `json:"source"`
	}
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &d) != nil {
		t.Fatalf("draft: %d %s", w.Code, w.Body.String())
	}
	if len(d.Sections) != 3 || d.Sections[1].Lines != 2 || d.Sections[1].Grid == "" || !d.Source.Lyrics || d.Source.Grids != 3 || d.Source.IReal != "" {
		t.Errorf("draft: %+v", d)
	}
	if strings.Contains(w.Body.String(), "la la") {
		t.Error("the draft answers the lyrics; only their count")
	}
	r, err := st.SongRightsFor(ctx, f.Cores[2], d.SongID)
	if err != nil || r.Owner != store.BandOwner(f.Band) || !r.Edit || r.Public {
		t.Errorf("the song made: %+v %v, want the band's, private, its members' to fix", r, err)
	}
	if entries, _ := st.ListBandCatalog(ctx, f.Band); hasSong(entries, d.SongID) {
		t.Error("in the songbook before the vote")
	}
	if w := as(t, st, f.Outsider).do("GET", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", f.Band, d.SongID), nil); w.Code == http.StatusOK {
		t.Error("someone outside the band reads the draft")
	}

	// Fixed: a grid that doesn't read is refused under its field; then
	// the intro dropped, the verse renamed and its grid rewritten, the
	// chorus kept with its lyrics, the label and tempo set.
	put := func(sections []map[string]any) *testResponse {
		w := c.do("PUT", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", f.Band, d.SongID), map[string]any{
			"version": d.Version, "title": "Brand New", "artist": "Somebody", "versionName": "Live", "tempo": "96", "sections": sections,
		})
		return &testResponse{w.Code, w.Body.String()}
	}
	if res := put([]map[string]any{{"from": 1, "label": "Verse", "grid": "nothing here"}}); res.code != http.StatusBadRequest || !strings.Contains(res.body, `"field":"grid"`) {
		t.Errorf("an unreadable grid: %d %s", res.code, res.body)
	}
	if res := put([]map[string]any{{"from": 1, "label": "", "grid": ""}}); res.code != http.StatusBadRequest || !strings.Contains(res.body, `"field":"label"`) {
		t.Errorf("a section without a name: %d %s", res.code, res.body)
	}
	if res := put([]map[string]any{{"from": 1, "label": "Couplet", "grid": "| Em | C | G | D |"}, {"from": 2, "label": "Chorus", "grid": "| C | D |"}, {"from": -1, "label": "Outro", "grid": "G"}}); res.code != http.StatusOK {
		t.Fatalf("fixing: %d %s", res.code, res.body)
	}
	sg, _, err := st.LoadVariant(d.SongID)
	if err != nil {
		t.Fatal(err)
	}
	var labels []string
	for _, s := range sg.Sections {
		labels = append(labels, s.Label)
	}
	if strings.Join(labels, ",") != "Couplet,Chorus,Outro" || len(sg.Sections[0].Lyrics) != 2 || sg.Sections[0].Measures[0].Tokens[0] != "Em" || len(sg.Sections[1].Lyrics) != 1 || sg.Get("tempo") != "96" {
		t.Errorf("fixed: %v, %d lyric lines in the verse, tempo %q", labels, len(sg.Sections[0].Lyrics), sg.Get("tempo"))
	}
	if v, _ := st.GetVariant(ctx, d.SongID); v.VersionName != "Live" {
		t.Errorf("label %q", v.VersionName)
	}

	// Proposed for the set list, voted for by the three core members,
	// added by hand: in the songbook and at the end of the set list.
	w = c.do("POST", fmt.Sprintf("/api/bands/%d/proposals", f.Band), map[string]any{
		"kind": "new_song", "targetVariantId": d.SongID, "payload": map[string]any{"note": "for the encore", "target": "setlist", "setlistId": f.SetlistID},
	})
	var created struct{ ID int64 }
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &created) != nil {
		t.Fatalf("proposing: %d %s", w.Code, w.Body.String())
	}
	p, _ := st.GetProposal(ctx, created.ID)
	if p.Note != "for the encore" || p.Target != "setlist" || p.SetlistID != f.SetlistID {
		t.Errorf("the proposal: %+v", p)
	}
	for i, n := range []int{4, 3, 3} {
		vote(t, st, f, f.Cores[i], created.ID, map[string]any{"stars": n}, http.StatusOK)
	}
	if p, _ := st.GetProposal(ctx, created.ID); p.Status != "open" {
		t.Fatalf("status %q after the last vote, want open: the votes decide nothing", p.Status)
	}
	if w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/accept", f.Band, created.ID), nil); w.Code != http.StatusOK {
		t.Fatalf("adding it: %d %s", w.Code, w.Body.String())
	}
	if entries, _ := st.ListBandCatalog(ctx, f.Band); !hasSong(entries, d.SongID) {
		t.Error("accepted, not in the songbook")
	}
	l, _, _, err := st.LoadSetlistByID(ctx, f.SetlistID)
	if err != nil {
		t.Fatal(err)
	}
	last := l.Sets[len(l.Sets)-1].Entries
	if last[len(last)-1].VariantID != d.SongID {
		t.Error("not at the end of the set list")
	}

	// Another set list than the band's isn't a target; a draft never
	// sent is dropped by its maker only.
	w = c.do("POST", fmt.Sprintf("/api/bands/%d/proposals", f.Band), map[string]any{
		"kind": "new_song", "targetVariantId": f.CatalogSong, "payload": map[string]any{"target": "setlist", "setlistId": 999999},
	})
	if w.Code == http.StatusOK {
		t.Error("an unknown set list taken as target")
	}
	w = c.do("POST", fmt.Sprintf("/api/bands/%d/proposal-drafts", f.Band), map[string]any{"title": "Never sent"})
	var unsent struct{ SongID int64 }
	_ = json.Unmarshal(w.Body.Bytes(), &unsent)
	if w := as(t, st, f.Cores[0]).do("DELETE", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", f.Band, unsent.SongID), nil); w.Code == http.StatusNoContent {
		t.Error("someone else dropped the draft")
	}
	if w := c.do("DELETE", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", f.Band, unsent.SongID), nil); w.Code != http.StatusNoContent {
		t.Errorf("dropping the unsent draft: %d %s", w.Code, w.Body.String())
	}
	if w := c.do("DELETE", fmt.Sprintf("/api/bands/%d/proposal-drafts/%d", f.Band, d.SongID), nil); w.Code == http.StatusNoContent {
		t.Error("a proposed song dropped as a draft")
	}
}

type testResponse struct {
	code int
	body string
}

// TestBandListens: Discover and Influences — any member shares a song
// by its YouTube link, the list and the player show it, only who shared
// it removes it, a shared song proposed for a vote says so; no sync
// between devices left.
func TestBandListens(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	add := func(who int64, body map[string]any) (int, int64) {
		w := as(t, st, who).do("POST", fmt.Sprintf("/api/bands/%d/listens", f.Band), body)
		var out struct{ ID int64 }
		_ = json.Unmarshal(w.Body.Bytes(), &out)
		return w.Code, out.ID
	}
	// A guest shares to Discover; a core member adds an influence, tagged.
	code, d := add(f.Guest, map[string]any{"pool": "discover", "url": "https://youtu.be/dQw4w9WgXcQ", "title": "Shared One", "artist": "Somebody", "note": "it's us"})
	if code != http.StatusOK {
		t.Fatalf("a guest shares: %d", code)
	}
	code, inf := add(f.Core, map[string]any{"pool": "influence", "url": "https://www.youtube.com/watch?v=abcdefghijk", "title": "Big Influence", "note": "the drum feel", "tags": []string{" Groove", "groove", "#harmonies", ""}})
	if code != http.StatusOK {
		t.Fatalf("an influence: %d", code)
	}
	// Not a YouTube video, an unknown pool, someone outside the band: no.
	if code, _ := add(f.Core, map[string]any{"pool": "discover", "url": "https://example.com/x"}); code != http.StatusBadRequest {
		t.Errorf("a link that isn't a video: %d", code)
	}
	if code, _ := add(f.Core, map[string]any{"pool": "elsewhere", "url": "https://youtu.be/dQw4w9WgXcQ"}); code != http.StatusBadRequest {
		t.Errorf("an unknown pool: %d", code)
	}
	if code, _ := add(f.Outsider, map[string]any{"pool": "discover", "url": "https://youtu.be/dQw4w9WgXcQ"}); code == http.StatusOK {
		t.Error("someone outside the band shares")
	}
	infl, err := st.ListBandListens(ctx, f.Band, store.PoolInfluence)
	if err != nil || len(infl) != 1 || infl[0].ID != inf || strings.Join(infl[0].Tags, ",") != "groove,harmonies" || infl[0].VideoID != "abcdefghijk" || infl[0].AddedByName != "Core" {
		t.Errorf("influences: %+v %v", infl, err)
	}
	// The pages and the player.
	page := as(t, st, f.Cores[1]).page(fmt.Sprintf("/bands/%d/catalog/discover", f.Band))
	if !strings.Contains(page, "Shared One") || !strings.Contains(page, "data-listen-propose") || strings.Contains(page, "data-listen-remove") {
		t.Error("Discover, for a member who didn't share the song")
	}
	if page := as(t, st, f.Guest).page(fmt.Sprintf("/bands/%d/catalog/discover", f.Band)); !strings.Contains(page, "data-listen-remove") {
		t.Error("who shared it can't remove it")
	}
	if page := as(t, st, f.Guest).page(fmt.Sprintf("/bands/%d/catalog/influences", f.Band)); !strings.Contains(page, "#groove") || strings.Contains(page, "data-listen-propose") {
		t.Error("Influences")
	}
	player := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/influences/listen", f.Band))
	if !strings.Contains(player, "abcdefghijk") || !strings.Contains(player, "Watch on YouTube") {
		t.Error("the influences' player")
	}
	// Only who shared it removes it.
	if w := as(t, st, f.Core).do("DELETE", fmt.Sprintf("/api/bands/%d/listens/%d", f.Band, d), nil); w.Code != http.StatusForbidden {
		t.Errorf("someone else removes it: %d", w.Code)
	}
	// Proposed for a vote: marked.
	p := f.propose(t, st, "From Discover")
	if w := as(t, st, f.Core).do("PUT", fmt.Sprintf("/api/bands/%d/listens/%d/proposal", f.Band, d), map[string]any{"proposalId": p}); w.Code != http.StatusNoContent {
		t.Errorf("marking it proposed: %d %s", w.Code, w.Body.String())
	}
	if page := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/discover", f.Band)); !strings.Contains(page, fmt.Sprintf("/bands/%d/proposals/%d", f.Band, p)) {
		t.Error("Discover doesn't say it was proposed")
	}
	if w := as(t, st, f.Guest).do("DELETE", fmt.Sprintf("/api/bands/%d/listens/%d", f.Band, d), nil); w.Code != http.StatusNoContent {
		t.Errorf("who shared it removes it: %d", w.Code)
	}
	if list, _ := st.ListBandListens(ctx, f.Band, store.PoolDiscover); len(list) != 0 {
		t.Errorf("still there: %+v", list)
	}
	// No sync between devices: the party's addresses are gone.
	for _, m := range []string{"GET", "PUT"} {
		if w := as(t, st, f.Core).do(m, fmt.Sprintf("/api/bands/%d/party", f.Band), map[string]any{}); w.Code == http.StatusNoContent || strings.Contains(w.Body.String(), `"now"`) {
			t.Errorf("%s party still answers: %d %s", m, w.Code, w.Body.String())
		}
	}
}
