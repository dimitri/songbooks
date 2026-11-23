package store

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/jackc/pgx/v5"
)

// A student proposes a song of their own to the coach (docs/coaching.md,
// "A student proposes a song"; migration 0128): a song they found or made
// in their own songbook, with a note and, if they like, the key they'd
// sing it in. It waits with the repertoire's "Ask for it" requests
// (coaching_song_requests, kind 'propose'); while it waits the coach may
// open and play it (SongRights.ViaProposal), nothing more. Accepted, the
// song enters the repertoire and the student's songbook with the coach,
// in a key:
//
//   - a public song (the catalogue's, anyone's made public) is linked: the
//     community's own arrangement of it when it has one already
//     (CommunityVersionOf), else the song itself;
//   - the student's private song becomes the community's arrangement made
//     from it: a copy owned by the community, private, forked from the
//     student's (its media copied), which the coach and the student edit
//     in place as any song of theirs (coachingSongRights). The student's
//     own song stays theirs, in their songbook, untouched;
//   - a song of the community already is itself.
//
// Declined, nothing moves. The student keeps the song in their own
// songbook either way.

// MaxProposalNote is how long a proposal's note, or the coach's answer,
// may be.
const MaxProposalNote = 1000

// proposalSongRights lets a coaching community's staff (and site admins)
// see a song a student proposed to it while the proposal waits
// (SongRights.ViaProposal): open it, play it; no link, no arrangement.
func proposalSongRights(ctx context.Context, q dbtx, r *SongRights, personID int64) error {
	var ok bool
	err := q.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1 FROM coaching_song_requests rq
			WHERE rq.variant_id = $1 AND rq.kind = 'propose' AND rq.status = 'pending'
			  AND (EXISTS (SELECT 1 FROM people WHERE id = $2 AND is_admin)
			       OR EXISTS (SELECT 1 FROM community_members cm
			                  JOIN community_role_privileges crp ON crp.role_id = cm.role_id
			                  WHERE cm.community_id = rq.community_id AND cm.person_id = $2 AND upper_inf(cm.validity)
			                    AND crp.privilege IN ($3, $4, $5, $6, $7))))`,
		r.SongID, personID, PrivManageMembers, PrivManageEvents, PrivManageSubmissions, PrivManageSettings, PrivManageFiles).Scan(&ok)
	if err != nil || !ok {
		return err
	}
	r.See, r.ViaProposal = true, true
	return nil
}

// ProposalCheck says whether personID may propose songID to coaching
// community communityID: nil, or ErrForbidden wrapped with why. A song
// the student sees and that may leave where it is: their own (private or
// public), any public song, or a song of the community's own; never a
// closed songbook's, a band's or another community's private song.
func (st *Store) ProposalCheck(ctx context.Context, personID, communityID, songID int64) error {
	r, err := st.SongRightsFor(ctx, personID, songID)
	if err != nil {
		return err
	}
	switch {
	case !r.See || r.ViaProposal:
		return fmt.Errorf("you can't see this song: %w", ErrForbidden)
	case r.Closed:
		return fmt.Errorf("this song is in a closed songbook: it can't go anywhere else: %w", ErrForbidden)
	case r.Public, r.Owner == PersonOwner(personID), r.Owner == CommunityOwner(communityID):
		return nil
	}
	return fmt.Errorf("this song is someone else’s private song: only your own songs or public ones can be proposed: %w", ErrForbidden)
}

// ProposeSong files a student's proposal of songID to communityID's coach,
// with a note and the key they'd like (nil: none said). created is false
// when the same proposal was waiting already (its note and key brought up
// to date, nothing new to tell the coach). A request of the same song
// answered before, or asked for from the repertoire and waiting, becomes
// this proposal. The caller checks ProposalCheck first.
func (st *Store) ProposeSong(ctx context.Context, communityID, personID, songID int64, note string, wished *int) (id int64, created bool, err error) {
	note = strings.TrimSpace(note)
	if len([]rune(note)) > MaxProposalNote {
		return 0, false, fmt.Errorf("a note is %d characters at most: %w", MaxProposalNote, ErrInvalid)
	}
	if wished != nil && (*wished < -12 || *wished > 12) {
		return 0, false, fmt.Errorf("key out of range: %w", ErrInvalid)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)
	var was, wasKind string
	err = tx.QueryRow(ctx, `
		SELECT status, kind FROM coaching_song_requests WHERE community_id = $1 AND person_id = $2 AND variant_id = $3 FOR UPDATE`,
		communityID, personID, songID).Scan(&was, &wasKind)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, false, err
	}
	waiting := was == SongRequestPending && wasKind == SongRequestPropose
	err = tx.QueryRow(ctx, `
		INSERT INTO coaching_song_requests (community_id, person_id, variant_id, kind, note, wished_semitones)
		VALUES ($1, $2, $3, 'propose', $4, $5)
		ON CONFLICT (community_id, person_id, variant_id) DO UPDATE SET
			kind = 'propose', note = EXCLUDED.note, wished_semitones = EXCLUDED.wished_semitones,
			reply = '', result_variant_id = NULL, decided_at = NULL, decided_by_person_id = NULL,
			created_at = CASE WHEN $6 THEN coaching_song_requests.created_at ELSE now() END,
			status = 'pending'
		RETURNING id`, communityID, personID, songID, note, wished, waiting).Scan(&id)
	if err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return id, !waiting, nil
}

// StudentProposals are a student's proposals to a community, the latest
// first: waiting, accepted (ResultVariantID) or declined (Reply).
func (st *Store) StudentProposals(ctx context.Context, communityID, personID int64) ([]SongRequest, error) {
	rows, err := st.db.Query(ctx, `SELECT `+songRequestColumns+`
		WHERE r.community_id = $1 AND r.person_id = $2 AND r.kind = 'propose'
		ORDER BY (r.status = 'pending') DESC, coalesce(r.decided_at, r.created_at) DESC, r.id DESC`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongRequest
	for rows.Next() {
		q, err := scanSongRequest(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, q)
	}
	return out, rows.Err()
}

// WaitingProposal is personID's proposal of songID waiting in a community,
// nil when there's none.
func (st *Store) WaitingProposal(ctx context.Context, communityID, personID, songID int64) (*SongRequest, error) {
	q, err := scanSongRequest(st.db.QueryRow(ctx, `SELECT `+songRequestColumns+`
		WHERE r.community_id = $1 AND r.person_id = $2 AND r.variant_id = $3 AND r.kind = 'propose' AND r.status = 'pending'`,
		communityID, personID, songID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &q, nil
}

// ProposalTarget is the song a proposal of songID puts in communityID's
// repertoire, and whether accepting it makes it (the community's
// arrangement of the student's private song, made then): the song itself
// when it's the community's; the community's own arrangement of it when
// it has one already; else the song, linked, when it's public; else a
// copy to be made (target songID, copy true). ok false: none of these
// (the song may not go there).
func (st *Store) ProposalTarget(ctx context.Context, communityID, studentID, songID int64) (target int64, copied, ok bool, err error) {
	r, err := songRightsFor(ctx, st.db, studentID, songID)
	if err != nil {
		return 0, false, false, err
	}
	if r.Closed {
		return 0, false, false, nil
	}
	if r.Owner == CommunityOwner(communityID) {
		return songID, false, true, nil
	}
	v, err := st.CommunityVersionOf(ctx, communityID, songID)
	if err != nil {
		return 0, false, false, err
	}
	if v != songID {
		return v, false, true, nil
	}
	if r.Public {
		return songID, false, true, nil
	}
	if r.Owner == PersonOwner(studentID) {
		return songID, true, true, nil
	}
	return 0, false, false, nil
}

// ProposalAccepted is what accepting a proposal did.
type ProposalAccepted struct {
	// SongID is the song now in the repertoire and the student's songbook.
	SongID int64
	// Copied: it's the community's arrangement made from the student's
	// private song, just now.
	Copied            bool
	StudentSongbookID int64
}

// AcceptSongProposal answers a waiting proposal yes, for coachID: the song
// (ProposalTarget) into the community's repertoire and into the student's
// songbook (studentBookID, made by the caller), in semitones (from that
// song's own key), all in one transaction; the request says what entered
// (result_variant_id) and the coach's note. ErrNotFound when the proposal
// isn't waiting any more (the other coach was quicker); ErrForbidden when
// the song may not go there (it went private, into a closed songbook).
func (st *Store) AcceptSongProposal(ctx context.Context, requestID, coachID, studentBookID int64, semitones int, reply string) (*ProposalAccepted, error) {
	if semitones < -12 || semitones > 12 {
		return nil, fmt.Errorf("key out of range: %w", ErrInvalid)
	}
	q, err := st.GetSongRequest(ctx, requestID)
	if err != nil {
		return nil, err
	}
	if !q.IsProposal() || q.Status != SongRequestPending {
		return nil, ErrNotFound
	}
	target, copied, ok, err := st.ProposalTarget(ctx, q.CommunityID, q.PersonID, q.VariantID)
	if err != nil {
		return nil, err
	}
	if !ok {
		return nil, fmt.Errorf("song %d can't go into community %d's repertoire: %w", q.VariantID, q.CommunityID, ErrForbidden)
	}
	repID, err := st.GetDefaultSongbook(ctx, "community", q.CommunityID)
	if err != nil {
		return nil, err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// Still waiting, and only this one answers it.
	var status string
	if err := tx.QueryRow(ctx, `SELECT status FROM coaching_song_requests WHERE id = $1 FOR UPDATE`, requestID).Scan(&status); err != nil {
		return nil, err
	}
	if status != SongRequestPending {
		return nil, ErrNotFound
	}
	out := &ProposalAccepted{SongID: target, Copied: copied, StudentSongbookID: studentBookID}
	if copied {
		// The community's arrangement of the student's song: as
		// MakeVersion makes one (unnamed: CommunityVersionOf finds it
		// again), without touching the student's songbooks.
		if err := tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id, version_name, owner_community_id, forked_from_id,
			                   created_by_person_id, base_body, base_version, base_synced_at)
			SELECT title, artist, reference_url, body, work_id, '', $2, id, $3, body, version, now()
			FROM songs WHERE id = $1
			RETURNING id`, q.VariantID, q.CommunityID, coachID).Scan(&out.SongID); err != nil {
			return nil, err
		}
		if err := copySongMedia(ctx, tx, q.VariantID, out.SongID); err != nil {
			return nil, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)
		ON CONFLICT (songbook_id, variant_id) DO NOTHING`, repID, out.SongID, nullID(coachID)); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id, semitones) VALUES ($1, $2, $3, $4)
		ON CONFLICT (songbook_id, variant_id) DO UPDATE SET semitones = EXCLUDED.semitones`,
		studentBookID, out.SongID, nullID(coachID), semitones); err != nil {
		return nil, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE coaching_song_requests SET status = 'added', decided_at = now(), decided_by_person_id = $2, reply = $3, result_variant_id = $4
		WHERE id = $1`, requestID, nullID(coachID), reply, out.SongID); err != nil {
		return nil, err
	}
	if err := tx.Commit(ctx); err != nil {
		return nil, err
	}
	return out, nil
}

// StudentCoachingCommunities are the coaching communities personID is a
// current student of (a member without the staff role), by name: whom
// they may propose a song to.
func (st *Store) StudentCoachingCommunities(ctx context.Context, personID int64) ([]Community, error) {
	rows, err := st.db.Query(ctx, `
		SELECT c.id, c.name FROM communities c
		JOIN community_members cm ON cm.community_id = c.id AND cm.person_id = $1 AND upper_inf(cm.validity)
		LEFT JOIN community_roles r ON r.id = cm.role_id
		WHERE c.kind = 'coaching' AND coalesce(r.name, '') <> 'staff'
		ORDER BY lower(c.name), c.id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Community
	for rows.Next() {
		var c Community
		if err := rows.Scan(&c.ID, &c.Name); err != nil {
			return nil, err
		}
		c.Kind = "coaching"
		out = append(out, c)
	}
	return out, rows.Err()
}

// OwnSongbookSongs are the songs of personID's own songbooks (My songbook
// and any other of theirs), each once, by title: what they may propose
// from.
func (st *Store) OwnSongbookSongs(ctx context.Context, personID int64) ([]StudentSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT ON (lower(s.title), s.id) s.id, s.title, s.artist, 0,
		       coalesce(substring(s.body from '(?n)^key:[ \t]*([^ \t\r\n]+)'), '')
		FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id JOIN songs s ON s.id = se.variant_id
		WHERE sb.owner_kind = 'person' AND sb.owner_person_id = $1
		ORDER BY lower(s.title), s.id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StudentSong
	for rows.Next() {
		var s StudentSong
		if err := rows.Scan(&s.VariantID, &s.Title, &s.Artist, &s.Semitones, &s.Key); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}
