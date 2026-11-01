package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// ErrAlreadyDecided is returned by AcceptProposal/RejectProposal (and the
// vote-triggered auto-decision inside CastVote) when the proposal is no
// longer 'open' — kept distinct from ErrConflict (which means "the
// version you loaded is stale") since a decided proposal isn't a
// stale-read problem, it's a plain "too late" one. CastVote treats this
// as a harmless race (someone else's vote already decided it a moment
// ago) rather than an error to surface.
var ErrAlreadyDecided = fmt.Errorf("proposal is already decided")

// BandProposal is one row of band_proposals, joined with enough context
// (proposer/decider names, target variant's title/book/stem, current vote
// tally) for a proposal list/detail view to render without extra round
// trips.
type BandProposal struct {
	ID                int64
	BandID            int64
	ProposerPersonID  int64
	ProposerUsername  string
	Kind              string // "new_song" or "new_reference"
	TargetVariantID   *int64
	TargetTitle       string
	TargetBook        string
	TargetStem        string
	Payload           json.RawMessage
	Status            string // "open", "accepted", "rejected"
	CreatedAt         time.Time
	DecidedAt         *time.Time
	DecidedByPersonID *int64
	DecidedByUsername string
	YesVotes          int
	VetoVotes         int
}

// NewReferencePayload is the payload shape for kind='new_reference':
// the proposed reference URL, plus an optional free-text note.
type NewReferencePayload struct {
	URL  string `json:"url"`
	Note string `json:"note,omitempty"`
}

// NewSongPayload is the payload shape for kind='new_song'. There's no
// required field — TargetVariantID alone identifies the variant being
// proposed for the band's repertoire — but a free-text note is accepted
// since a proposer may want to say why.
type NewSongPayload struct {
	Note string `json:"note,omitempty"`
}

// BandProposalSnapshot is the entity_type 'band_proposal' snapshot shape
// recorded by RecordRevision — same Exists convention as
// BandMemberSnapshot/BandCatalogEntrySnapshot.
type BandProposalSnapshot struct {
	Exists            bool            `json:"exists"`
	BandID            int64           `json:"band_id"`
	ProposerPersonID  int64           `json:"proposer_person_id"`
	Kind              string          `json:"kind"`
	TargetVariantID   *int64          `json:"target_variant_id"`
	Payload           json.RawMessage `json:"payload"`
	Status            string          `json:"status"`
	CreatedAt         time.Time       `json:"created_at"`
	DecidedAt         *time.Time      `json:"decided_at"`
	DecidedByPersonID *int64          `json:"decided_by_person_id"`
}

// CreateProposal records a new 'new_song' or 'new_reference' proposal.
// targetVariantID is required for both kinds in this phase's simplified
// shape (see migration 0022's comment): for 'new_song' it's the existing
// catalog variant being proposed for the band's repertoire; for
// 'new_reference' it's the existing band variant the proposed URL is
// for. Any current band member may propose (checked by the HTTP layer's
// requireBandMember, same as every other band mutation).
//
// Deliberately does NOT notify anyone — see the package doc comment on
// notification policy at the top of band_proposals_notify.go: creating a
// proposal is discoverable via the "changes since last visit" digest,
// not a push notification, to avoid spamming every member for every
// proposal.
func (st *Store) CreateProposal(ctx context.Context, bandID, proposerID int64, kind string, targetVariantID int64, payload json.RawMessage) (int64, error) {
	if kind != "new_song" && kind != "new_reference" {
		return 0, fmt.Errorf("invalid proposal kind %q", kind)
	}
	if targetVariantID == 0 {
		return 0, fmt.Errorf("a proposal must reference an existing variant id")
	}
	if len(payload) == 0 {
		payload = json.RawMessage("{}")
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO band_proposals (band_id, proposer_person_id, kind, target_variant_id, payload)
		VALUES ($1, $2, $3, $4, $5) RETURNING id`,
		bandID, proposerID, kind, targetVariantID, payload).Scan(&id); err != nil {
		return 0, err
	}

	actor := &proposerID
	if err := st.RecordRevision(ctx, tx, "band_proposal", fmt.Sprint(id), actor, BandProposalSnapshot{Exists: false}); err != nil {
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	return id, nil
}

// proposalFilter narrows ListProposals. Zero values mean "no filter".
type ProposalFilter struct {
	Status string // "open", "accepted", "rejected"
	Kind   string // "new_song", "new_reference"
}

// ListProposals lists bandID's proposals (newest first) matching filter,
// with each proposal's current yes/veto vote counts.
func (st *Store) ListProposals(ctx context.Context, bandID int64, filter ProposalFilter) ([]BandProposal, error) {
	q := `
		SELECT bp.id, bp.band_id, bp.proposer_person_id, p.username, bp.kind::text, bp.target_variant_id,
		       coalesce(s.title, ''), coalesce(pr.slug, ''), coalesce(s.stem, ''),
		       bp.payload, bp.status::text, bp.created_at, bp.decided_at, bp.decided_by_person_id, coalesce(dp.username, ''),
		       (SELECT count(*) FROM proposal_votes v WHERE v.proposal_id = bp.id AND v.vote = 'yes'),
		       (SELECT count(*) FROM proposal_votes v WHERE v.proposal_id = bp.id AND v.vote = 'veto')
		FROM band_proposals bp
		JOIN people p ON p.id = bp.proposer_person_id
		LEFT JOIN songs s ON s.id = bp.target_variant_id
		LEFT JOIN projects pr ON pr.id = s.project_id
		LEFT JOIN people dp ON dp.id = bp.decided_by_person_id
		WHERE bp.band_id = $1`
	args := []any{bandID}
	if filter.Status != "" {
		args = append(args, filter.Status)
		q += fmt.Sprintf(" AND bp.status = $%d", len(args))
	}
	if filter.Kind != "" {
		args = append(args, filter.Kind)
		q += fmt.Sprintf(" AND bp.kind = $%d", len(args))
	}
	q += " ORDER BY bp.created_at DESC"

	rows, err := st.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandProposal
	for rows.Next() {
		var p BandProposal
		if err := rows.Scan(&p.ID, &p.BandID, &p.ProposerPersonID, &p.ProposerUsername, &p.Kind, &p.TargetVariantID,
			&p.TargetTitle, &p.TargetBook, &p.TargetStem, &p.Payload, &p.Status, &p.CreatedAt, &p.DecidedAt,
			&p.DecidedByPersonID, &p.DecidedByUsername, &p.YesVotes, &p.VetoVotes); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// GetProposal loads one proposal by id, same shape ListProposals rows
// have.
func (st *Store) GetProposal(ctx context.Context, proposalID int64) (*BandProposal, error) {
	var p BandProposal
	err := st.db.QueryRow(ctx, `
		SELECT bp.id, bp.band_id, bp.proposer_person_id, p.username, bp.kind::text, bp.target_variant_id,
		       coalesce(s.title, ''), coalesce(pr.slug, ''), coalesce(s.stem, ''),
		       bp.payload, bp.status::text, bp.created_at, bp.decided_at, bp.decided_by_person_id, coalesce(dp.username, ''),
		       (SELECT count(*) FROM proposal_votes v WHERE v.proposal_id = bp.id AND v.vote = 'yes'),
		       (SELECT count(*) FROM proposal_votes v WHERE v.proposal_id = bp.id AND v.vote = 'veto')
		FROM band_proposals bp
		JOIN people p ON p.id = bp.proposer_person_id
		LEFT JOIN songs s ON s.id = bp.target_variant_id
		LEFT JOIN projects pr ON pr.id = s.project_id
		LEFT JOIN people dp ON dp.id = bp.decided_by_person_id
		WHERE bp.id = $1`, proposalID).
		Scan(&p.ID, &p.BandID, &p.ProposerPersonID, &p.ProposerUsername, &p.Kind, &p.TargetVariantID,
			&p.TargetTitle, &p.TargetBook, &p.TargetStem, &p.Payload, &p.Status, &p.CreatedAt, &p.DecidedAt,
			&p.DecidedByPersonID, &p.DecidedByUsername, &p.YesVotes, &p.VetoVotes)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such proposal %d: %w", proposalID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// ProposalVote is one cast vote, for the detail/listening-party view's
// "who voted what" list — no secret ballot per the brief.
type ProposalVote struct {
	PersonID int64
	Username string
	Vote     string
	CastAt   time.Time
}

// ListProposalVotes lists every vote cast on proposalID, most recent
// first.
func (st *Store) ListProposalVotes(ctx context.Context, proposalID int64) ([]ProposalVote, error) {
	rows, err := st.db.Query(ctx, `
		SELECT v.person_id, p.username, v.vote::text, v.cast_at
		FROM proposal_votes v JOIN people p ON p.id = v.person_id
		WHERE v.proposal_id = $1
		ORDER BY v.cast_at DESC`, proposalID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProposalVote
	for rows.Next() {
		var v ProposalVote
		if err := rows.Scan(&v.PersonID, &v.Username, &v.Vote, &v.CastAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// decideProposal locks proposalID, checks it's still 'open', records the
// pre-decide revision, and flips it to newStatus ("accepted"/"rejected").
// decidedBy is nil for a vote-triggered decision (see CastVote's doc
// comment — a veto or a crossed majority threshold isn't any one
// person's unilateral call) and non-nil for a manual accept/reject.
//
// This is called AFTER any side effect the decision implies (repertoire
// add, reference-field update — see AcceptProposal) has already been
// applied, deliberately: if the side effect fails, the proposal should
// stay 'open' so it can be retried, rather than show "accepted" with
// nothing having actually happened.
func (st *Store) decideProposal(ctx context.Context, proposalID int64, newStatus string, decidedBy *int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var snap BandProposalSnapshot
	var kind string
	err = tx.QueryRow(ctx, `
		SELECT band_id, proposer_person_id, kind::text, target_variant_id, payload, status::text, created_at, decided_at, decided_by_person_id
		FROM band_proposals WHERE id = $1 FOR UPDATE`, proposalID).
		Scan(&snap.BandID, &snap.ProposerPersonID, &kind, &snap.TargetVariantID, &snap.Payload, &snap.Status, &snap.CreatedAt, &snap.DecidedAt, &snap.DecidedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such proposal %d: %w", proposalID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if snap.Status != "open" {
		return ErrAlreadyDecided
	}
	snap.Exists = true
	snap.Kind = kind

	if err := st.RecordRevision(ctx, tx, "band_proposal", fmt.Sprint(proposalID), decidedBy, snap); err != nil {
		return err
	}

	if _, err := tx.Exec(ctx, `
		UPDATE band_proposals SET status = $1, decided_at = now(), decided_by_person_id = $2 WHERE id = $3`,
		newStatus, decidedBy, proposalID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// applyAcceptEffect performs the real-world side effect a proposal's
// acceptance implies — adding the target variant to the band's
// repertoire for 'new_song', or updating the target variant's
// "reference" header field for 'new_reference' — reusing the existing
// mechanisms (AddBandCatalogEntry; LoadSong/SaveSong, same path
// handlePutMeta already uses) rather than a second storage location, per
// the brief.
func (st *Store) applyAcceptEffect(ctx context.Context, p *BandProposal, actorPersonID int64) error {
	switch p.Kind {
	case "new_song":
		if p.TargetVariantID == nil {
			return fmt.Errorf("new_song proposal %d has no target variant", p.ID)
		}
		return st.AddBandCatalogEntry(ctx, p.BandID, *p.TargetVariantID, actorPersonID)

	case "new_reference":
		if p.TargetVariantID == nil {
			return fmt.Errorf("new_reference proposal %d has no target variant", p.ID)
		}
		var payload NewReferencePayload
		if err := json.Unmarshal(p.Payload, &payload); err != nil {
			return fmt.Errorf("decoding proposal %d payload: %w", p.ID, err)
		}
		if payload.URL == "" {
			return fmt.Errorf("new_reference proposal %d has no url in its payload", p.ID)
		}
		v, err := st.GetVariant(ctx, *p.TargetVariantID)
		if err != nil {
			return err
		}
		sg, version, err := st.LoadSong(v.Book, v.Stem)
		if err != nil {
			return err
		}
		sg.Set("reference", payload.URL)
		if _, err := st.SaveSong(v.Book, v.Stem, sg, version, actorPersonID); err != nil {
			return err
		}
		return nil

	default:
		return fmt.Errorf("unknown proposal kind %q", p.Kind)
	}
}

// AcceptProposal manually accepts proposalID: any current band member may
// call this (not just the proposer — consistent with "no band owner/
// admin role, no hierarchy" — see bands.go), applies the kind-specific
// side effect, then notifies the proposer (see notifyProposalDecided).
func (st *Store) AcceptProposal(ctx context.Context, proposalID, actorPersonID int64) error {
	return st.decideAndNotify(ctx, proposalID, "accepted", &actorPersonID)
}

// RejectProposal manually rejects proposalID — no side effect, just
// marks it decided and notifies the proposer.
func (st *Store) RejectProposal(ctx context.Context, proposalID, actorPersonID int64) error {
	return st.decideAndNotify(ctx, proposalID, "rejected", &actorPersonID)
}

// decideAndNotify is AcceptProposal/RejectProposal's shared body, also
// used by CastVote for a vote-triggered decision (with decidedBy = nil).
func (st *Store) decideAndNotify(ctx context.Context, proposalID int64, newStatus string, decidedBy *int64) error {
	p, err := st.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if p.Status != "open" {
		return ErrAlreadyDecided
	}

	// decidedBy != nil means a manual accept/reject (see
	// AcceptProposal/RejectProposal) — check membership against the
	// PROPOSAL's own band, not whatever band id the HTTP path happened to
	// carry, same defense-in-depth ForkVariant's scope="band" branch
	// uses. A vote-triggered decision (decidedBy == nil) skips this:
	// CastVote already checked the voter's membership against p.BandID
	// before ever calling here.
	if decidedBy != nil {
		isMember, err := st.IsCurrentBandMember(ctx, p.BandID, *decidedBy)
		if err != nil {
			return err
		}
		if !isMember {
			return fmt.Errorf("person %d is not a current member of band %d: %w", *decidedBy, p.BandID, ErrForbidden)
		}
	}

	if newStatus == "accepted" {
		var actor int64
		if decidedBy != nil {
			actor = *decidedBy
		}
		if err := st.applyAcceptEffect(ctx, p, actor); err != nil {
			return err
		}
	}

	if err := st.decideProposal(ctx, proposalID, newStatus, decidedBy); err != nil {
		return err
	}

	return st.notifyProposalDecided(ctx, p, newStatus)
}

// CastVote records personID's yes/veto vote on proposalID (upserting
// over any previous vote from them — "what is everyone's CURRENT vote,"
// not a history), then checks whether the vote just cast decides the
// proposal:
//
//   - A single veto FORCES rejection immediately, regardless of any yes
//     votes already cast — "veto discard" per the brief, not merely a
//     strong "no." Checked on every vote cast, so a veto arriving after
//     several yes votes still overturns them.
//   - Otherwise, JUDGMENT CALL (not spec'd anywhere else in this app):
//     auto-accept once strictly more than half of the band's CURRENT
//     (core+guest) members have voted yes, using ListBandMembers'
//     current-member count at the moment the decisive vote is cast.
//     There's no formal quorum/band-size concept elsewhere to borrow a
//     threshold from, so this is "more than half of current members,"
//     documented here as a choice rather than a handed-down rule.
//
// Either path, if it fires, is a vote-triggered decision (decidedBy =
// nil in decideAndNotify — no single person unilaterally decided it).
// Manual accept/reject (AcceptProposal/RejectProposal) can still
// short-circuit an undecided vote at any time — voting is an optional
// accelerant, not the only path to a decision, per the brief's low-
// ceremony band model.
func (st *Store) CastVote(ctx context.Context, proposalID, personID int64, vote string) error {
	if vote != "yes" && vote != "veto" {
		return fmt.Errorf("invalid vote %q", vote)
	}

	p, err := st.GetProposal(ctx, proposalID)
	if err != nil {
		return err
	}
	if p.Status != "open" {
		return ErrAlreadyDecided
	}

	isMember, err := st.IsCurrentBandMember(ctx, p.BandID, personID)
	if err != nil {
		return err
	}
	if !isMember {
		return fmt.Errorf("person %d is not a current member of band %d: %w", personID, p.BandID, ErrForbidden)
	}

	if _, err := st.db.Exec(ctx, `
		INSERT INTO proposal_votes (proposal_id, person_id, vote) VALUES ($1, $2, $3)
		ON CONFLICT (proposal_id, person_id) DO UPDATE SET vote = excluded.vote, cast_at = now()`,
		proposalID, personID, vote); err != nil {
		return err
	}

	var hasVeto bool
	if err := st.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM proposal_votes WHERE proposal_id = $1 AND vote = 'veto')`,
		proposalID).Scan(&hasVeto); err != nil {
		return err
	}
	if hasVeto {
		if err := st.decideAndNotify(ctx, proposalID, "rejected", nil); err != nil && !errors.Is(err, ErrAlreadyDecided) {
			return err
		}
		return nil
	}

	var yesVotes int
	if err := st.db.QueryRow(ctx, `
		SELECT count(*) FROM proposal_votes WHERE proposal_id = $1 AND vote = 'yes'`,
		proposalID).Scan(&yesVotes); err != nil {
		return err
	}
	members, err := st.ListBandMembers(ctx, p.BandID, false)
	if err != nil {
		return err
	}
	if len(members) > 0 && yesVotes*2 > len(members) {
		if err := st.decideAndNotify(ctx, proposalID, "accepted", nil); err != nil && !errors.Is(err, ErrAlreadyDecided) {
			return err
		}
	}
	return nil
}
