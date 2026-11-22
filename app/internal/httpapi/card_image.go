package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"app/internal/brand"
	"app/internal/store"
)

// A band's card picture, chosen in its settings (band_settings.html,
// card-image.js): its logo, a mosaic of its songs' covers, or the covers
// it picks (store.CardImage). Every member may see the choice; its core
// members (and site admins) may change it, as they may change its look.

// cardImageChoice is one cover to pick from.
type cardImageChoice struct {
	WorkID int64  `json:"workId"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Album  string `json:"album,omitempty"`
	Image  string `json:"image"`
}

// cardImageState is what the settings show: the choice, and what the
// preview draws from (the same rules as the home page's card).
type cardImageState struct {
	Mode    string            `json:"mode"`
	Works   []int64           `json:"works"`
	Name    string            `json:"name"`
	Logo    string            `json:"logo"`
	Mark    string            `json:"mark"`
	TileBg  string            `json:"tileBg"`
	TileFg  string            `json:"tileFg"`
	Choices []cardImageChoice `json:"choices"`
	// The mosaic sizes, as the home page's (home.go).
	CardMosaic int `json:"cardMosaic"`
	MaxPicked  int `json:"maxPicked"`
}

func (s *Server) cardImageState(r *http.Request, kind string, id int64) (cardImageState, error) {
	ctx := r.Context()
	choice, err := s.store.GetCardImage(ctx, kind, id)
	if err != nil {
		return cardImageState{}, err
	}
	b, name, err := s.store.GetBrand(ctx, kind, id)
	if err != nil {
		return cardImageState{}, err
	}
	st := cardImageState{Mode: choice.Mode, Works: choice.Works, Name: name, Mark: initials(name),
		Choices: []cardImageChoice{}, CardMosaic: homeCardMosaic, MaxPicked: store.CardMaxPicked}
	if st.Works == nil {
		st.Works = []int64{}
	}
	if t := brand.Resolve(b, name); t != nil {
		st.Logo = t.LogoURL
		if t.Mark != "" {
			st.Mark = t.Mark
		}
		st.TileBg = t.Header
		if st.TileBg == "" {
			st.TileBg = t.Accent
		}
		st.TileFg = brand.ContrastOn(st.TileBg)
	}
	songs, err := s.store.OwnerCovers(ctx, kind, id)
	if err != nil {
		return cardImageState{}, err
	}
	for _, c := range store.CoverChoices(songs) {
		st.Choices = append(st.Choices, cardImageChoice{WorkID: c.WorkID, Title: c.Title, Artist: c.Artist, Album: c.Album, Image: c.Image})
	}
	return st, nil
}

// handleGetCardImage is GET /api/bands/{id}/card-image: the choice and
// what the preview needs.
func (s *Server) handleGetCardImage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		st, err := s.cardImageState(r, kind, id)
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		}
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}

// handleSetCardImage is POST /api/bands/{id}/card-image, {"mode": "",
// "logo", "mosaic" or "picked", "works": [workID...]}: the choice saved
// (an unknown mode is automatic; picked works that aren't the band's
// songs' covers are dropped). The new state comes back.
func (s *Server) handleSetCardImage(kind string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid id")
			return
		}
		var in store.CardImage
		if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4<<10)).Decode(&in); err != nil {
			httpError(w, http.StatusBadRequest, "invalid card image")
			return
		}
		in = in.Clean()
		if in.Mode == store.CardPicked {
			songs, err := s.store.OwnerCovers(r.Context(), kind, id)
			if err != nil {
				httpError(w, http.StatusInternalServerError, err.Error())
				return
			}
			ours := map[int64]bool{}
			for _, c := range store.CoverChoices(songs) {
				ours[c.WorkID] = true
			}
			var works []int64
			for _, w := range in.Works {
				if ours[w] {
					works = append(works, w)
				}
			}
			if len(works) == 0 {
				httpError(w, http.StatusBadRequest, "pick at least one of the songs' covers")
				return
			}
			in.Works = works
		}
		if err := s.store.SetCardImage(r.Context(), kind, id, in); errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "not found")
			return
		} else if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		st, err := s.cardImageState(r, kind, id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		writeJSON(w, http.StatusOK, st)
	}
}
