package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/song"
	"app/internal/songimport"
	"app/internal/store"
)

// Finding and adding songs (docs/ownership-model.md §1, §3, §6, §7;
// steps 4–5):
//
//	GET  /api/find?q=&scope=&songbook=      one search over everything the person sees, by work,
//	                                        with whose each song is and who sees it; for a songbook,
//	                                        whether each may be linked there (the Add / Create dialog)
//	GET  /api/songbook-targets?song=&keep=  the songbooks the person adds to (Create's "Goes into",
//	                                        the Add to… picker), for a song whether each may hold it
//	GET  /big-songbook?from=&q=             The Big Songbook: every public song by work
//	GET  /big-songbook/duplicates           its staff's duplicates review
//	POST /api/big-songbook/merge            {keep, drop, dryRun}: merge one into the other

// ownerBadge is a song's "whose · who sees it" line, as the catalogue,
// the search palette and the Add dialog show it: "Yours · private",
// "Mercy · private", "Other Lies · original · public", "The Big Songbook
// · public", "Atelier Blues · closed".
func ownerBadge(lang string, f store.FoundSong) string {
	var parts []string
	switch {
	case f.Mine:
		parts = append(parts, i18n.T(lang, "Yours"))
	case f.Catalogue:
		parts = append(parts, i18n.T(lang, "The Big Songbook"))
	default:
		parts = append(parts, f.OwnerName)
	}
	if f.Original {
		parts = append(parts, i18n.T(lang, "badge|original"))
	}
	switch {
	case f.Closed:
		parts = append(parts, i18n.T(lang, "badge|closed"))
	case f.Public:
		parts = append(parts, i18n.T(lang, "badge|public"))
	default:
		parts = append(parts, i18n.T(lang, "badge|private"))
	}
	return strings.Join(parts, " · ")
}

// findScopes are the catalogue's filters, in order.
var findScopes = []struct{ Scope, Label string }{
	{store.FindAll, "find|All"}, {store.FindMine, "find|Mine"}, {store.FindBands, "My bands"},
	{store.FindCommunities, "My communities"}, {store.FindPublic, "find|Public"},
}

func validFindScope(s string) string {
	for _, f := range findScopes {
		if f.Scope == s {
			return s
		}
	}
	return store.FindAll
}

// foundSongJSON is a FoundSong for the dialog: its badge in the page's
// language, and why it can't be linked, said.
type foundSongJSON struct {
	store.FoundSong
	Badge      string `json:"badge"`
	RefusalMsg string `json:"refusalMsg,omitempty"`
}

type foundWorkJSON struct {
	WorkID int64           `json:"workId"`
	Title  string          `json:"title"`
	Artist string          `json:"artist"`
	Songs  []foundSongJSON `json:"songs"`
}

// refusalText says why a songbook can't hold a song.
func refusalText(lang, reason string) string {
	switch reason {
	case "closed":
		return i18n.T(lang, "In a closed songbook: it can’t go anywhere else.")
	case "private":
		return i18n.T(lang, "Private to its owner: only a person’s own songbook may link it.")
	}
	return ""
}

func foundJSON(lang string, works []store.FoundWork) []foundWorkJSON {
	out := make([]foundWorkJSON, 0, len(works))
	for _, w := range works {
		fw := foundWorkJSON{WorkID: w.WorkID, Title: w.Title, Artist: w.Artist}
		for _, s := range w.Songs {
			fw.Songs = append(fw.Songs, foundSongJSON{FoundSong: s, Badge: ownerBadge(lang, s), RefusalMsg: refusalText(lang, s.Refusal)})
		}
		out = append(out, fw)
	}
	return out
}

// handleFindAPI is GET /api/find: what the person sees matching q, by
// work (at most 30), in scope ("mine", "bands", "communities", "public";
// "" all). With songbook=N (one the person adds to), each song says
// whether it's in it and whether it may be linked there; a closed
// songbook's song is never offered elsewhere (§2).
func (s *Server) handleFindAPI(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 100 {
		q = q[:100]
	}
	f := store.FindFilter{Query: q, Scope: validFindScope(r.URL.Query().Get("scope")), Limit: 30}
	if sbID, _ := strconv.ParseInt(r.URL.Query().Get("songbook"), 10, 64); sbID > 0 {
		rights, err := s.store.SongbookRightsFor(r.Context(), person.ID, sbID)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such songbook")
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !rights.Edit {
			httpError(w, http.StatusForbidden, "you can't add songs to this songbook")
			return
		}
		f.Songbook = rights.Songbook
	}
	if q == "" && f.Scope == store.FindAll && f.Songbook == nil {
		writeJSON(w, http.StatusOK, map[string]any{"works": []foundWorkJSON{}})
		return
	}
	works, err := s.store.FindSongs(r.Context(), person.ID, f)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// A closed songbook's song is never offered anywhere else (§2): it
	// isn't listed at all.
	if f.Songbook != nil {
		var keep []store.FoundWork
		for _, wk := range works {
			var songs []store.FoundSong
			for _, sg := range wk.Songs {
				if sg.Refusal != "closed" {
					songs = append(songs, sg)
				}
			}
			if len(songs) > 0 {
				wk.Songs = songs
				keep = append(keep, wk)
			}
		}
		works = keep
	}
	// A songbook's dialog with nothing typed: its own songs aren't news;
	// the others the person may add, first.
	if q == "" && f.Songbook != nil {
		var keep []store.FoundWork
		for _, wk := range works {
			var songs []store.FoundSong
			for _, sg := range wk.Songs {
				if !sg.InSongbook && sg.Linkable {
					songs = append(songs, sg)
				}
			}
			if len(songs) > 0 {
				wk.Songs = songs
				keep = append(keep, wk)
			}
		}
		works = keep
	}
	writeJSON(w, http.StatusOK, map[string]any{"works": foundJSON(lang, works)})
}

// songbookTargetJSON is a SongbookTarget with its label said.
type songbookTargetJSON struct {
	store.SongbookTarget
	Label      string `json:"label"`
	Big        bool   `json:"big,omitempty"`
	RefusalMsg string `json:"refusalMsg,omitempty"`
}

// targetLabel names a songbook for a picker: "My songbook", "Mercy —
// Songbook", "Atelier Blues — Méthode Blues, volume 1".
func targetLabel(lang string, t store.SongbookTarget) string {
	if t.Mine {
		if t.IsDefault {
			return i18n.T(lang, "My songbook")
		}
		return t.Name
	}
	if t.IsDefault {
		return t.OwnerName
	}
	return t.OwnerName + " — " + t.Name
}

// handleSongbookTargetsAPI is GET /api/songbook-targets: the songbooks
// the person adds to (keep=N: also that student's songbook, the one the
// dialog was opened from). With song=N, each says whether it holds the
// song and whether it may; without, a site admin also gets The Big
// Songbook (a new song there is the catalogue's, public).
func (s *Server) handleSongbookTargetsAPI(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	keep, _ := strconv.ParseInt(r.URL.Query().Get("keep"), 10, 64)
	songID, _ := strconv.ParseInt(r.URL.Query().Get("song"), 10, 64)
	var targets []store.SongbookTarget
	var err error
	if songID > 0 {
		targets, err = s.store.SongbookTargetsFor(r.Context(), person.ID, songID, keep)
	} else {
		targets, err = s.store.SongbookTargets(r.Context(), person.ID, keep)
	}
	switch {
	case errors.Is(err, store.ErrForbidden), errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, "no such song")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := make([]songbookTargetJSON, 0, len(targets)+1)
	for _, t := range targets {
		out = append(out, songbookTargetJSON{SongbookTarget: t, Label: targetLabel(lang, t), RefusalMsg: refusalText(lang, t.Refusal)})
	}
	if songID == 0 && person.IsAdmin {
		out = append(out, songbookTargetJSON{
			SongbookTarget: store.SongbookTarget{Name: "The Big Songbook", OwnerKind: "community", CanPublish: true, Linkable: true},
			Label:          i18n.T(lang, "The Big Songbook"), Big: true,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"targets": out})
}

// fitView is a song's "follows the recording" badge (§3, flags only).
type fitView struct {
	Fit   string // "follows" or "differs"
	Label string
	Why   string // the reasons it differs, said
}

// fitBudget is how long one page spends reading verdicts not kept yet;
// the rest are read on the next visit.
const fitBudget = 1500 * time.Millisecond

// recordingFits is the "follows the recording" verdict of each of ids
// whose reference recording has an analysis: kept ones, and the ones not
// kept yet read (songimport.RecordingFit) and kept, within fitBudget.
func (s *Server) recordingFits(ctx context.Context, lang string, ids []int64) map[int64]fitView {
	out := map[int64]fitView{}
	kept, stale, err := s.store.RecordingFits(ctx, ids)
	if err != nil {
		return out
	}
	deadline := time.Now().Add(fitBudget)
	for _, id := range stale {
		if time.Now().After(deadline) {
			break
		}
		in, err := s.store.FitInputOf(ctx, id)
		if err != nil || in == nil {
			continue
		}
		f := songimport.RecordingFit(in.Analysis, in.Body, SyncSections(song.Parse(in.Body)))
		if err := s.store.SaveRecordingFit(ctx, in, f.Fit, f.Reasons); err != nil {
			continue
		}
		raw, _ := json.Marshal(f.Reasons)
		kept[id] = store.RecordingFit{Fit: f.Fit, Detail: raw}
	}
	for id, k := range kept {
		switch k.Fit {
		case songimport.FitFollows:
			out[id] = fitView{Fit: k.Fit, Label: i18n.T(lang, "follows the recording")}
		case songimport.FitDiffers:
			var reasons []songimport.FitReason
			_ = json.Unmarshal(k.Detail, &reasons)
			out[id] = fitView{Fit: k.Fit, Label: i18n.T(lang, "differs from the recording"), Why: fitWhy(lang, reasons)}
		}
	}
	return out
}

// fitWhy says how a chart differs from its recording.
func fitWhy(lang string, reasons []songimport.FitReason) string {
	var parts []string
	for _, w := range reasons {
		switch w.Kind {
		case "intro":
			parts = append(parts, i18n.N(lang, w.Bars, "the recording plays %d bar before the chart’s first section", "the recording plays %d bars before the chart’s first section"))
		case "section":
			parts = append(parts, i18n.T(lang, "%s: %d bars written, %d played", w.Label, w.Written, w.Played))
		case "bars":
			parts = append(parts, i18n.T(lang, "%d bars written in all, about %d played", w.Written, w.Played))
		case "unplaced":
			parts = append(parts, i18n.T(lang, "many of the chart’s bars aren’t heard in the recording"))
		}
	}
	return strings.Join(parts, "; ")
}

// bigSongbookFroms are The Big Songbook page's filters, in order.
var bigSongbookFroms = []struct{ From, Label string }{
	{"", "find|All"}, {store.FromCatalogue, "Catalogue"}, {store.FromBands, "From bands"}, {store.FromCommunities, "From communities"},
}

// bigWorkView is one work on The Big Songbook's page.
type bigWorkView struct {
	store.FoundWork
	Badges map[int64]string
	Fits   map[int64]fitView
}

// handleBigSongbookPage is The Big Songbook's page (§6): every public
// song, by work with its versions; from the catalogue, from bands, from
// communities; each with "Add to…" and whether it follows its recording.
// Its staff (the site's admins) see how many duplicates wait for review.
func (s *Server) handleBigSongbookPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOf(w)
	from := r.URL.Query().Get("from")
	switch from {
	case store.FromCatalogue, store.FromBands, store.FromCommunities:
	default:
		from = ""
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	works, err := s.store.FindSongs(r.Context(), person.ID, store.FindFilter{OnlyPublic: true, From: from, Query: q})
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	var ids []int64
	songs := 0
	for _, wk := range works {
		for _, sg := range wk.Songs {
			ids = append(ids, sg.ID)
			songs++
		}
	}
	fits := s.recordingFits(r.Context(), lang, ids)
	views := make([]bigWorkView, len(works))
	for i, wk := range works {
		v := bigWorkView{FoundWork: wk, Badges: map[int64]string{}, Fits: map[int64]fitView{}}
		for _, sg := range wk.Songs {
			v.Badges[sg.ID] = ownerBadge(lang, sg)
			if f, ok := fits[sg.ID]; ok {
				v.Fits[sg.ID] = f
			}
		}
		views[i] = v
	}
	dups := 0
	if person.IsAdmin {
		groups, err := s.store.FindDuplicates(r.Context())
		if err == nil {
			dups = len(groups)
		}
	}
	type fromView struct {
		From, Label, Href string
		Active            bool
	}
	var froms []fromView
	for _, f := range bigSongbookFroms {
		v := url.Values{}
		if f.From != "" {
			v.Set("from", f.From)
		}
		if q != "" {
			v.Set("q", q)
		}
		href := "/big-songbook"
		if len(v) > 0 {
			href += "?" + v.Encode()
		}
		froms = append(froms, fromView{From: f.From, Label: f.Label, Href: href, Active: f.From == from})
	}
	s.render(w, "big_songbook", map[string]any{
		"Me": person, "Works": views, "Songs": songs, "From": from, "Froms": froms, "Q": q,
		"IsStaff": person.IsAdmin, "Duplicates": dups,
	})
}

// handleBigSongbookDuplicatesPage is the duplicates review (§7), for The
// Big Songbook's staff: each group of public songs with the same title
// and artist or the same reference video, what points at each, and a
// merge with its dry run first.
func (s *Server) handleBigSongbookDuplicatesPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	groups, err := s.store.FindDuplicates(r.Context())
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "big_songbook_duplicates", map[string]any{"Me": person, "Groups": groups})
}

// mergeReportJSON is a merge's report, said.
type mergeReportJSON struct {
	DryRun  bool     `json:"dryRun"`
	Keep    int64    `json:"keep"`
	Drop    int64    `json:"drop"`
	Summary string   `json:"summary"`
	Lines   []string `json:"lines"`
}

// handleMergeSongsAPI is POST /api/big-songbook/merge {keep, drop,
// dryRun}: drop merged into keep (store.MergeSongs), or what that would
// do. The site's admins only (the route's check, and the store's).
func (s *Server) handleMergeSongsAPI(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	var req struct {
		Keep   int64 `json:"keep"`
		Drop   int64 `json:"drop"`
		DryRun bool  `json:"dryRun"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.Keep == 0 || req.Drop == 0 {
		httpError(w, http.StatusBadRequest, "keep and drop are needed")
		return
	}
	rep, err := s.store.MergeSongs(r.Context(), person.ID, req.Keep, req.Drop, req.DryRun)
	switch {
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, err.Error())
		return
	case errors.Is(err, store.ErrNotDuplicates):
		httpError(w, http.StatusConflict, i18n.T(lang, "These songs aren’t duplicates: both must be public, with the same title and artist or the same reference video."))
		return
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	out := mergeReportJSON{DryRun: rep.DryRun, Keep: rep.Keep.ID, Drop: rep.Drop.ID}
	if rep.DryRun {
		out.Summary = i18n.T(lang, "Merging song %d into song %d would:", rep.Drop.ID, rep.Keep.ID)
	} else {
		out.Summary = i18n.T(lang, "Song %d is merged into song %d:", rep.Drop.ID, rep.Keep.ID)
	}
	m := rep.Moved
	out.Lines = []string{
		i18n.N(lang, m.Entries, "move %d songbook entry (its key kept)", "move %d songbook entries (their keys kept)"),
		i18n.N(lang, m.SetlistItems, "move %d set list line", "move %d set list lines"),
		i18n.N(lang, m.Recordings, "move %d band recording", "move %d band recordings"),
		i18n.N(lang, m.Media, "move %d recording or Guitar Pro file the kept song lacks", "move %d recordings or Guitar Pro files the kept song lacks"),
		i18n.N(lang, m.Other, "move %d other reference (notes, practice, visits, versions made from it…)", "move %d other references (notes, practice, visits, versions made from it…)"),
	}
	if rep.MediaDropped > 0 {
		out.Lines = append(out.Lines, i18n.N(lang, rep.MediaDropped,
			"drop %d recording or Guitar Pro file of a kind the kept song has its own of",
			"drop %d recordings or Guitar Pro files of a kind the kept song has its own of"))
	}
	out.Lines = append(out.Lines, i18n.T(lang, "keep song %d’s history on song %d’s, and send its old links there", rep.Drop.ID, rep.Keep.ID))
	writeJSON(w, http.StatusOK, out)
}

// mergedRedirect sends a request for a merged song (§7) to the song it
// went into, the rest of its path kept; false when id wasn't merged.
func (s *Server) mergedRedirect(w http.ResponseWriter, r *http.Request, id int64) bool {
	if r.Method != http.MethodGet {
		return false
	}
	to, err := s.store.MergedSongTarget(r.Context(), id)
	if err != nil || to == 0 {
		return false
	}
	u := *r.URL
	u.Path = strings.Replace(u.Path, fmt.Sprintf("/%d", id), fmt.Sprintf("/%d", to), 1)
	http.Redirect(w, r, u.String(), http.StatusMovedPermanently)
	return true
}
