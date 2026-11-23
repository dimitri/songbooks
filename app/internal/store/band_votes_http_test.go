package store_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

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
	if w.Code != http.StatusForbidden || !strings.Contains(w.Body.String(), "core members") {
		t.Errorf("a guest's vote: %d %s, want 403 saying only core members vote", w.Code, w.Body.String())
	}
	if _, err := st.CastVote(ctx, b, f.Guest, store.Vote{Stars: 3}); err != store.ErrGuestNoVote {
		t.Errorf("a guest's vote in the store: %v", err)
	}
	if w := as(t, st, f.Outsider).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/votes", f.Band, b), map[string]any{"stars": 3}); w.Code != http.StatusForbidden && w.Code != http.StatusNotFound {
		t.Errorf("an outsider's vote: %d", w.Code)
	}

	// Sorted by the average: Bravo (4), Alpha (3), Charlie (no stars, a
	// no thanks); by the most no thanks: Charlie first.
	order := func(sortBy string) string {
		list, err := st.ListProposals(ctx, f.Band, store.ProposalFilter{Status: "open", Sort: sortBy})
		if err != nil {
			t.Fatal(err)
		}
		var out []string
		for _, p := range list {
			out = append(out, fmt.Sprintf("%s:%s:%d", p.TargetTitle, p.AvgText(), p.NoThanks))
		}
		return strings.Join(out, " ")
	}
	if got := order("stars"); got != "Bravo:4.0:0 Alpha:3.0:0 Charlie::1" {
		t.Errorf("by stars: %s", got)
	}
	if got := order("no_thanks"); !strings.HasPrefix(got, "Charlie::1") {
		t.Errorf("by no thanks: %s", got)
	}
	if got := order("newest"); got != "Charlie::1 Bravo:4.0:0 Alpha:3.0:0" {
		t.Errorf("newest: %s", got)
	}
	list, _ := st.ListProposals(ctx, f.Band, store.ProposalFilter{Sort: "stars"})
	if list[1].Positive != 1 || list[1].Negative != 1 {
		t.Errorf("Alpha's split: %d for, %d against", list[1].Positive, list[1].Negative)
	}

	// A vote changed, then taken back: not voted, never 0 stars.
	ans = vote(t, st, f, f.Cores[1], a, map[string]any{"stars": 3}, http.StatusOK)
	if ans.Tally.AvgText != "3.5" {
		t.Errorf("changed to 3: average %q, want 3.5", ans.Tally.AvgText)
	}
	ans = vote(t, st, f, f.Cores[1], a, map[string]any{"clear": true}, http.StatusOK)
	if ans.Tally.Ratings != 1 || ans.Tally.Mine != nil || ans.Tally.Voted != 1 || len(ans.Tally.NotVoted) != 2 {
		t.Errorf("taken back: %+v", ans.Tally)
	}
	history, err := st.ProposalVoteLog(ctx, a)
	if err != nil || len(history) != 4 {
		t.Errorf("history: %d lines (%v), want 4: every vote cast, changed or taken back", len(history), err)
	}

	// The tally answers as the page follows it; the To vote page shows
	// the cards sorted, the voted count, and no control for a guest.
	if w := as(t, st, f.Core).do("GET", fmt.Sprintf("/api/bands/%d/proposals/%d/tally", f.Band, b), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"voted":1`) {
		t.Errorf("tally: %d %s", w.Code, w.Body.String())
	}
	page := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/vote", f.Band))
	if !(strings.Index(page, ">Bravo<") < strings.Index(page, ">Alpha<") && strings.Index(page, ">Alpha<") < strings.Index(page, ">Charlie<")) {
		t.Error("the To vote page isn't sorted by stars")
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

func TestNoThanksRules(t *testing.T) {
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

	// block (the default): every core member voted, a no thanks stands:
	// still open, and no one accepts it by hand until it's taken back.
	p := f.propose(t, st, "Held")
	vote(t, st, f, f.Cores[0], p, map[string]any{"stars": 4}, http.StatusOK)
	vote(t, st, f, f.Cores[1], p, map[string]any{"stars": 4}, http.StatusOK)
	if a := vote(t, st, f, f.Cores[2], p, map[string]any{"noThanks": true}, http.StatusOK); a.Status != "open" || !a.Tally.Blocked {
		t.Errorf("held back: %+v", a)
	}
	w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/accept", f.Band, p), nil)
	if w.Code != http.StatusConflict || !strings.Contains(w.Body.String(), "no thanks") {
		t.Errorf("accepting it by hand: %d %s, want 409", w.Code, w.Body.String())
	}
	// Changing it to 3 stars: every core member voted, for it: in.
	if a := vote(t, st, f, f.Cores[2], p, map[string]any{"stars": 3}, http.StatusOK); a.Status != "accepted" {
		t.Errorf("once the no thanks is gone: %q, want accepted", a.Status)
	}
	if entries, _ := st.ListBandCatalog(ctx, f.Band); !hasSong(entries, mustTarget(t, st, p)) {
		t.Error("accepted, but not in the songbook")
	}
	vote(t, st, f, f.Cores[0], p, map[string]any{"stars": 1}, http.StatusConflict) // decided: no more votes

	// veto: one core member's no thanks turns it down at once.
	if w := as(t, st, f.Guest).do("PUT", fmt.Sprintf("/api/bands/%d/proposal-rule", f.Band), map[string]any{"rule": "veto"}); w.Code != http.StatusForbidden {
		t.Errorf("a guest changes the rule: %d", w.Code)
	}
	if w := as(t, st, f.Core).do("PUT", fmt.Sprintf("/api/bands/%d/proposal-rule", f.Band), map[string]any{"rule": "veto"}); w.Code != http.StatusOK {
		t.Fatalf("setting veto: %d %s", w.Code, w.Body.String())
	}
	v := f.propose(t, st, "Vetoed")
	if a := vote(t, st, f, f.Cores[1], v, map[string]any{"noThanks": true}, http.StatusOK); a.Status != "rejected" {
		t.Errorf("veto: %q", a.Status)
	}

	// count: a no thanks only counts against.
	if err := st.SetBandProposalRule(ctx, f.Band, store.NoThanksCount); err != nil {
		t.Fatal(err)
	}
	cnt := f.propose(t, st, "Counted")
	vote(t, st, f, f.Cores[0], cnt, map[string]any{"stars": 4}, http.StatusOK)
	vote(t, st, f, f.Cores[1], cnt, map[string]any{"noThanks": true}, http.StatusOK)
	if a := vote(t, st, f, f.Cores[2], cnt, map[string]any{"stars": 3}, http.StatusOK); a.Status != "accepted" {
		t.Errorf("count: %q, want accepted (2 for, 1 no thanks, average 3.5)", a.Status)
	}
	// More than half of the core members saying no thanks: out, whatever
	// the rule.
	maj := f.propose(t, st, "Majority")
	vote(t, st, f, f.Cores[0], maj, map[string]any{"noThanks": true}, http.StatusOK)
	if a := vote(t, st, f, f.Cores[1], maj, map[string]any{"noThanks": true}, http.StatusOK); a.Status != "rejected" {
		t.Errorf("most say no thanks: %q", a.Status)
	}

	// By hand: a core member decides, a guest doesn't.
	hand := f.propose(t, st, "By hand")
	if w := as(t, st, f.Guest).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/reject", f.Band, hand), nil); w.Code != http.StatusForbidden {
		t.Errorf("a guest turns it down: %d", w.Code)
	}
	if w := as(t, st, f.Core).do("POST", fmt.Sprintf("/api/bands/%d/proposals/%d/reject", f.Band, hand), nil); w.Code != http.StatusOK || status(hand) != "rejected" {
		t.Errorf("a core member turns it down: %d %s", w.Code, status(hand))
	}
	if err := st.SetBandProposalRule(ctx, f.Band, "maybe"); err == nil {
		t.Error("an unknown rule taken")
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

	// Proposed for the set list, voted in by the three core members: in
	// the songbook and at the end of the set list.
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
	if p, _ := st.GetProposal(ctx, created.ID); p.Status != "accepted" {
		t.Fatalf("status %q, want accepted", p.Status)
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

// TestListeningParty: the host's player says what plays; every member's
// phone (the party page) follows it, with the song's vote; the event's
// page leads to it.
func TestListeningParty(t *testing.T) {
	st := store.NewTestStore(t)
	ctx := context.Background()
	f := newVotesBand(t, st)
	p := f.propose(t, st, "Tonight")
	ev, err := st.CreateEvent(ctx, "band", f.Band, "listening_party", "Listening party", "", "", time.Now().Add(time.Hour), nil, "members", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if w := as(t, st, f.Cores[1]).do("GET", fmt.Sprintf("/api/bands/%d/party", f.Band), nil); w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"now":null`) {
		t.Errorf("nothing playing: %d %s", w.Code, w.Body.String())
	}
	if w := as(t, st, f.Core).do("PUT", fmt.Sprintf("/api/bands/%d/party", f.Band), map[string]any{"proposalId": p, "eventId": ev, "playing": true}); w.Code != http.StatusNoContent {
		t.Fatalf("the host: %d %s", w.Code, w.Body.String())
	}
	vote(t, st, f, f.Cores[1], p, map[string]any{"stars": 3}, http.StatusOK)
	w := as(t, st, f.Cores[1]).do("GET", fmt.Sprintf("/api/bands/%d/party", f.Band), nil)
	var got struct {
		Now struct {
			ProposalID int64 `json:"proposalId"`
			EventID    int64 `json:"eventId"`
			Playing    bool  `json:"playing"`
			Host       string
		} `json:"now"`
		Tally store.ProposalTally `json:"tally"`
	}
	if json.Unmarshal(w.Body.Bytes(), &got) != nil || got.Now.ProposalID != p || got.Now.EventID != ev || !got.Now.Playing || got.Now.Host != "Core" || got.Tally.Mine == nil || got.Tally.Mine.Stars != 3 {
		t.Errorf("a member's phone: %s", w.Body.String())
	}
	// Another band's proposal isn't this party's.
	other, err := st.CreateBand(ctx, "Elsewhere", f.Core)
	if err != nil {
		t.Fatal(err)
	}
	if w := as(t, st, f.Core).do("PUT", fmt.Sprintf("/api/bands/%d/party", other), map[string]any{"proposalId": p}); w.Code != http.StatusNotFound {
		t.Errorf("another band's proposal at the party: %d", w.Code)
	}
	page := as(t, st, f.Guest).page(fmt.Sprintf("/bands/%d/catalog/discover?event=%d", f.Band, ev))
	if !strings.Contains(page, "Listening party") || strings.Contains(page, "canVote: true") {
		t.Error("the party page for a guest")
	}
	host := as(t, st, f.Core).page(fmt.Sprintf("/bands/%d/catalog/vote/listen?party=1&event=%d", f.Band, ev))
	if !strings.Contains(host, `<span class="pl-h1-sub">listening party</span>`) {
		t.Error("the host's player doesn't say so")
	}
	if !strings.Contains(as(t, st, f.Core).page(fmt.Sprintf("/events/%d", ev)), fmt.Sprintf("/bands/%d/catalog/discover?event=%d", f.Band, ev)) {
		t.Error("the event's page doesn't lead to the party")
	}
}
