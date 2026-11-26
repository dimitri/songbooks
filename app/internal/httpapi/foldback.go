package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"

	"app/internal/i18n"
	"app/internal/store"
)

// Fold back (the edit model, docs/catalogue-edit-review.md §4.7;
// store/foldback.go): an arrangement of The Big Songbook's song that is
// the same as the shared chart, or differs from it by a fix, is offered —
// on its page and on its editors' home page — to go back to the shared
// chart, never automatically:
//
//	POST /api/variants/{id}/fold       {"send": bool}: identical, back to
//	     the link; a fix, sent to the shared song first (an edit by the
//	     person, weighed like any: applied, then folded; a suggestion, the
//	     arrangement stays until a reviewer accepts it)
//	POST /api/variants/{id}/fold-keep  "It's our arrangement": not offered again

// foldOfferFor is arrangement songID's offer for personID, who may change
// it; nil when there's none.
func (s *Server) foldOfferFor(r *http.Request, personID, songID int64) *store.FoldCandidate {
	rights, err := s.store.SongRightsFor(r.Context(), personID, songID)
	if err != nil || !rights.Edit {
		return nil
	}
	cs, err := s.store.FoldCandidates(r.Context(), songID)
	if err != nil || len(cs) != 1 || !cs[0].Foldable() {
		return nil
	}
	return &cs[0]
}

func (s *Server) handleFoldBack(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Send bool `json:"send"`
	}
	if r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
			return
		}
	}
	person := personFromContext(r)
	lang := s.langOfRequest(r)
	offer := s.foldOfferFor(r, person.ID, id)
	if offer == nil {
		httpError(w, http.StatusForbidden, i18n.T(lang, "This arrangement isn’t one to fold back."))
		return
	}
	if offer.Kind == store.FoldFix {
		if !req.Send {
			httpError(w, http.StatusBadRequest, i18n.T(lang, "This arrangement differs from the shared chart: send its fix first."))
			return
		}
		// The fix, as the person's edit of the shared song.
		origID, body, ok, err := s.store.FoldFixBody(r.Context(), id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if !ok {
			httpError(w, http.StatusConflict, i18n.T(lang, "The shared chart changed in the same place since: compare them and fix it there by hand."))
			return
		}
		cur, err := s.store.SongChartOf(r.Context(), origID)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		r2 := r.Clone(r.Context())
		r2.Header.Set(editSummaryHeader, url.QueryEscape(i18n.T(lang, "Fix from %s’s arrangement", offer.OwnerName)))
		r2.Header.Set(structureConfirmedHeader, "1")
		r2.Header.Set(editKindHeader, editKindFix)
		rec := httptest.NewRecorder()
		if !s.catalogueSave(rec, r2, origID, cur.Title, cur.Artist, cur.ReferenceURL, body, strconv.Itoa(cur.Version)) {
			httpError(w, http.StatusForbidden, i18n.T(lang, "This arrangement isn’t one to fold back."))
			return
		}
		var out map[string]any
		_ = json.Unmarshal(rec.Body.Bytes(), &out)
		if rec.Code != http.StatusOK || out["suggested"] == true {
			if rec.Code == http.StatusAccepted || out["suggested"] == true {
				writeJSON(w, http.StatusAccepted, map[string]any{
					"pending": true, "url": out["url"],
					"message": i18n.T(lang, "Your fix waits for a reviewer: the arrangement stays until it’s accepted, then use the shared chart."),
				})
				return
			}
			for k, v := range rec.Header() {
				w.Header()[k] = v
			}
			w.WriteHeader(rec.Code)
			_, _ = w.Write(rec.Body.Bytes())
			return
		}
	}
	rep, err := s.store.FoldBack(r.Context(), person.ID, id, false, store.FoldIdentical, store.FoldFix)
	if errors.Is(err, store.ErrNotFoldable) {
		httpError(w, http.StatusConflict, i18n.T(lang, "This arrangement isn’t one to fold back."))
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	q := r.URL.Query()
	link := fmt.Sprintf("/song/%d", rep.Candidate.OriginalID)
	if len(q) > 0 {
		link += "?" + q.Encode()
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"folded": true, "variantId": rep.Candidate.OriginalID, "url": link,
		"message": i18n.T(lang, "Back to the shared chart: every songbook and set list of the arrangement opens it, in its key."),
	})
}

func (s *Server) handleKeepArrangement(w http.ResponseWriter, r *http.Request) {
	id, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	person := personFromContext(r)
	rights, err := s.store.SongRightsFor(r.Context(), person.ID, id)
	if err != nil || !rights.Edit {
		httpError(w, http.StatusForbidden, "forbidden")
		return
	}
	if err := s.store.KeepArrangement(r.Context(), person.ID, id); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"kept": true})
}

// homeFoldOffers is the arrangements the person may change that are only
// fixes, for their home page (nil for none).
func (s *Server) homeFoldOffers(r *http.Request, personID int64) []store.FoldCandidate {
	cs, err := s.store.FoldOffersFor(r.Context(), personID)
	if err != nil {
		return nil
	}
	return cs
}
