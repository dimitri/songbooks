package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"app/internal/i18n"
	"app/internal/store"
	"app/internal/webassets"
)

// A band's pools of songs to listen to (docs/band-votes.md;
// store/band_listens.go), views of its Songbook beside To vote:
// Discover — songs a member shares for the band to hear, no vote, each
// with who shared it and why, "Propose for a vote" turning one into a
// proposal — and Influences — the artists and songs that shape the
// band's sound, with what in them, tagged. The same add-by-link flow (a
// YouTube link, its title and artist read from the video) and the same
// player (the playlist player, the videos big beside the queue) as the
// rest. Any current member adds; who added a song removes it.

// listenPools are the pools by their path (and their Songbook view).
var listenPools = map[string]string{"discover": store.PoolDiscover, "influences": store.PoolInfluence}

// listenCard is a pool's song as its page shows it.
type listenCard struct {
	store.BandListen
	Mine      bool
	CanRemove bool
}

// handleBandListensPage is a pool's page: GET /bands/{id}/catalog/discover
// or /catalog/influences.
func (s *Server) handleBandListensPage(pathPool string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		band, ok := s.bandPage(w, r)
		if !ok {
			return
		}
		person := personFromContext(r)
		pool := listenPools[pathPool]
		list, err := s.store.ListBandListens(r.Context(), band.ID, pool)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		cards := make([]listenCard, len(list))
		for i, l := range list {
			cards[i] = listenCard{BandListen: l, Mine: l.AddedBy == person.ID}
		}
		page := "band_discover"
		if pool == store.PoolInfluence {
			page = "band_influences"
		}
		ctx := r.Context()
		canVote, _ := s.store.MayCastVote(ctx, band.ID, person.ID)
		privs, _ := s.bandPrivs(r, band.ID)
		curate := privs.Can(store.BandPrivCurateListen)
		for i := range cards {
			// Its author removes it, or whoever curates the band's pools
			// (curate_listen); never in an archived band (read only).
			cards[i].CanRemove = !band.Archived() && (cards[i].Mine || curate)
		}
		s.render(w, page, map[string]any{
			"Me": person, "Band": band, "Cards": cards, "Pool": pool, "PathPool": pathPool, "CanVote": canVote,
			"CanAdd": privs.Can(store.BandPrivPropose),
		})
	}
}

// handleBandListensPlay is a pool's songs played one after another, the
// videos big beside the queue: the playlist player (playlist.html).
// ?start= plays one first.
func (s *Server) handleBandListensPlay(pathPool string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		band, ok := s.bandPage(w, r)
		if !ok {
			return
		}
		ctx := r.Context()
		lang := s.langOf(w)
		pool := listenPools[pathPool]
		list, err := s.store.ListBandListens(ctx, band.ID, pool)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		start, _ := strconv.ParseInt(r.URL.Query().Get("start"), 10, 64)
		var tracks []playlistTrack
		for _, l := range list {
			t := playlistTrack{
				Title: orDefault(l.Title, l.URL), Artist: l.Artist, Href: l.URL, Watch: l.URL, Note: l.Note,
				Source: "video", VideoID: l.VideoID, SourceLabel: "Video", Sections: []playlistSection{},
				Subtitle: strings.Join(nonEmpty(l.Artist, i18n.T(lang, "shared by %s", l.AddedByName)), " · "),
			}
			if l.ID == start {
				tracks = append([]playlistTrack{t}, tracks...)
			} else {
				tracks = append(tracks, t)
			}
		}
		for i := range tracks {
			tracks[i].Num = strconv.Itoa(i + 1)
		}
		sbID, _ := s.store.GetDefaultSongbook(ctx, "band", band.ID)
		tracksJSON, _ := json.Marshal(tracks)
		label := "Discover"
		if pool == store.PoolInfluence {
			label = "Influences"
		}
		s.render(w, "playlist", map[string]any{
			"Me": personFromContext(r), "Songbook": &store.Songbook{ID: sbID, OwnerKind: "band", OwnerBandID: band.ID, Name: band.Name},
			"Owner": band.Name, "Back": fmt.Sprintf("/bands/%d/catalog/%s", band.ID, pathPool),
			"Tracks": tracks, "TracksJSON": template.JS(tracksJSON), "VideoFirst": true, "Listens": label,
			"StoreKey": fmt.Sprintf("songbooks:%s:%d:%d", pathPool, band.ID, start), "NavSection": webassets.NavBands,
		})
	}
}

// handleListenLookup is the add form's look at a pasted link: the song's
// title and artist from the video (oEmbed), to fix before adding.
func (s *Server) handleListenLookup(w http.ResponseWriter, r *http.Request) {
	q := strings.TrimSpace(r.URL.Query().Get("url"))
	vid := store.YouTubeID(q)
	if vid == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a YouTube video", "field": "url",
			"message": i18n.T(s.langOfRequest(r), "Paste a YouTube video’s link (youtube.com/watch?v=… or youtu.be/…).")})
		return
	}
	out := map[string]string{"videoId": vid, "url": "https://www.youtube.com/watch?v=" + vid}
	if RefTitles != nil {
		c, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		info, err := RefTitles.Info(c, out["url"])
		cancel()
		if err != nil {
			log.Printf("listen lookup: %s: %v", out["url"], err)
		}
		out["title"], out["artist"] = songOfVideo(info.Title, info.Author)
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAddListen adds a song to one of the band's pools:
// {pool, url, title, artist, note, tags}. Any current member.
func (s *Server) handleAddListen(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req struct {
		Pool   string   `json:"pool"`
		URL    string   `json:"url"`
		Title  string   `json:"title"`
		Artist string   `json:"artist"`
		Note   string   `json:"note"`
		Tags   []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	lang := s.langOfRequest(r)
	if store.YouTubeID(req.URL) == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "not a YouTube video", "field": "url",
			"message": i18n.T(lang, "Paste a YouTube video’s link (youtube.com/watch?v=… or youtu.be/…).")})
		return
	}
	id, err := s.store.AddBandListen(r.Context(), bandID, personFromContext(r).ID, store.NewBandListen{
		Pool: req.Pool, URL: req.URL, Title: req.Title, Artist: req.Artist, Note: req.Note, Tags: req.Tags,
	})
	switch {
	case errors.Is(err, store.ErrInvalid):
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": err.Error(), "message": i18n.T(lang, "Check what you entered, then try again.")})
		return
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusForbidden, "not a member of this band")
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleDeleteListen removes a song from the band's pools: who added it,
// or anyone's for a member whose role grants curate_listen.
func (s *Server) handleDeleteListen(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	listenID, err := pathID(r, "listenID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	curate, err := s.bandPrivilege(r, bandID, store.BandPrivCurateListen)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	err = s.store.DeleteBandListen(r.Context(), bandID, listenID, personFromContext(r).ID, curate)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusNotFound, err.Error())
	case errors.Is(err, store.ErrForbidden):
		writeJSON(w, http.StatusForbidden, map[string]string{"error": err.Error(),
			"message": i18n.T(s.langOfRequest(r), "Only who shared it, or a member whose role lets them remove anyone’s, removes it.")})
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
	default:
		w.WriteHeader(http.StatusNoContent)
	}
}

// handleListenProposed says a shared song was proposed for a vote since:
// {proposalId}.
func (s *Server) handleListenProposed(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	listenID, err := pathID(r, "listenID")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		ProposalID int64 `json:"proposalId"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	if err := s.store.SetBandListenProposal(r.Context(), bandID, listenID, req.ProposalID); err != nil {
		writeProposalErr(w, r, s, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
