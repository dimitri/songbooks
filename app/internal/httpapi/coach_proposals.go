package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"app/internal/chordsym"
	"app/internal/i18n"
	"app/internal/store"
)

// A student proposes a song of their own to the coach (docs/coaching.md,
// "A student proposes a song"; store/coaching_proposals.go): from the
// song's page (⋯ "Propose to Inès…") or from the coaching Songbooks
// section ("Propose a song…", their own songbook's songs to pick from, or
// one found or made there and then), with a note and, if they like, the
// key they'd sing it in. The coach finds it with the "Ask for it" requests
// (one Requests list, each saying which it is), opens and plays it, and
// accepts it (into the repertoire and the student's songbook with the
// coach, in a key: the one wished, or the coach's) or declines it with a
// note. The student is told either way, and keeps the song in their own
// songbook regardless.

// proposeTarget is a coaching community the viewer may propose the song
// to, on the song's page.
type proposeTarget struct {
	CommunityID int64
	CoachName   string
	CoachFirst  string
	// Waiting: proposed already, not answered yet (proposing again brings
	// the note and key up to date); InBook: it's in their songbook there.
	Waiting bool
	InBook  bool
}

// addProposeTargets puts on the song page whom the viewer may propose the
// song to (data "ProposeTo"): the coaching communities they're a student
// of, when the song may go there (store.ProposalCheck).
func (s *Server) addProposeTargets(r *http.Request, person *store.Person, variantID int64, data map[string]any) {
	if person == nil {
		return
	}
	ctx := r.Context()
	cs, err := s.store.StudentCoachingCommunities(ctx, person.ID)
	if err != nil || len(cs) == 0 {
		return
	}
	var out []proposeTarget
	for _, c := range cs {
		if s.store.ProposalCheck(ctx, person.ID, c.ID, variantID) != nil {
			continue
		}
		t := proposeTarget{CommunityID: c.ID, CoachName: s.store.CoachName(ctx, c.ID)}
		t.CoachFirst = firstWord(t.CoachName)
		if sb, err := s.store.FindStudentSongbook(ctx, c.ID, person.ID); err == nil && sb != 0 {
			if target, _, ok, err := s.store.ProposalTarget(ctx, c.ID, person.ID, variantID); err == nil && ok {
				if n, err := s.store.SongbookHasVariant(ctx, sb, target); err == nil && n {
					t.InBook = true
				}
			}
		}
		if q, err := s.store.WaitingProposal(ctx, c.ID, person.ID, variantID); err == nil && q != nil {
			t.Waiting = true
		}
		out = append(out, t)
	}
	if len(out) > 0 {
		data["ProposeTo"] = out
	}
}

// proposableSong is a song of the student's own songbook, as the
// "Propose a song" dialog lists it.
type proposableSong struct {
	ID     int64  `json:"id"`
	Title  string `json:"title"`
	Artist string `json:"artist"`
	Key    string `json:"key"`
	// State: "" (may be proposed), "waiting" (proposed, not answered),
	// "in" (in their songbook with the coach), "no" (may not go: Why).
	State string `json:"state"`
	Why   string `json:"why,omitempty"`
}

// studentContext checks a student's call on a coaching community: a
// current student (never the coach, never a read-only preview).
func (s *Server) studentContext(w http.ResponseWriter, r *http.Request) (*store.Community, *store.CommunityMember, bool) {
	community, coach, ok := s.coachingContext(w, r)
	if !ok {
		return nil, nil, false
	}
	if coach {
		httpError(w, http.StatusForbidden, "the coach adds songs; students propose them")
		return nil, nil, false
	}
	m, ok := s.currentStudent(r, community.ID, personFromContext(r).ID)
	if !ok {
		httpError(w, http.StatusForbidden, "only a student of this community can propose a song")
		return nil, nil, false
	}
	return community, m, true
}

// handleGetProposableSongs is GET /api/communities/{id}/song-proposals/songs:
// the student's own songbook's songs, each with whether it may be
// proposed to this community's coach.
func (s *Server) handleGetProposableSongs(w http.ResponseWriter, r *http.Request) {
	community, m, ok := s.studentContext(w, r)
	if !ok {
		return
	}
	ctx := r.Context()
	lang := s.langOfRequest(r)
	songs, err := s.store.OwnSongbookSongs(ctx, m.PersonID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	songs = s.withKeys(ctx, songs)
	inBook := map[int64]bool{}
	if sb, err := s.store.FindStudentSongbook(ctx, community.ID, m.PersonID); err == nil && sb != 0 {
		if mine, err := s.store.ListStudentSongs(ctx, sb); err == nil {
			for _, sg := range mine {
				inBook[sg.VariantID] = true
			}
		}
	}
	out := make([]proposableSong, 0, len(songs))
	for _, sg := range songs {
		p := proposableSong{ID: sg.VariantID, Title: sg.Title, Artist: sg.Artist, Key: sg.Key}
		switch {
		case s.store.ProposalCheck(ctx, m.PersonID, community.ID, sg.VariantID) != nil:
			p.State, p.Why = "no", i18n.T(lang, "someone else’s private song")
		case inBook[sg.VariantID]:
			p.State = "in"
		default:
			if target, _, ok, err := s.store.ProposalTarget(ctx, community.ID, m.PersonID, sg.VariantID); err == nil && ok && inBook[target] {
				p.State = "in"
			} else if q, err := s.store.WaitingProposal(ctx, community.ID, m.PersonID, sg.VariantID); err == nil && q != nil {
				p.State = "waiting"
			}
		}
		out = append(out, p)
	}
	writeJSON(w, http.StatusOK, map[string]any{"songs": out})
}

// handlePostSongProposal is POST /api/communities/{id}/song-proposals
// {variantId, note, semitones (null: no key said)}: a student proposes a
// song of their own to the coach. The song must be theirs or public (not
// a band's, another community's or a closed songbook's), not in their
// songbook with the coach already. It goes into their own songbook if it
// isn't there (they keep it whatever the answer). Each of the staff is
// told, once while it waits.
func (s *Server) handlePostSongProposal(w http.ResponseWriter, r *http.Request) {
	community, m, ok := s.studentContext(w, r)
	if !ok {
		return
	}
	var req struct {
		VariantID int64  `json:"variantId"`
		Note      string `json:"note"`
		Semitones *int   `json:"semitones"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || req.VariantID == 0 {
		httpError(w, http.StatusBadRequest, "which song?")
		return
	}
	ctx := r.Context()
	lang := s.langOfRequest(r)
	if err := s.store.ProposalCheck(ctx, m.PersonID, community.ID, req.VariantID); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			httpError(w, http.StatusNotFound, "no such song")
			return
		}
		httpError(w, http.StatusForbidden, i18n.T(lang, "Only your own songs, or public ones, can be proposed."))
		return
	}
	if target, _, ok, err := s.store.ProposalTarget(ctx, community.ID, m.PersonID, req.VariantID); err == nil && ok {
		if sb, err := s.store.FindStudentSongbook(ctx, community.ID, m.PersonID); err == nil && sb != 0 {
			if in, err := s.store.SongbookHasVariant(ctx, sb, target); err == nil && in {
				httpError(w, http.StatusConflict, i18n.T(lang, "It’s in your songbook with %s already.", firstWord(s.store.CoachName(ctx, community.ID))))
				return
			}
		}
	}
	if len([]rune(strings.TrimSpace(req.Note))) > store.MaxProposalNote {
		httpError(w, http.StatusBadRequest, i18n.T(lang, "A note is %d characters at most.", store.MaxProposalNote))
		return
	}
	id, created, err := s.store.ProposeSong(ctx, community.ID, m.PersonID, req.VariantID, req.Note, req.Semitones)
	if err != nil {
		if errors.Is(err, store.ErrInvalid) {
			httpError(w, http.StatusBadRequest, err.Error())
			return
		}
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	// Kept in their own songbook, whatever the coach says.
	if mine, err := s.store.GetDefaultSongbook(ctx, "person", m.PersonID); err == nil {
		if s.store.LinkCheckFor(ctx, m.PersonID, req.VariantID, store.PersonOwner(m.PersonID)) == nil {
			_ = s.store.AddToSongbook(ctx, mine, req.VariantID, m.PersonID)
		}
	}
	if created {
		q, err := s.store.GetSongRequest(ctx, id)
		if err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if err := s.notifyCoachesOfProposal(r, community, q); err != nil {
			httpError(w, http.StatusInternalServerError, err.Error())
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": id, "status": store.SongRequestPending, "created": created})
}

// notifyCoachesOfProposal tells each of the community's staff a student
// proposed a song, in the staff member's language, the request one tap
// away.
func (s *Server) notifyCoachesOfProposal(r *http.Request, community *store.Community, q *store.SongRequest) error {
	members, err := s.store.ListCommunityMembers(r.Context(), community.ID, false)
	if err != nil {
		return err
	}
	for _, m := range members {
		if ok, err := s.store.HasAnyCommunityManagePrivilege(r.Context(), community.ID, m.PersonID); err != nil || !ok {
			continue
		}
		lang := s.recipientLang(r, m.PersonID, community.ID)
		body := i18n.T(lang, "%s proposes “%s” for their songbook", q.StudentName, q.Title)
		if _, err := s.store.CreateNotification(r.Context(), m.PersonID, "song_proposal", body, songRequestLink(community.ID, q.ID)); err != nil {
			return err
		}
	}
	return nil
}

// acceptProposal is the coach's yes to a proposal (handlePostSongRequestAdd
// for a proposal): the song into the repertoire (linked when public, the
// community's arrangement of the student's private song otherwise) and
// into the student's songbook in semitones; the student told.
func (s *Server) acceptProposal(w http.ResponseWriter, r *http.Request, community *store.Community, q *store.SongRequest, semitones int, reply string) {
	ctx := r.Context()
	me := personFromContext(r)
	student, ok := s.currentStudent(r, community.ID, q.PersonID)
	if !ok {
		httpError(w, http.StatusConflict, "no longer a student here")
		return
	}
	studentLang := s.recipientLang(r, q.PersonID, community.ID)
	sbID, err := s.store.StudentSongbook(ctx, community.ID, q.PersonID, i18n.T(studentLang, "%s’s songs", student.DisplayName), me.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	got, err := s.store.AcceptSongProposal(ctx, q.ID, me.ID, sbID, semitones, reply)
	switch {
	case errors.Is(err, store.ErrNotFound):
		httpError(w, http.StatusConflict, "already answered")
		return
	case errors.Is(err, store.ErrForbidden):
		httpError(w, http.StatusConflict, i18n.T(s.langOfRequest(r), "This song can’t go into the repertoire any more: its owner made it private, or it’s in a closed songbook."))
		return
	case errors.Is(err, store.ErrInvalid):
		httpError(w, http.StatusBadRequest, err.Error())
		return
	case err != nil:
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	_ = s.store.MarkNotificationsReadByLink(ctx, "song_proposal", songRequestLink(community.ID, q.ID))
	body := i18n.T(studentLang, "%s took “%s”: it’s in your songbook", me.DisplayName, q.Title)
	if _, err := s.store.CreateNotification(ctx, q.PersonID, "song_proposal_accepted", body, fmt.Sprintf("/song/%d?songbook=%d", got.SongID, sbID)); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	key := s.songKeyOf(ctx, got.SongID)
	writeJSON(w, http.StatusOK, map[string]any{"songbookId": sbID, "variantId": got.SongID, "copied": got.Copied,
		"semitones": semitones, "keyName": chordsym.KeyIn(key, semitones)})
}

// declineProposal tells the student the coach's no, with their note.
func (s *Server) declineProposal(r *http.Request, community *store.Community, q *store.SongRequest, reply string) error {
	ctx := r.Context()
	me := personFromContext(r)
	_ = s.store.MarkNotificationsReadByLink(ctx, "song_proposal", songRequestLink(community.ID, q.ID))
	lang := s.recipientLang(r, q.PersonID, community.ID)
	body := i18n.T(lang, "%s answered your proposal “%s”: not this time", me.DisplayName, q.Title)
	if reply != "" {
		body = i18n.T(lang, "%s answered your proposal “%s”: %s", me.DisplayName, q.Title, reply)
	}
	_, err := s.store.CreateNotification(ctx, q.PersonID, "song_proposal_declined", body, fmt.Sprintf("/communities/%d/songbooks#proposal-%d", community.ID, q.ID))
	return err
}

// proposalStart is where the coach's key picker starts for a proposal, in
// semitones from target's key: the key the student wished (from the
// proposed song's key), carried over to the song that enters the
// repertoire when that's another (the community's own arrangement).
func proposalStart(wished *int, songKey, targetKey string) int {
	if wished == nil {
		return 0
	}
	n := *wished
	ks, ok1 := chordsym.ParseKey(songKey)
	kt, ok2 := chordsym.ParseKey(targetKey)
	if ok1 && ok2 && songKey != targetKey {
		d := ((ks.PitchClass()-kt.PitchClass())%12 + 12) % 12
		if d > 5 {
			d -= 12
		}
		n += d
	}
	switch {
	case n > 12:
		n -= 12
	case n < -12:
		n += 12
	}
	return n
}

// proposalRow is one of the student's proposals, as their Songbooks
// section lists it.
type proposalRow struct {
	store.SongRequest
	// Href: the song now in their songbook (accepted), else theirs.
	Href string
}

// studentProposalsData puts the student's proposals on their Songbooks
// section (data "Proposals"), and whether they may propose ("CanPropose").
func (s *Server) studentProposalsData(r *http.Request, community *store.Community, personID, bookID int64, data map[string]any) error {
	if personID == 0 {
		return nil
	}
	if _, ok := s.currentStudent(r, community.ID, personID); !ok {
		return nil
	}
	data["CanPropose"] = true
	if mine, err := s.store.GetDefaultSongbook(r.Context(), "person", personID); err == nil {
		data["MySongbookID"] = mine
	}
	qs, err := s.store.StudentProposals(r.Context(), community.ID, personID)
	if err != nil {
		return err
	}
	rows := make([]proposalRow, len(qs))
	for i, q := range qs {
		rows[i] = proposalRow{SongRequest: q, Href: fmt.Sprintf("/song/%d", q.VariantID)}
		if q.Status == store.SongRequestAdded && q.ResultVariantID != 0 && bookID != 0 {
			rows[i].Href = fmt.Sprintf("/song/%d?songbook=%d", q.ResultVariantID, bookID)
		}
	}
	data["Proposals"] = rows
	return nil
}

// proposalView fills in what the coach's Requests list says of a
// proposal: the key the student wished, where the picker starts, which
// song enters the repertoire and how.
func (s *Server) proposalView(r *http.Request, v *requestView) {
	ctx := r.Context()
	q := v.SongRequest
	songKey := v.Key
	if q.WishedSemitones != nil {
		// "E", or "+2" when the song's key isn't known; "" (the
		// template's "original") unmoved.
		if v.WishedKey = chordsym.KeyIn(songKey, *q.WishedSemitones); v.WishedKey == "" {
			v.WishedKey = keyPill(songKey, *q.WishedSemitones)
		}
	}
	target, copied, ok, err := s.store.ProposalTarget(ctx, q.CommunityID, q.PersonID, q.VariantID)
	if err != nil || !ok {
		v.Start = proposalStart(q.WishedSemitones, songKey, songKey)
		return
	}
	switch {
	case copied:
		v.Accept = "copy"
	case target != q.VariantID:
		v.Accept, v.StripVariant = "own", target
		v.Key = s.songKeyOf(ctx, target)
	default:
		if rights, err := s.store.SongRightsFor(ctx, q.PersonID, q.VariantID); err == nil && rights.Owner == store.CommunityOwner(q.CommunityID) {
			v.Accept = "own"
		} else {
			v.Accept = "link"
		}
	}
	v.Start = proposalStart(q.WishedSemitones, songKey, v.Key)
}
