package httpapi

import (
	"errors"
	"net/http"

	"app/internal/store"
)

// eventOfCommunity guards every /communities/{id}/events|lessons/{eventID}
// route: the community middleware in front of it only checks the caller's
// rights in community {id}, so without this an organiser of their own
// community could act on any other community's event by putting its id in
// the path (e.g. make themselves its manager). The event must belong to
// community {id}; anything else is a 404, like an event that doesn't exist.
func (s *Server) eventOfCommunity(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		communityID, err := pathID(r, "id")
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid community id")
			return
		}
		eventID, err := pathID(r, "eventID")
		if err != nil {
			httpError(w, http.StatusBadRequest, "invalid event id")
			return
		}
		ev, err := s.store.GetEvent(r.Context(), eventID)
		if err != nil {
			if errors.Is(err, store.ErrNotFound) {
				http.NotFound(w, r)
				return
			}
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if ev.OwnerKind != "community" || ev.OwnerCommunityID != communityID {
			http.NotFound(w, r)
			return
		}
		next(w, r)
	}
}
