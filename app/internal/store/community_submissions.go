package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// CommunitySubmission is one row of community_submissions, joined with enough
// context (student/decider names, source/submitted variant titles) for
// a submissions queue / history view to render without extra round
// trips — same shape BandProposal plays for band_proposals.
type CommunitySubmission struct {
	ID                 int64
	CommunityID           int64
	StudentPersonID    int64
	StudentUsername    string
	Kind               string // "new_song" or "edit"
	SourceVariantID    *int64
	SourceTitle        string
	SubmittedVariantID int64
	SubmittedTitle     string
	Note               string
	Status             string // "pending", "accepted", "rejected"
	CreatedAt          time.Time
	DecidedAt          *time.Time
	DecidedByPersonID  *int64
	DecidedByUsername  string
}

// ErrAlreadyDecided (reused from band_proposals.go — same package, same
// meaning: the submission is no longer 'pending', not a stale-read
// problem).

// CreateCommunitySubmission records a student's submission of work for
// communityID's consideration. kind "new_song" has no sourceVariantID (the
// submitted variant IS the whole proposal); kind "edit" requires one —
// the community/global variant the student is proposing a change to.
// submittedVariantID must be a variant the student themselves owns
// (their own personal-scope fork/draft, created via the normal phase-1
// CreateSong/fork flow BEFORE ever calling this) — enforced here so a
// submission can never point at someone else's work.
func (st *Store) CreateCommunitySubmission(ctx context.Context, communityID, studentPersonID int64, kind string, sourceVariantID *int64, submittedVariantID int64, note string) (int64, error) {
	if kind != "new_song" && kind != "edit" {
		return 0, fmt.Errorf("invalid submission kind %q", kind)
	}
	if kind == "edit" && (sourceVariantID == nil || *sourceVariantID == 0) {
		return 0, fmt.Errorf("an 'edit' submission must reference a source variant id")
	}
	if kind == "new_song" {
		sourceVariantID = nil
	}

	submitted, err := st.GetVariant(ctx, submittedVariantID)
	if err != nil {
		return 0, err
	}
	if submitted.OwnerID == nil || *submitted.OwnerID != studentPersonID {
		return 0, fmt.Errorf("submitted variant %d is not owned by person %d: %w", submittedVariantID, studentPersonID, ErrForbidden)
	}

	if sourceVariantID != nil {
		if _, err := st.GetVariant(ctx, *sourceVariantID); err != nil {
			return 0, err
		}
	}

	var id int64
	err = st.db.QueryRow(ctx, `
		INSERT INTO community_submissions (community_id, student_person_id, kind, source_variant_id, submitted_variant_id, note)
		VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		communityID, studentPersonID, kind, sourceVariantID, submittedVariantID, note).Scan(&id)
	return id, err
}

// submissionCols/submissionJoins is ListCommunitySubmissions/
// GetCommunitySubmission's shared SELECT shape.
const submissionCols = `
	sub.id, sub.community_id, sub.student_person_id, stu.username, sub.kind::text, sub.source_variant_id,
	coalesce(src.title, ''), sub.submitted_variant_id, coalesce(sv.title, ''),
	sub.note, sub.status::text, sub.created_at, sub.decided_at, sub.decided_by_person_id, coalesce(dp.username, '')`
const submissionJoins = `
	FROM community_submissions sub
	JOIN people stu ON stu.id = sub.student_person_id
	LEFT JOIN songs src ON src.id = sub.source_variant_id
	LEFT JOIN songs sv ON sv.id = sub.submitted_variant_id
	LEFT JOIN people dp ON dp.id = sub.decided_by_person_id`

func scanSubmission(row pgx.Row) (*CommunitySubmission, error) {
	var s CommunitySubmission
	err := row.Scan(&s.ID, &s.CommunityID, &s.StudentPersonID, &s.StudentUsername, &s.Kind, &s.SourceVariantID,
		&s.SourceTitle, &s.SubmittedVariantID, &s.SubmittedTitle,
		&s.Note, &s.Status, &s.CreatedAt, &s.DecidedAt, &s.DecidedByPersonID, &s.DecidedByUsername)
	if err != nil {
		return nil, err
	}
	return &s, nil
}

// SubmissionFilter narrows ListCommunitySubmissions. "" means "no filter".
type SubmissionFilter struct {
	Status string // "pending", "accepted", "rejected"
}

// ListCommunitySubmissions lists communityID's submissions, newest first.
func (st *Store) ListCommunitySubmissions(ctx context.Context, communityID int64, filter SubmissionFilter) ([]CommunitySubmission, error) {
	q := `SELECT ` + submissionCols + submissionJoins + ` WHERE sub.community_id = $1`
	args := []any{communityID}
	if filter.Status != "" {
		args = append(args, filter.Status)
		q += fmt.Sprintf(" AND sub.status = $%d", len(args))
	}
	q += " ORDER BY sub.created_at DESC"

	rows, err := st.db.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunitySubmission
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// ListCommunitySubmissionsForStudent lists studentPersonID's own submission
// history across every community, newest first — "your submission history/
// status" on the student-facing page.
func (st *Store) ListCommunitySubmissionsForStudent(ctx context.Context, communityID, studentPersonID int64) ([]CommunitySubmission, error) {
	rows, err := st.db.Query(ctx, `SELECT `+submissionCols+submissionJoins+`
		WHERE sub.community_id = $1 AND sub.student_person_id = $2
		ORDER BY sub.created_at DESC`, communityID, studentPersonID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CommunitySubmission
	for rows.Next() {
		s, err := scanSubmission(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *s)
	}
	return out, rows.Err()
}

// GetCommunitySubmission loads one submission by id.
func (st *Store) GetCommunitySubmission(ctx context.Context, submissionID int64) (*CommunitySubmission, error) {
	row := st.db.QueryRow(ctx, `SELECT `+submissionCols+submissionJoins+` WHERE sub.id = $1`, submissionID)
	s, err := scanSubmission(row)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such submission %d: %w", submissionID, ErrNotFound)
	}
	return s, err
}

// decideCommunitySubmission locks submissionID, checks it's still
// 'pending', and flips it to newStatus. Called AFTER any accept side
// effect has already been applied (same "side effect first, so a
// failure leaves the submission retriable rather than falsely
// 'accepted'" ordering decideProposal uses in band_proposals.go).
func (st *Store) decideCommunitySubmission(ctx context.Context, submissionID int64, newStatus string, decidedByPersonID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var status string
	err = tx.QueryRow(ctx, `SELECT status::text FROM community_submissions WHERE id = $1 FOR UPDATE`, submissionID).Scan(&status)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such submission %d: %w", submissionID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if status != "pending" {
		return ErrAlreadyDecided
	}

	if _, err := tx.Exec(ctx, `
		UPDATE community_submissions SET status = $1, decided_at = now(), decided_by_person_id = $2 WHERE id = $3`,
		newStatus, decidedByPersonID, submissionID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AcceptCommunitySubmission accepts submissionID: instructorPersonID must
// be a current instructor of the submission's own community (checked
// against the SUBMISSION's community_id, not whatever id the HTTP path
// happened to carry — same defense-in-depth ForkVariant's scope="band"/
// "community" branches use). Applies the kind-specific side effect, then
// notifies the student.
//
// For kind="new_song": the student's own submitted variant is pointed
// at from the community's catalog (AddCommunityCatalogEntry) — a pointer, not
// a copy, same "fork/pointer not copy" rule as everywhere else; the
// student keeps owning it, it's now ALSO community material.
//
// For kind="edit": the submitted variant's CONTENT (title/artist/
// reference/body) is copied onto the actual source (community-owned, or
// global) variant via SaveVariantByID — the normal save/edit path,
// which naturally records a revision. JUDGMENT CALL on attribution: the
// revision's actor is recorded as the ACCEPTING INSTRUCTOR
// (instructorPersonID), not the student — the brief frames acceptance
// as the community formally taking ownership of the change, and
// SaveVariantByID's actor convention elsewhere always means "who wrote
// this to the row," which is, at the moment of acceptance, the
// instructor's decision to apply it. The durable record of who
// ORIGINALLY authored the change is never lost: community_submissions.
// student_person_id keeps pointing at the student regardless of this
// revision's own actor. The student's own submitted variant (their
// personal fork) is left untouched either way — accepting an edit never
// deletes or mutates the student's draft, consistent with "rejection
// doesn't delete their work" for the reject path.
func (st *Store) AcceptCommunitySubmission(ctx context.Context, submissionID, instructorPersonID int64) error {
	sub, err := st.GetCommunitySubmission(ctx, submissionID)
	if err != nil {
		return err
	}
	if sub.Status != "pending" {
		return ErrAlreadyDecided
	}

	isInstructor, err := st.IsCurrentCommunityStaff(ctx, sub.CommunityID, instructorPersonID)
	if err != nil {
		return err
	}
	if !isInstructor {
		return fmt.Errorf("person %d is not a current staff member of community %d: %w", instructorPersonID, sub.CommunityID, ErrForbidden)
	}

	switch sub.Kind {
	case "new_song":
		if err := st.AddCommunityCatalogEntry(ctx, sub.CommunityID, sub.SubmittedVariantID, instructorPersonID); err != nil {
			return err
		}
	case "edit":
		if sub.SourceVariantID == nil {
			return fmt.Errorf("edit submission %d has no source variant", sub.ID)
		}
		title, artist, referenceURL, body, err := st.VariantContent(ctx, sub.SubmittedVariantID)
		if err != nil {
			return err
		}
		if _, err := st.SaveVariantByID(ctx, *sub.SourceVariantID, title, artist, referenceURL, body, "", instructorPersonID); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unknown submission kind %q", sub.Kind)
	}

	if err := st.decideCommunitySubmission(ctx, submissionID, "accepted", instructorPersonID); err != nil {
		return err
	}
	return st.notifyCommunitySubmissionDecided(ctx, sub, "accepted")
}

// RejectCommunitySubmission rejects submissionID — no side effect, just
// marks it decided and notifies the student. The submitted variant
// stays in the student's own personal space untouched: rejecting a
// submission never deletes or alters their work.
func (st *Store) RejectCommunitySubmission(ctx context.Context, submissionID, instructorPersonID int64) error {
	sub, err := st.GetCommunitySubmission(ctx, submissionID)
	if err != nil {
		return err
	}
	if sub.Status != "pending" {
		return ErrAlreadyDecided
	}

	isInstructor, err := st.IsCurrentCommunityStaff(ctx, sub.CommunityID, instructorPersonID)
	if err != nil {
		return err
	}
	if !isInstructor {
		return fmt.Errorf("person %d is not a current staff member of community %d: %w", instructorPersonID, sub.CommunityID, ErrForbidden)
	}

	if err := st.decideCommunitySubmission(ctx, submissionID, "rejected", instructorPersonID); err != nil {
		return err
	}
	return st.notifyCommunitySubmissionDecided(ctx, sub, "rejected")
}

// notifyCommunitySubmissionDecided tells the student their submission was
// accepted/rejected — the one notification this phase sends for
// submissions, same "did my thing get picked up" one-recipient pattern
// notifyProposalDecided uses for band proposals.
func (st *Store) notifyCommunitySubmissionDecided(ctx context.Context, sub *CommunitySubmission, newStatus string) error {
	community, err := st.GetCommunity(ctx, sub.CommunityID)
	if err != nil {
		return err
	}
	title := sub.SubmittedTitle
	if title == "" {
		title = fmt.Sprintf("variant %d", sub.SubmittedVariantID)
	}
	var what string
	switch sub.Kind {
	case "new_song":
		what = fmt.Sprintf("your submission of %q to %s's repertoire", title, community.Name)
	case "edit":
		what = fmt.Sprintf("your submitted edit of %q in %s", title, community.Name)
	default:
		what = fmt.Sprintf("your submission in %s", community.Name)
	}
	var body string
	switch newStatus {
	case "accepted":
		body = fmt.Sprintf("Accepted: %s", what)
	case "rejected":
		body = fmt.Sprintf("Rejected: %s", what)
	default:
		body = fmt.Sprintf("Decided (%s): %s", newStatus, what)
	}
	link := fmt.Sprintf("/communities/%d", sub.CommunityID)
	_, err = st.CreateNotification(ctx, sub.StudentPersonID, "community_submission_"+newStatus, body, link)
	return err
}
