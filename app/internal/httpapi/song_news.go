package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/songdiff"
	"app/internal/store"
)

// "Changes in your songs" on the home page (store/song_news.go): the
// latest accepted change to each song the person has — its one-line
// summary (or what it changed, when it has none), who, when, a link to
// it in the song's history and, for the original of their arrangement,
// the offer to take its changes. No notification is sent for these; the
// person marks the list seen.

// songNewsShown is how many songs the list shows, the rest behind "more";
// songNewsMax how many it holds at most.
const (
	songNewsShown = 4
	songNewsMax   = 16
)

// songNewsRow is one song's latest change, as the home page shows it.
type songNewsRow struct {
	Title, Artist, Label string
	Link                 string // the song
	DiffLink             string // the change, in its history
	TakeLink             string // the arrangement's offer to take its changes; "" for none
	What                 string
	Quoted               bool // What is the author's summary
	By, When             string
	More                 int    // other changes since
	Via                  string // where the person has it, "" for their songbook
}

// songNewsView is the home page's list: Rows shown, More behind a
// "more"; UpTo the newest change, what "Mark as seen" marks.
type songNewsView struct {
	Rows, More []songNewsRow
	UpTo       string
}

// songNews is personID's list, nil when there's nothing new.
func (s *Server) songNews(ctx context.Context, lang string, personID int64) (*songNewsView, error) {
	news, err := s.store.SongNewsFor(ctx, personID, songNewsMax)
	if err != nil || len(news) == 0 {
		return nil, err
	}
	v := &songNewsView{UpTo: news[0].At.UTC().Format(time.RFC3339Nano)}
	for i, n := range news {
		row := songNewsRow{
			Title: n.Title, Artist: n.Artist,
			Link:     fmt.Sprintf("/song/%d", n.SongID),
			DiffLink: fmt.Sprintf("/variants/%d/history#rev-%d", n.SongID, n.RevisionID),
			By:       n.ActorName, When: timeAgo(lang, n.At), More: n.Changes - 1,
		}
		if n.Catalogue {
			row.Label = n.VersionName
		}
		if n.Summary != "" {
			row.What, row.Quoted = n.Summary, true
		} else {
			row.What = describeSongChange(lang, n.Before, n.After)
		}
		switch n.Via {
		case "band":
			row.Via = n.BandName
		case "arrangement":
			row.Via = i18n.T(lang, "the original of your arrangement")
		}
		if n.ArrangementID != 0 {
			row.TakeLink = store.VersionChangedLink(n.ArrangementID)
		}
		if i < songNewsShown {
			v.Rows = append(v.Rows, row)
		} else {
			v.More = append(v.More, row)
		}
	}
	return v, nil
}

// describeSongChange says what a change without a summary did, from the
// song before and after it: a new reference, its label, its title or
// artist, else the first of its chart's changes (songdiff.Describe).
func describeSongChange(lang string, before, after store.VariantSnapshot) string {
	switch {
	case strings.TrimSpace(before.ReferenceURL) != strings.TrimSpace(after.ReferenceURL):
		if strings.TrimSpace(after.ReferenceURL) == "" {
			return i18n.T(lang, "Reference recording removed")
		}
		return i18n.T(lang, "New reference recording")
	case before.VersionName != nil && after.VersionName != nil && *before.VersionName != *after.VersionName:
		return i18n.T(lang, "Reference label: %s", *after.VersionName)
	case before.Title != after.Title || before.Artist != after.Artist:
		return i18n.T(lang, "Title or artist changed")
	}
	changes := songdiff.Describe(before.Body, after.Body)
	if len(changes) == 0 {
		return i18n.T(lang, "Edited")
	}
	what := changes[0].Where + ": " + changes[0].What
	if len(changes) > 1 {
		what += " " + i18n.T(lang, "(and %d more)", len(changes)-1)
	}
	return what
}

// handleSeeSongNews is POST /api/song-news/seen, {"upTo"}: the person's
// "Changes in your songs" marked seen up to the newest change the page
// showed.
func (s *Server) handleSeeSongNews(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UpTo string `json:"upTo"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	upTo, err := time.Parse(time.RFC3339Nano, req.UpTo)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid upTo")
		return
	}
	if err := s.store.SeeSongNews(r.Context(), personFromContext(r).ID, upTo); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"seen": true})
}
