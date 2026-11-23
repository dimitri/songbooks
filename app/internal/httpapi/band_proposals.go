package httpapi

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/song"
	"app/internal/store"
	"app/internal/webassets"
)

// A band's proposals and their votes (docs/band-votes.md; the rules in
// internal/store/band_proposals.go): the To vote view of its songbook
// (the proposals as cards, sorted by stars, a vote on each), a proposal's
// page, and the player of the songs to vote on (the set lists' playlist
// player, playlist.html, with the stars under it). The band's other
// pools of songs to listen to, Discover and Influences, are
// band_listens.go's.
//
// Proposing a song goes through the new song-adding flow (quick add,
// quick_add.go): a YouTube link or "Title — Artist" is looked up — the
// song the band may link, or a new one made for the band with the video
// as its reference, its lyrics and chord grids fetched (ProposalDraft)
// and its recording downloaded (ProposalAudio) — then the proposer
// checks and fixes it (title, artist, label, sections and grids) before
// sending it, with a note and where it should go.

// ProposalDraftResult is a new song's chart fetched for a proposal: its
// text (lyrics in their sections, the grids), where it came from (the
// Ultimate Guitar tab, the iReal Pro chart used) and what was done.
type ProposalDraftResult struct {
	Text  string
	URL   string
	IReal string
	Notes []string
}

// ProposalDraft fetches a new song's chart (the editor's Import, as
// songimport.VocalDraft: Ultimate Guitar's lyrics and grids, iReal Pro's
// grids where its chart matches); cmd/songbooks sets it, nil (tests)
// fetches nothing.
var ProposalDraft func(ctx context.Context, st *store.Store, title, artist string) (*ProposalDraftResult, error)

// ProposalAudio downloads a video's sound as MP3 (ytaudio.Download), for
// the song made for a proposal to play in the vote's player; nil (tests)
// downloads nothing.
var ProposalAudio func(ctx context.Context, url string) ([]byte, error)

// proposalCard is a proposal as the pages show it: its votes, member by
// member, as the viewer sees them.
type proposalCard struct {
	store.BandProposal
	Tally        store.ProposalTally
	TallyJSON    string
	Thumb        string
	SetlistTitle string
}

func (s *Server) proposalCards(ctx context.Context, bandID, viewer int64, list []store.BandProposal) ([]proposalCard, error) {
	ids := make([]int64, len(list))
	songs := []int64{}
	for i, p := range list {
		ids[i] = p.ID
		if p.TargetVariantID != nil {
			songs = append(songs, *p.TargetVariantID)
		}
	}
	ballots, err := s.store.ProposalBallots(ctx, ids)
	if err != nil {
		return nil, err
	}
	members, err := s.store.ListBandMembers(ctx, bandID, false)
	if err != nil {
		return nil, err
	}
	covers, _ := s.store.SongCoverImages(ctx, songs)
	setlists := map[int64]string{}
	if sls, err := s.store.ListSetlistsForBand(ctx, bandID); err == nil {
		for _, sl := range sls {
			setlists[sl.ID] = sl.Title
		}
	}
	out := make([]proposalCard, len(list))
	for i, p := range list {
		// Before the viewer votes, only how many voted (BlindFor).
		c := proposalCard{BandProposal: p, Tally: store.TallyOf(ballots[p.ID], members, viewer).BlindFor(p.Status)}
		if c.Tally.Hidden {
			c.Ratings, c.StarSum, c.Positive, c.Negative, c.NoThanks, c.Hidden = 0, 0, 0, 0, 0, true
		}
		raw, _ := json.Marshal(c.Tally)
		c.TallyJSON = string(raw)
		if p.TargetVariantID != nil {
			c.Thumb = covers[*p.TargetVariantID]
		}
		if v := p.VideoID(); v != "" && (c.Thumb == "" || p.Kind == "new_reference") {
			c.Thumb = "https://i.ytimg.com/vi/" + v + "/mqdefault.jpg"
		}
		c.SetlistTitle = setlists[p.SetlistID]
		out[i] = c
	}
	return out, nil
}

// bandPage loads the band of a page's path, or answers 404/500.
func (s *Server) bandPage(w http.ResponseWriter, r *http.Request) (*store.Band, bool) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return nil, false
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return nil, false
	}
	return band, true
}

// handleBandProposalsPage is the band's To vote view: its proposals as
// cards — open ones by default, ?status=decided or all — sorted by their
// average stars (?sort=stars, the default), the newest, or the most no
// thanks, each with the viewer's own vote to give or change.
func (s *Server) handleBandProposalsPage(w http.ResponseWriter, r *http.Request) {
	band, ok := s.bandPage(w, r)
	if !ok {
		return
	}
	person := personFromContext(r)
	ctx := r.Context()
	status := r.URL.Query().Get("status")
	if status != "decided" && status != "all" {
		status = "open"
	}
	sortBy := r.URL.Query().Get("sort")
	if sortBy != "newest" && sortBy != "no_thanks" {
		sortBy = "stars"
	}
	filter := store.ProposalFilter{Status: status, Sort: sortBy, Viewer: person.ID}
	if status == "all" {
		filter.Status = ""
	}
	list, err := s.store.ListProposals(ctx, band.ID, filter)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards, err := s.proposalCards(ctx, band.ID, person.ID, list)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	all, err := s.store.ListProposals(ctx, band.ID, store.ProposalFilter{})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	open := 0
	for _, p := range all {
		if p.Status == "open" {
			open++
		}
	}
	members, err := s.store.ListBandMembers(ctx, band.ID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	isCore, _ := s.store.MayVote(ctx, band.ID, person.ID)
	mayDecide, _ := s.store.MayDecideProposal(ctx, band.ID, person.ID)
	setlists, _ := s.store.ListSetlistsForBand(ctx, band.ID)
	s.render(w, "band_proposals", map[string]any{
		"Me": person, "Band": band, "Cards": cards, "Status": status, "Sort": sortBy,
		"OpenCount": open, "DecidedCount": len(all) - open, "MemberCount": len(members),
		"CanDecide": mayDecide || person.IsAdmin, "CanVote": isCore, "Setlists": setlists,
	})
}

// handleBandProposalDetailPage is one proposal's page: its recording to
// play and its video, the viewer's vote, every member's (who rated what,
// who said no thanks, who hasn't voted), the history of the votes, and
// a core member's decision by hand.
func (s *Server) handleBandProposalDetailPage(w http.ResponseWriter, r *http.Request) {
	band, ok := s.bandPage(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	person := personFromContext(r)
	proposal, err := s.store.GetProposal(ctx, proposalID)
	if errors.Is(err, store.ErrNotFound) || (proposal != nil && proposal.BandID != band.ID) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	cards, err := s.proposalCards(ctx, band.ID, person.ID, []store.BandProposal{*proposal})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	history, err := s.store.ProposalVoteLog(ctx, proposalID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if cards[0].Tally.Hidden {
		// The others' votes stay hidden until the viewer votes: only
		// their own past ones (a vote taken back) show.
		mine := history[:0]
		for _, e := range history {
			if e.PersonID == person.ID {
				mine = append(mine, e)
			}
		}
		history = mine
	}
	isCore, _ := s.store.MayVote(ctx, band.ID, person.ID)
	mayDecide, _ := s.store.MayDecideProposal(ctx, band.ID, person.ID)
	audio := ""
	if proposal.Kind == "new_song" && proposal.TargetVariantID != nil {
		if t, ok := s.playlistTrackFor(ctx, 0, *proposal.TargetVariantID, ""); ok {
			audio = t.AudioURL
		}
	}
	s.render(w, "band_proposal", map[string]any{
		"Me": person, "Band": band, "Card": cards[0], "Proposal": proposal, "History": history,
		"CanDecide": mayDecide || person.IsAdmin, "CanVote": isCore, "Audio": audio, "Video": proposal.VideoID(),
	})
}

// proposalTrack is what plays for p in the vote's player: its song's
// recording, else its reference video; a new recording's own video.
func (s *Server) proposalTrack(ctx context.Context, bandID int64, p store.BandProposal, cover string) (playlistTrack, bool) {
	if p.TargetVariantID == nil {
		return playlistTrack{}, false
	}
	t, ok := s.playlistTrackFor(ctx, 0, *p.TargetVariantID, "")
	if !ok {
		return playlistTrack{}, false
	}
	if p.Kind == "new_reference" {
		t.Source, t.Category, t.AudioURL, t.VideoID, t.SourceLabel = "", "", "", "", ""
		if v := store.YouTubeID(p.URL); v != "" {
			t.Source, t.VideoID, t.SourceLabel = "video", v, "Video"
		}
		for i := range t.Sections {
			t.Sections[i].Start = -1
		}
	}
	if v := p.VideoID(); v != "" {
		t.Watch = "https://www.youtube.com/watch?v=" + v
		if cover == "" {
			cover = "https://i.ytimg.com/vi/" + v + "/hqdefault.jpg"
		}
	}
	t.Cover = cover
	if p.TargetVersionName != "" {
		t.Title += " — " + p.TargetVersionName
	}
	t.Href = fmt.Sprintf("/bands/%d/proposals/%d", bandID, p.ID)
	t.ProposalID, t.Note = p.ID, p.Note
	return t, true
}

// handleVoteListen is the player of the songs to vote on: the set lists'
// and songbooks' playlist player (playlist.html), its queue the open
// proposals one after another (in the order they came; ?start= one of
// them first), the stars to give under the song playing (keys 1–4, N
// for no thanks) and where its vote stands beside it.
func (s *Server) handleVoteListen(w http.ResponseWriter, r *http.Request) {
	band, ok := s.bandPage(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	person := personFromContext(r)
	list, err := s.store.ListProposals(ctx, band.ID, store.ProposalFilter{Status: "open"})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	slices.Reverse(list)
	cards, err := s.proposalCards(ctx, band.ID, person.ID, list)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sbID, _ := s.store.GetDefaultSongbook(ctx, "band", band.ID)
	start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
	var tracks, silent []playlistTrack
	tallies := map[string]store.ProposalTally{}
	for _, c := range cards {
		cover := ""
		if c.Thumb != "" && !strings.Contains(c.Thumb, "ytimg.com") {
			cover = c.Thumb
		}
		t, ok := s.proposalTrack(ctx, band.ID, c.BandProposal, cover)
		if !ok {
			continue
		}
		t.Subtitle = strings.Join(nonEmpty(t.Artist, i18n.T(s.langOf(w), "proposed by %s", c.ProposerName)), " · ")
		tallies[strconv.FormatInt(c.ID, 10)] = c.Tally
		if t.Source == "" {
			silent = append(silent, t)
			continue
		}
		if c.ID == start {
			tracks = append([]playlistTrack{t}, tracks...)
		} else {
			tracks = append(tracks, t)
		}
	}
	for i := range tracks {
		tracks[i].Num = strconv.Itoa(i + 1)
	}
	for i := range silent {
		silent[i].Num = strconv.Itoa(len(tracks) + i + 1)
	}
	back := fmt.Sprintf("/bands/%d/catalog/vote", band.ID)
	tracksJSON, _ := json.Marshal(tracks)
	talliesJSON, _ := json.Marshal(tallies)
	isCore, _ := s.store.MayVote(ctx, band.ID, person.ID)
	vote := map[string]any{"BandID": band.ID, "TalliesJSON": template.JS(talliesJSON), "CanVote": isCore}
	s.render(w, "playlist", map[string]any{
		"Me": person, "Songbook": &store.Songbook{ID: sbID, OwnerKind: "band", OwnerBandID: band.ID, Name: band.Name},
		"Owner": band.Name, "Back": back, "Tracks": tracks, "Silent": silent, "TracksJSON": template.JS(tracksJSON),
		"StoreKey": fmt.Sprintf("songbooks:vote:%d:%d", band.ID, start), "Vote": vote, "NavSection": webassets.NavBands,
	})
}

// ---- Proposing a song: the new song-adding flow ---------------------

// proposalLookup is what GET /api/bands/{id}/proposals/lookup answers:
// quick add's lookup (the title and artist of a video or of what was
// typed, the song found), and where that song stands in the band.
type proposalLookup struct {
	quickLookup
	InRepertoire bool `json:"inRepertoire"`
	// OpenProposal: the song is up for a vote already.
	OpenProposal int64 `json:"openProposal,omitempty"`
	// SameReference: the link is the song's reference already.
	SameReference bool `json:"sameReference,omitempty"`
}

func (s *Server) handleProposalLookup(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ctx := r.Context()
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if q == "" {
		httpError(w, http.StatusBadRequest, "missing q")
		return
	}
	out := proposalLookup{quickLookup: quickLookup{Lyrics: ProposalDraft != nil}}
	typed := !isLink(q)
	if typed {
		out.Title, out.Artist = songOfText(q)
	} else {
		if !strings.Contains(q, "://") {
			q = "https://" + q
		}
		out.Reference = q
		out.VideoID = store.YouTubeID(q)
		if out.VideoID != "" {
			out.Reference = "https://www.youtube.com/watch?v=" + out.VideoID
		}
		if RefTitles != nil {
			c, cancel := context.WithTimeout(ctx, 6*time.Second)
			info, err := RefTitles.Info(c, out.Reference)
			cancel()
			if err != nil {
				log.Printf("proposal lookup: %s: %v", out.Reference, err)
			}
			out.Title, out.Artist = songOfVideo(info.Title, info.Author)
		}
	}
	me := personFromContext(r).ID
	owner := store.BandOwner(bandID)
	m, err := s.store.FindSongMatch(ctx, me, owner, out.Title, out.Artist, out.VideoID)
	if err == nil && m == nil && typed && out.Artist != "" {
		if m, err = s.store.FindSongMatch(ctx, me, owner, out.Artist, out.Title, ""); m != nil {
			out.Title, out.Artist = out.Artist, out.Title
		}
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out.Match = m
	if m != nil {
		if sbID, err := s.store.GetDefaultSongbook(ctx, "band", bandID); err == nil {
			_, out.InRepertoire, _ = s.store.SongbookEntryKey(ctx, sbID, m.ID)
		}
		out.SameReference = m.ByVideo
		if open, err := s.store.ListProposals(ctx, bandID, store.ProposalFilter{Status: "open"}); err == nil {
			for _, p := range open {
				if p.TargetVariantID != nil && *p.TargetVariantID == m.ID && (p.Kind == "new_song" || store.YouTubeID(p.URL) == out.VideoID) {
					out.OpenProposal = p.ID
				}
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

// proposalDraft is a song made for a proposal as its check step shows
// and fixes it: its header, its sections (each one's grid as written,
// how many lines of lyrics it has — not the lyrics), where its chart
// came from.
type proposalDraft struct {
	SongID      int64                  `json:"songId"`
	Version     string                 `json:"version"`
	Title       string                 `json:"title"`
	Artist      string                 `json:"artist"`
	VersionName string                 `json:"versionName"`
	Key         string                 `json:"key"`
	Tempo       string                 `json:"tempo"`
	Reference   string                 `json:"reference"`
	VideoID     string                 `json:"videoId,omitempty"`
	Sections    []proposalDraftSection `json:"sections"`
	Source      *proposalDraftSource   `json:"source,omitempty"`
}

type proposalDraftSection struct {
	Label  string `json:"label"`
	Grid   string `json:"grid"`
	Lines  int    `json:"lines"`
	Repeat bool   `json:"repeat,omitempty"`
}

// proposalDraftSource says where a draft's chart came from.
type proposalDraftSource struct {
	Lyrics bool     `json:"lyrics"` // lyrics were found
	Grids  int      `json:"grids"`  // sections with a grid
	UG     string   `json:"ug,omitempty"`
	IReal  string   `json:"ireal,omitempty"`
	Notes  []string `json:"notes,omitempty"`
}

// gridText is a section's grid as the chart writes it ("| Am | % |").
func gridText(ms []song.Measure) string {
	if len(ms) == 0 {
		return ""
	}
	out := song.Write(&song.Song{Sections: []song.Section{{Label: "x", Measures: ms}}})
	for _, line := range strings.Split(out, "\n") {
		if g, ok := strings.CutPrefix(line, "grid: "); ok {
			return g
		}
	}
	return ""
}

// parseGridText is grid's bars, ok false when it has none to read.
func parseGridText(grid string) ([]song.Measure, bool) {
	grid = strings.TrimSpace(grid)
	if grid == "" {
		return nil, true
	}
	if !strings.Contains(grid, "|") {
		grid = "| " + grid + " |"
	}
	sg := song.Parse("[x]\ngrid: " + grid + "\n")
	if len(sg.Sections) == 0 || len(sg.Sections[0].Measures) == 0 {
		return nil, false
	}
	// Each bar holds chords (or a repeat, a rest, no chord), not words.
	for _, m := range sg.Sections[0].Measures {
		for _, tok := range m.Tokens {
			switch c := song.TokenChord(tok); c {
			case "%", ".", "-", "/", "x", "R", "r", "N.C.", "NC", "n.c.":
			default:
				if chordsym.Split(c).Root == "" {
					return nil, false
				}
			}
		}
	}
	return sg.Sections[0].Measures, true
}

func draftOf(id int64, sg *song.Song, version, versionName string) proposalDraft {
	d := proposalDraft{
		SongID: id, Version: version, Title: sg.Get("title"), Artist: sg.Get("artist"), VersionName: versionName,
		Key: sg.Get("key"), Tempo: sg.Get("tempo"), Reference: sg.Get("reference"),
		Sections: []proposalDraftSection{},
	}
	d.VideoID = store.YouTubeID(d.Reference)
	for _, sec := range sg.Sections {
		d.Sections = append(d.Sections, proposalDraftSection{Label: sec.Label, Grid: gridText(sec.Measures), Lines: len(sec.Lyrics), Repeat: sec.Repeat})
	}
	return d
}

var (
	irealUsedRE = regexp.MustCompile(`^iReal Pro grids used in (\d+) sections$`)
	irealKeptRE = regexp.MustCompile(`^(\d+) sections keep Ultimate Guitar's grid`)
)

// draftNote is one of the import's notes in the page's language.
func draftNote(lang, note string) string {
	if m := irealUsedRE.FindStringSubmatch(note); m != nil {
		n, _ := strconv.Atoi(m[1])
		return i18n.N(lang, n, "iReal Pro’s grid used in %d section", "iReal Pro’s grids used in %d sections")
	}
	if m := irealKeptRE.FindStringSubmatch(note); m != nil {
		n, _ := strconv.Atoi(m[1])
		return i18n.N(lang, n, "%d section keeps Ultimate Guitar’s grid: iReal Pro’s differs there", "%d sections keep Ultimate Guitar’s grid: iReal Pro’s differs there")
	}
	switch note {
	case "the lyrics weren't cross-checked: read them through":
		return i18n.T(lang, "The lyrics weren’t cross-checked with another source: read them through.")
	case "no iReal Pro chart found: the grids are Ultimate Guitar's":
		return i18n.T(lang, "No iReal Pro chart found: the grids are Ultimate Guitar’s.")
	}
	return note
}

// proposalSong is the band's song a draft fixes: the band's own, which
// the person may change; the draft's song, else an error answered.
func (s *Server) proposalSong(w http.ResponseWriter, r *http.Request) (int64, bool) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	songID, err := pathID(r, "songID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid song id")
		return 0, false
	}
	rights, err := s.store.SongRightsFor(r.Context(), personFromContext(r).ID, songID)
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, "no such song")
		return 0, false
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return 0, false
	}
	if rights.Owner.Kind != "band" || rights.Owner.ID != bandID || !rights.Edit {
		httpError(w, http.StatusForbidden, "not one of the band's songs")
		return 0, false
	}
	return songID, true
}

// handleCreateProposalDraft makes the song of a proposal, the band's: a
// title, an artist and the video, its chart fetched (lyrics, grids) and
// its recording downloaded behind; it answers the draft to check.
func (s *Server) handleCreateProposalDraft(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	ctx := r.Context()
	me := personFromContext(r).ID
	var req struct {
		Title     string `json:"title"`
		Artist    string `json:"artist"`
		Reference string `json:"reference"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	title, artist, ref := strings.TrimSpace(req.Title), strings.TrimSpace(req.Artist), strings.TrimSpace(req.Reference)
	if title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing title", "field": "title",
			"message": i18n.T(s.langOfRequest(r), "Give the song its title.")})
		return
	}
	id, err := s.store.CreateProposedSong(ctx, me, bandID, title, artist, ref)
	switch {
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "not a member of this band")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	lang := s.langOfRequest(r)
	src := &proposalDraftSource{}
	if ProposalDraft != nil {
		c, cancel := context.WithTimeout(ctx, quickDraftTimeout)
		d, err := ProposalDraft(c, s.store, title, artist)
		cancel()
		if err != nil {
			log.Printf("proposal: chart of %q — %q: %v", title, artist, err)
		} else if d != nil && strings.TrimSpace(d.Text) != "" {
			sg := song.Parse(d.Text)
			sg.Set("title", title)
			if artist != "" {
				sg.Set("artist", artist)
			}
			if ref != "" {
				sg.Set("reference", ref)
			}
			if _, version, err := s.store.LoadVariant(id); err == nil {
				if _, err := s.store.SaveVariant(id, sg, version, me); err != nil {
					log.Printf("proposal: saving the chart of song %d: %v", id, err)
				} else {
					src.UG, src.IReal = d.URL, d.IReal
					for _, n := range d.Notes {
						if m := irealUsedRE.FindStringSubmatch(n); m != nil && m[1] == "0" {
							src.IReal = "" // a chart found, none of its grids used
						}
						src.Notes = append(src.Notes, draftNote(lang, n))
					}
				}
			}
		}
	}
	if ref != "" {
		s.fillRefTitle(id, ref)
		s.fetchProposalAudio(id, ref, me)
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	d := draftOf(id, sg, version, "")
	for _, sec := range d.Sections {
		if sec.Lines > 0 {
			src.Lyrics = true
		}
		if sec.Grid != "" {
			src.Grids++
		}
	}
	d.Source = src
	writeJSON(w, http.StatusOK, d)
}

// fetchProposalAudio downloads the video's sound for the song made for a
// proposal (its MP3 for the vote's player), behind the request.
func (s *Server) fetchProposalAudio(songID int64, ref string, actor int64) {
	if ProposalAudio == nil || store.YouTubeID(ref) == "" {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Minute)
		defer cancel()
		data, err := ProposalAudio(ctx, ref)
		if err != nil {
			log.Printf("proposal: the recording of song %d: %v", songID, err)
			return
		}
		sum := sha256.Sum256(data)
		if _, err := s.store.SetSongAudio(ctx, songID, store.AudioYouTube, data, "audio/mpeg", hex.EncodeToString(sum[:]), ref, actor); err != nil {
			log.Printf("proposal: keeping the recording of song %d: %v", songID, err)
		}
	}()
}

func (s *Server) handleGetProposalDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := s.proposalSong(w, r)
	if !ok {
		return
	}
	sg, version, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	v, err := s.store.GetVariant(r.Context(), id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, draftOf(id, sg, version, v.VersionName))
}

// handlePutProposalDraft saves what the proposer fixed: the title,
// artist, label, key and tempo, and the sections — renamed, reordered,
// added or removed, each one's grid rewritten — the lyrics of each kept
// with it (From: the section it was, -1 for a new one).
func (s *Server) handlePutProposalDraft(w http.ResponseWriter, r *http.Request) {
	id, ok := s.proposalSong(w, r)
	if !ok {
		return
	}
	lang := s.langOfRequest(r)
	var req struct {
		Version     string `json:"version"`
		Title       string `json:"title"`
		Artist      string `json:"artist"`
		VersionName string `json:"versionName"`
		Key         string `json:"key"`
		Tempo       string `json:"tempo"`
		Sections    []struct {
			From  int    `json:"from"`
			Label string `json:"label"`
			Grid  string `json:"grid"`
		} `json:"sections"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	fieldErr := func(field string, index int, msg string) {
		writeJSON(w, http.StatusBadRequest, map[string]any{"error": "invalid " + field, "field": field, "index": index, "message": msg})
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		fieldErr("title", -1, i18n.T(lang, "Give the song its title."))
		return
	}
	if t := strings.TrimSpace(req.Tempo); t != "" {
		if n, err := strconv.Atoi(t); err != nil || n < 20 || n > 400 {
			fieldErr("tempo", -1, i18n.T(lang, "A tempo is a number of beats per minute, 20 to 400."))
			return
		}
	}
	if len(strings.TrimSpace(req.VersionName)) > 80 {
		fieldErr("versionName", -1, i18n.T(lang, "A label is 80 characters at most."))
		return
	}
	sg, _, err := s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	var secs []song.Section
	for i, rs := range req.Sections {
		label := strings.TrimSpace(rs.Label)
		if label == "" {
			fieldErr("label", i, i18n.T(lang, "Name the section (Verse, Chorus…)."))
			return
		}
		var sec song.Section
		if rs.From >= 0 && rs.From < len(sg.Sections) {
			sec = sg.Sections[rs.From]
		}
		ms, ok := parseGridText(rs.Grid)
		if !ok {
			fieldErr("grid", i, i18n.T(lang, "Write the bars between bars lines: | Am | F | C | G |"))
			return
		}
		if gridText(ms) != gridText(sec.Measures) {
			sec.Measures, sec.BarsPerLine = ms, nil
		}
		sec.Label = label
		secs = append(secs, sec)
	}
	if len(secs) == 0 {
		fieldErr("sections", -1, i18n.T(lang, "Keep at least one section."))
		return
	}
	sg.Sections = secs
	sg.Set("title", title)
	setOrUnset := func(k, v string) {
		if v = strings.TrimSpace(v); v != "" {
			sg.Set(k, v)
		} else {
			sg.Unset(k)
		}
	}
	setOrUnset("artist", req.Artist)
	setOrUnset("key", req.Key)
	setOrUnset("tempo", req.Tempo)
	version, err := s.store.SaveVariant(id, sg, req.Version, actorID(r))
	if err != nil {
		writeSaveErr(w, err)
		return
	}
	if err := s.store.SetVersionName(r.Context(), id, req.VersionName); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	sg, version, err = s.store.LoadVariant(id)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, draftOf(id, sg, version, strings.TrimSpace(req.VersionName)))
}

// handleDeleteProposalDraft drops a song made for a proposal that was
// never sent (the flow closed before): the proposer's, in no songbook
// and no proposal.
func (s *Server) handleDeleteProposalDraft(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	id, ok := s.proposalSong(w, r)
	if !ok {
		return
	}
	if err := s.store.DeleteUnsentProposedSong(r.Context(), bandID, id, personFromContext(r).ID); err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleCreateProposal makes a new 'new_song' or 'new_reference'
// proposal, with its note and where it should go. Any current band
// member may propose (requireBandMember).
func (s *Server) handleCreateProposal(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Kind            string          `json:"kind"`
		TargetVariantID int64           `json:"targetVariantId"`
		Payload         json.RawMessage `json:"payload"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r)
	// A band links public songs and its own: another band's private song,
	// or a closed songbook's, isn't proposed (writeLinkErr: the
	// catalogue's version is offered instead when there is one).
	if req.Kind == "new_song" && req.TargetVariantID != 0 {
		if err := s.store.LinkCheckFor(r.Context(), actor.ID, req.TargetVariantID, store.BandOwner(bandID)); err != nil && !errors.Is(err, store.ErrNotFound) {
			s.writeLinkErr(w, r, err)
			return
		}
	}
	id, err := s.store.CreateProposal(r.Context(), bandID, actor.ID, req.Kind, req.TargetVariantID, req.Payload)
	if err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// proposalOfPath is the proposal of the path, in the band of the path.
func (s *Server) proposalOfPath(w http.ResponseWriter, r *http.Request) (*store.BandProposal, bool) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return nil, false
	}
	p, err := s.store.GetProposal(r.Context(), proposalID)
	if err != nil || p.BandID != bandID {
		httpError(w, http.StatusNotFound, "no such proposal in this band")
		return nil, false
	}
	return p, true
}

// handleAcceptProposal / handleRejectProposal decide a proposal by hand:
// any current member of its band (store.decideAndNotify), whatever the
// votes.
func (s *Server) handleAcceptProposal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.proposalOfPath(w, r)
	if !ok {
		return
	}
	if err := s.store.AcceptProposal(r.Context(), p.ID, personFromContext(r).ID); err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"accepted": true, "status": "accepted"})
}

func (s *Server) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	p, ok := s.proposalOfPath(w, r)
	if !ok {
		return
	}
	if err := s.store.RejectProposal(r.Context(), p.ID, personFromContext(r).ID); err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"rejected": true, "status": "rejected"})
}

// voteAnswer is what a vote, or a look at one, answers: the proposal's
// status and where its vote stands as the viewer sees it.
func (s *Server) voteAnswer(w http.ResponseWriter, r *http.Request, proposalID int64) {
	p, err := s.store.GetProposal(r.Context(), proposalID)
	if err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	tally, err := s.store.ProposalTallyFor(r.Context(), p, personFromContext(r).ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": p.ID, "status": p.Status, "tally": tally})
}

// handleCastVote casts, changes or takes back the viewer's vote:
// {"stars": 1–4}, {"noThanks": true} or {"clear": true}.
func (s *Server) handleCastVote(w http.ResponseWriter, r *http.Request) {
	p, ok := s.proposalOfPath(w, r)
	if !ok {
		return
	}
	var v store.Vote
	if err := json.NewDecoder(r.Body).Decode(&v); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if _, err := s.store.CastVote(r.Context(), p.ID, personFromContext(r).ID, v); err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	s.voteAnswer(w, r, p.ID)
}

// handleProposalTally is where a proposal's vote stands (the pages
// follow it live).
func (s *Server) handleProposalTally(w http.ResponseWriter, r *http.Request) {
	p, ok := s.proposalOfPath(w, r)
	if !ok {
		return
	}
	s.voteAnswer(w, r, p.ID)
}

// writeProposalErr answers a proposal's error, with a message in the
// page's language for the ones a person can do something about.
func writeProposalErr(w http.ResponseWriter, r *http.Request, s *Server, err error) {
	lang := s.langOfRequest(r)
	answer := func(code int, msg string) {
		writeJSON(w, code, map[string]string{"error": err.Error(), "message": msg})
	}
	switch {
	case errors.Is(err, store.ErrGuestNoVote):
		answer(http.StatusForbidden, i18n.T(lang, "Your role in this band doesn’t vote."))
	case errors.Is(err, store.ErrNotFound):
		answer(http.StatusNotFound, i18n.T(lang, "This proposal isn’t there any more. Reload the page."))
	case errors.Is(err, store.ErrAlreadyDecided):
		answer(http.StatusConflict, i18n.T(lang, "This one has already been decided. Reload to see how."))
	case errors.Is(err, store.ErrForbidden):
		answer(http.StatusForbidden, i18n.T(lang, "Your role in this band doesn’t allow that."))
	case errors.Is(err, store.ErrInvalid):
		answer(http.StatusBadRequest, i18n.T(lang, "Check what you entered, then try again."))
	default:
		httpError(w, http.StatusInternalServerError, err.Error())
	}
}

// proposalSubtitle is a proposal's line on the home page's "to do":
// who proposed it and where its vote stands.
func proposalSubtitle(lang string, p store.BandProposal) string {
	votes := i18n.T(lang, "no stars yet")
	if p.Hidden {
		votes = i18n.T(lang, "vote to see the others’ votes")
	} else if p.Ratings > 0 {
		votes = i18n.T(lang, "★ %s · %d for, %d against", p.AvgText(), p.Positive, p.Negative)
	}
	out := i18n.T(lang, "Proposed by %s", p.ProposerName) + " · " + votes
	if p.NoThanks > 0 {
		out += " · " + i18n.N(lang, p.NoThanks, "%d no thanks", "%d no thanks")
	}
	return out
}
