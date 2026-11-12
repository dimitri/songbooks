package httpapi

import "net/http"

// handleGroupsPage is the phone's fourth tab: everything a person plays
// with, in one place — their next gigs, their bands and their
// communities. Desktop reaches the same through the sidebar's Bands and
// Communities.
func (s *Server) handleGroupsPage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	bands, err := s.store.ListBandsForPerson(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	bandCards, _, err := s.buildBandCards(r.Context(), person.ID, bands)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	communityCards, err := s.buildCommunityCards(r.Context(), person.ID)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	gigs, err := s.store.ListUpcomingGigsForPerson(r.Context(), person.ID, 5)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	s.render(w, "groups", map[string]any{
		"Me": person, "Bands": bandCards, "Communities": communityCards, "Gigs": gigs,
	})
}
