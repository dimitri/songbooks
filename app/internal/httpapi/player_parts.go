package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"

	"app/internal/instruments"
	"app/internal/store"
)

// "Je joue" (store/player_parts.go): the song player shows one level per
// part the reader plays here, from the recording's parts. The song page
// carries their parts and where they came from (playerPartsData); the
// player's selector saves them for this song or for this context
// (handlePutPlayerParts), for the reader only.

// partsContext is the context a song page was opened in, for the reader's
// parts: the band or community of the event, songbook, set list or page
// it came from; none ("elsewhere") from nowhere in particular.
func (s *Server) partsContext(r *http.Request, data map[string]any) store.PartsContext {
	if id, ok := data["EventRef"].(int64); ok {
		if ev, err := s.store.GetEvent(r.Context(), id); err == nil {
			switch {
			case ev.OwnerKind == "community" && ev.OwnerCommunityID != 0:
				return store.PartsContext{Kind: "community", ID: ev.OwnerCommunityID}
			case ev.OwnerKind == "band" && ev.OwnerBandID != 0:
				return store.PartsContext{Kind: "band", ID: ev.OwnerBandID}
			}
		}
	}
	if kind, id := s.songBrandContext(r, data, nil); id != 0 && (kind == "band" || kind == "community") {
		return store.PartsContext{Kind: kind, ID: id}
	}
	return store.PartsContext{}
}

// partsContextName is a context's name, for "dans ce contexte (Mercy)".
func (s *Server) partsContextName(r *http.Request, c store.PartsContext) string {
	switch c.Kind {
	case "band":
		if b, err := s.store.GetBand(r.Context(), c.ID); err == nil {
			return b.Name
		}
	case "community":
		if cm, err := s.store.GetCommunity(r.Context(), c.ID); err == nil {
			return cm.Name
		}
	}
	return ""
}

type partsInstrument struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Stem  string `json:"stem"`
}

type playerPartsJSON struct {
	Parts   []string          `json:"parts"`
	From    string            `json:"from"` // store.PartsFrom*
	Context partsContextJSON  `json:"context"`
	All     []partsInstrument `json:"all,omitempty"` // the list to pick from
}

type partsContextJSON struct {
	Kind string `json:"kind"`
	ID   int64  `json:"id,omitempty"`
	Name string `json:"name,omitempty"`
}

func (s *Server) playerParts(r *http.Request, c store.PartsContext, variantID int64, withAll bool) (playerPartsJSON, error) {
	p := personFromContext(r)
	out := playerPartsJSON{Parts: []string{"voice"}, From: store.PartsFromDefault, Context: partsContextJSON{Kind: c.Kind, ID: c.ID, Name: s.partsContextName(r, c)}}
	if p != nil {
		res, err := s.store.ResolvePlayerParts(r.Context(), p.ID, c, variantID)
		if err != nil {
			return out, err
		}
		out.Parts, out.From = res.Instruments, res.From
	}
	if withAll {
		lang := s.langOfRequest(r)
		for _, in := range instruments.All {
			out.All = append(out.All, partsInstrument{ID: in.ID, Label: instruments.Label(in.ID, lang), Stem: in.Stem})
		}
	}
	return out, nil
}

// playerPartsData is the song page's data-parts: the reader's parts here,
// where they came from, the context, and the instruments to pick from.
func (s *Server) playerPartsData(r *http.Request, data map[string]any, variantID int64) string {
	c := s.partsContext(r, data)
	pp, err := s.playerParts(r, c, variantID, true)
	if err != nil {
		return ""
	}
	b, _ := json.Marshal(pp)
	return string(b)
}

// handlePutPlayerParts is PUT /api/songs/{id}/parts: the reader's parts,
// {"context": {"kind": "band", "id": 3}, "scope": "song"|"context",
// "instruments": ["guitar"]} to save, or with "clear": true to forget that
// choice. Answers with the parts as they now resolve for this song. Only
// one's own; a band or community one belongs to.
func (s *Server) handlePutPlayerParts(w http.ResponseWriter, r *http.Request) {
	variantID, err := pathID(r, "id")
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid id")
		return
	}
	var req struct {
		Context     partsContextJSON `json:"context"`
		Scope       string           `json:"scope"`
		Instruments []string         `json:"instruments"`
		Clear       bool             `json:"clear"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	me := personFromContext(r)
	ctx := r.Context()
	c := store.PartsContext{Kind: req.Context.Kind, ID: req.Context.ID}
	var member bool
	switch c.Kind {
	case "":
		member = c.ID == 0
	case "band":
		member, err = s.store.IsCurrentBandMember(ctx, c.ID, me.ID)
	case "community":
		member, err = s.store.IsCurrentCommunityMember(ctx, c.ID, me.ID)
	default:
		httpError(w, http.StatusBadRequest, "unknown context")
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if !member && !me.IsAdmin {
		httpError(w, http.StatusForbidden, "not one of your bands or communities")
		return
	}
	scopeVariant := int64(0)
	switch req.Scope {
	case "song":
		scopeVariant = variantID
	case "context":
	default:
		httpError(w, http.StatusBadRequest, `scope: "song" or "context"`)
		return
	}
	if req.Clear {
		err = s.store.ClearPlayerParts(ctx, me.ID, c, scopeVariant)
	} else {
		err = s.store.SetPlayerParts(ctx, me.ID, c, scopeVariant, req.Instruments)
	}
	if errors.Is(err, store.ErrBadParts) {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	pp, err := s.playerParts(r, c, variantID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, pp)
}
