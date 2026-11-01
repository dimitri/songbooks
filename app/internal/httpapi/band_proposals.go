package httpapi

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"app/internal/store"
)

// handleBandProposalsPage lists a band's proposals (newest first), with
// optional ?status=&kind= filters for the UI's own tabs/filters —
// everything else (vote casting, accept/reject) happens via the JSON
// endpoints below and a page reload, same client-side style as
// band.html/band_catalog.html.
func (s *Server) handleBandProposalsPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	filter := store.ProposalFilter{Status: r.URL.Query().Get("status"), Kind: r.URL.Query().Get("kind")}
	proposals, err := s.store.ListProposals(r.Context(), bandID, filter)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListBandMembers(r.Context(), bandID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_proposals", map[string]any{
		"Me": person, "Band": band, "Proposals": proposals, "MemberCount": len(members),
	})
}

// handleListeningPartyPage is a focused view over the SAME proposal/vote
// data as handleBandProposalsPage — just pre-filtered to open 'new_song'
// proposals and framed as a quick submit-and-vote queue, per the brief's
// "reuse the proposal+vote mechanism rather than building a third
// parallel system." See the package-level note in
// internal/store/band_proposals.go on why no separate "listening party
// session" schema was added.
func (s *Server) handleListeningPartyPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proposals, err := s.store.ListProposals(r.Context(), bandID, store.ProposalFilter{Status: "open", Kind: "new_song"})
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListBandMembers(r.Context(), bandID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "listening_party", map[string]any{
		"Me": person, "Band": band, "Proposals": proposals, "MemberCount": len(members),
	})
}

// handleBandProposalDetailPage is one proposal's detail view: full vote
// tally plus who voted what (no secret ballot per the brief).
func (s *Server) handleBandProposalDetailPage(w http.ResponseWriter, r *http.Request) {
	bandID, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	person := personFromContext(r)
	band, err := s.store.GetBand(r.Context(), bandID)
	if errors.Is(err, store.ErrNotFound) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	proposal, err := s.store.GetProposal(r.Context(), proposalID)
	if errors.Is(err, store.ErrNotFound) || (proposal != nil && proposal.BandID != bandID) {
		http.NotFound(w, r)
		return
	}
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	votes, err := s.store.ListProposalVotes(r.Context(), proposalID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	members, err := s.store.ListBandMembers(r.Context(), bandID, false)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	s.render(w, "band_proposal", map[string]any{
		"Me": person, "Band": band, "Proposal": proposal, "Votes": votes, "MemberCount": len(members),
	})
}

// handleCreateProposal makes a new 'new_song' or 'new_reference'
// proposal. Any current band member may propose (requireBandMember).
// No notification goes out on creation — see CreateProposal's doc
// comment — only on accept/reject.
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
	id, err := s.store.CreateProposal(r.Context(), bandID, actor.ID, req.Kind, req.TargetVariantID, req.Payload)
	if err != nil {
		httpError(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]int64{"id": id})
}

// handleAcceptProposal / handleRejectProposal manually decide a
// proposal — any current band member may call either, not just the
// proposer (consistent with "no band owner/admin role"), overriding or
// short-circuiting any in-progress vote. store.decideAndNotify checks
// the caller is a current member of the PROPOSAL's own band (not
// whatever band id this path happens to carry) before acting.
func (s *Server) handleAcceptProposal(w http.ResponseWriter, r *http.Request) {
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.AcceptProposal(r.Context(), proposalID, actor.ID); err != nil {
		writeProposalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"accepted": true})
}

func (s *Server) handleRejectProposal(w http.ResponseWriter, r *http.Request) {
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	actor := personFromContext(r)
	if err := s.store.RejectProposal(r.Context(), proposalID, actor.ID); err != nil {
		writeProposalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"rejected": true})
}

// handleCastVote casts (or changes) the logged-in person's yes/veto vote
// on a proposal — see store.CastVote for the veto/threshold logic.
func (s *Server) handleCastVote(w http.ResponseWriter, r *http.Request) {
	proposalID, err := strconv.ParseInt(r.PathValue("proposalID"), 10, 64)
	if err != nil {
		httpError(w, http.StatusBadRequest, "invalid proposal id")
		return
	}
	var req struct {
		Vote string `json:"vote"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		httpError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	actor := personFromContext(r)
	if err := s.store.CastVote(r.Context(), proposalID, actor.ID, req.Vote); err != nil {
		writeProposalErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"voted": true})
}

func writeProposalErr(w http.ResponseWriter, err error) {
	if errors.Is(err, store.ErrNotFound) {
		httpError(w, http.StatusNotFound, err.Error())
		return
	}
	if errors.Is(err, store.ErrForbidden) {
		httpError(w, http.StatusForbidden, err.Error())
		return
	}
	if errors.Is(err, store.ErrAlreadyDecided) {
		httpError(w, http.StatusConflict, err.Error())
		return
	}
	httpError(w, http.StatusInternalServerError, err.Error())
}
