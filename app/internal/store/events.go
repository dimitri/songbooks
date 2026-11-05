package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Event is one scheduled thing a community or a band hosts — a jam
// (or its bands-only "scène ouverte" variant, via AudienceScope), a
// band's concert/gig, or a lightweight social event (meetup, listening
// party, dinner, "going to a show together"). See migration 0049's own
// doc comment for the ownership/visibility split: a school-owned event
// gets a public listing/registration page, a band-owned one follows
// the band's own membership gate and is never public.
type Event struct {
	ID                int64
	OwnerKind         string // "school" or "band"
	OwnerSchoolID     int64
	OwnerBandID       int64
	OwnerName         string // the school/band's own name, joined in for display
	EventType         string
	Name              string
	Description       string
	Location          string
	StartsAt          time.Time
	EndsAt            *time.Time
	AudienceScope     string
	SetlistID         *int64
	CreatedByPersonID *int64
	CreatedAt         time.Time
}

// IsPast reports whether e has already happened (EndsAt if set, else
// StartsAt) — the line between a public event page's "upcoming" and
// "past" listings.
func (e Event) IsPast(now time.Time) bool {
	if e.EndsAt != nil {
		return e.EndsAt.Before(now)
	}
	return e.StartsAt.Before(now)
}

const eventSelectCols = `
	e.id, e.owner_kind::text, coalesce(e.owner_school_id,0), coalesce(e.owner_band_id,0),
	coalesce(sc.name, bd.name, ''),
	e.event_type::text, e.name, e.description, e.location, e.starts_at, e.ends_at,
	e.audience_scope::text, e.setlist_id, e.created_by_person_id, e.created_at`

const eventFromJoin = `
	FROM events e
	LEFT JOIN schools sc ON sc.id = e.owner_school_id
	LEFT JOIN bands bd ON bd.id = e.owner_band_id`

func scanEvent(row interface{ Scan(...any) error }) (*Event, error) {
	var ev Event
	var schoolID, bandID int64
	var setlistID *int64
	var createdBy *int64
	if err := row.Scan(&ev.ID, &ev.OwnerKind, &schoolID, &bandID, &ev.OwnerName,
		&ev.EventType, &ev.Name, &ev.Description, &ev.Location, &ev.StartsAt, &ev.EndsAt,
		&ev.AudienceScope, &setlistID, &createdBy, &ev.CreatedAt); err != nil {
		return nil, err
	}
	ev.OwnerSchoolID, ev.OwnerBandID = schoolID, bandID
	ev.SetlistID, ev.CreatedByPersonID = setlistID, createdBy
	return &ev, nil
}

// CreateEvent makes a new event owned by a community (ownerKind="school")
// or a band (ownerKind="band"). audienceScope only matters for
// eventType="jam" ("members", "bands" — the "scène ouverte" case, or
// "both"); pass "members" for every other event type.
func (st *Store) CreateEvent(ctx context.Context, ownerKind string, ownerID int64, eventType, name, description, location string, startsAt time.Time, endsAt *time.Time, audienceScope string, creatorID int64) (int64, error) {
	if ownerKind != "school" && ownerKind != "band" {
		return 0, fmt.Errorf("invalid event owner kind %q", ownerKind)
	}
	var schoolCol, bandCol any
	if ownerKind == "school" {
		schoolCol = ownerID
	} else {
		bandCol = ownerID
	}
	var creator *int64
	if creatorID != 0 {
		creator = &creatorID
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO events (owner_kind, owner_school_id, owner_band_id, event_type, name, description, location, starts_at, ends_at, audience_scope, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		RETURNING id`,
		ownerKind, schoolCol, bandCol, eventType, name, description, location, startsAt, endsAt, audienceScope, creator).Scan(&id)
	return id, err
}

// GetEvent loads one event by id.
func (st *Store) GetEvent(ctx context.Context, eventID int64) (*Event, error) {
	ev, err := scanEvent(st.db.QueryRow(ctx, `SELECT `+eventSelectCols+eventFromJoin+` WHERE e.id = $1`, eventID))
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such event %d: %w", eventID, ErrNotFound)
	}
	return ev, err
}

func (st *Store) listEvents(ctx context.Context, whereCol string, ownerID int64) ([]Event, error) {
	rows, err := st.db.Query(ctx, `SELECT `+eventSelectCols+eventFromJoin+`
		WHERE `+whereCol+` = $1 ORDER BY e.starts_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Event
	for rows.Next() {
		ev, err := scanEvent(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *ev)
	}
	return out, rows.Err()
}

// ListEventsForSchool lists schoolID's events, newest-starting first —
// the community's public events page and its admin "manage events" tab
// both use this, splitting upcoming/past client-side via Event.IsPast.
func (st *Store) ListEventsForSchool(ctx context.Context, schoolID int64) ([]Event, error) {
	return st.listEvents(ctx, "e.owner_school_id", schoolID)
}

// ListEventsForBand lists bandID's own events — never public, gated by
// the band's own membership check (requireBandMember) at the http layer.
func (st *Store) ListEventsForBand(ctx context.Context, bandID int64) ([]Event, error) {
	return st.listEvents(ctx, "e.owner_band_id", bandID)
}

// SetEventSetlist attaches (or replaces) eventID's setlist — an event
// manager's first step in "manage the songbook and the setlist" for
// their event.
func (st *Store) SetEventSetlist(ctx context.Context, eventID, setlistID int64) error {
	tag, err := st.db.Exec(ctx, `UPDATE events SET setlist_id = $1 WHERE id = $2`, setlistID, eventID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such event %d: %w", eventID, ErrNotFound)
	}
	return nil
}

// ---- Event managers ---------------------------------------------------

// EventManager is one person assigned to manage one event (the
// songbook/setlist for it) — a per-event grant, not a standing
// community-wide role (the user's own explicit choice).
type EventManager struct {
	PersonID       int64
	Username       string
	DisplayName    string
	AssignedByName string
	AssignedAt     time.Time
}

// AddEventManager assigns personID to manage eventID. Idempotent: an
// existing assignment is left as-is rather than erroring, so re-
// clicking "assign" in the UI never surfaces a confusing error.
func (st *Store) AddEventManager(ctx context.Context, eventID, personID, assignedByPersonID int64) error {
	var assignedBy *int64
	if assignedByPersonID != 0 {
		assignedBy = &assignedByPersonID
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO event_managers (event_id, person_id, assigned_by_person_id)
		VALUES ($1, $2, $3) ON CONFLICT (event_id, person_id) DO NOTHING`,
		eventID, personID, assignedBy)
	return err
}

// RemoveEventManager revokes personID's management of eventID.
func (st *Store) RemoveEventManager(ctx context.Context, eventID, personID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM event_managers WHERE event_id = $1 AND person_id = $2`, eventID, personID)
	return err
}

// ListEventManagers lists everyone currently assigned to manage eventID.
func (st *Store) ListEventManagers(ctx context.Context, eventID int64) ([]EventManager, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.username, p.display_name, coalesce(ap.display_name, ''), em.assigned_at
		FROM event_managers em
		JOIN people p ON p.id = em.person_id
		LEFT JOIN people ap ON ap.id = em.assigned_by_person_id
		WHERE em.event_id = $1 ORDER BY em.assigned_at`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventManager
	for rows.Next() {
		var m EventManager
		if err := rows.Scan(&m.PersonID, &m.Username, &m.DisplayName, &m.AssignedByName, &m.AssignedAt); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// IsEventManager reports whether personID currently manages eventID.
func (st *Store) IsEventManager(ctx context.Context, eventID, personID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM event_managers WHERE event_id = $1 AND person_id = $2)`,
		eventID, personID).Scan(&ok)
	return ok, err
}

// CanManageEvent is the real gate behind requireEventManager: an
// assigned event manager, OR anyone who could have assigned one in the
// first place (school staff for a school event, any band member for a
// band event — same "whoever can create it can also run it" rule
// every other owner-scoped admin surface in this app already follows).
func (st *Store) CanManageEvent(ctx context.Context, eventID, personID int64) (bool, error) {
	ev, err := st.GetEvent(ctx, eventID)
	if err != nil {
		return false, err
	}
	if ok, err := st.IsEventManager(ctx, eventID, personID); err != nil || ok {
		return ok, err
	}
	if ev.OwnerKind == "school" {
		return st.IsCurrentSchoolStaff(ctx, ev.OwnerSchoolID, personID)
	}
	return st.IsCurrentBandMember(ctx, ev.OwnerBandID, personID)
}

// ---- Event registration ("attending") ---------------------------------

// RegisterForEvent records personID (optionally as/with bandID) as
// registered for eventID. If the event is school-owned, this also
// joins them to that community at its own configured default role
// (schools.default_join_role) — "an event is part of a community so
// the account joins the community," applying that community's own
// edited defaults — a no-op if they're already a current member.
// Idempotent on (event, person): re-registering just leaves the
// existing row alone.
func (st *Store) RegisterForEvent(ctx context.Context, eventID, personID int64, bandID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)

	var ownerKind string
	var ownerSchoolID int64
	var defaultRole string
	if err := tx.QueryRow(ctx, `
		SELECT e.owner_kind::text, coalesce(e.owner_school_id,0), coalesce(sc.default_join_role::text, 'member')
		FROM events e LEFT JOIN schools sc ON sc.id = e.owner_school_id
		WHERE e.id = $1`, eventID).Scan(&ownerKind, &ownerSchoolID, &defaultRole); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("no such event %d: %w", eventID, ErrNotFound)
		}
		return err
	}

	var bandCol any
	if bandID != 0 {
		bandCol = bandID
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO event_registrations (event_id, person_id, band_id) VALUES ($1, $2, $3)
		ON CONFLICT (event_id, person_id) DO NOTHING`, eventID, personID, bandCol); err != nil {
		return err
	}

	if ownerKind == "school" {
		already, err := isCurrentSchoolMember(ctx, tx, ownerSchoolID, personID, "")
		if err != nil {
			return err
		}
		if !already {
			if _, err := tx.Exec(ctx, `
				INSERT INTO school_members (school_id, person_id, role) VALUES ($1, $2, $3)`,
				ownerSchoolID, personID, defaultRole); err != nil {
				return err
			}
		}
	}

	return tx.Commit(ctx)
}

// CreatePersonAndRegisterForEvent is RegisterForEvent's counterpart for
// a brand-new visitor with no account yet — creates the account (same
// live-account-creation convention as CreatePersonAndAcceptInvite, not
// an anonymous jam-style profile: the user's own explicit choice), then
// registers them, all in one transaction so a failed registration never
// leaves an orphan account behind.
func (st *Store) CreatePersonAndRegisterForEvent(ctx context.Context, eventID int64, username, displayName, email, bcryptHash string, bandID int64) (personID int64, err error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)

	var pwHash *string
	if bcryptHash != "" {
		pwHash = &bcryptHash
	}
	if err := tx.QueryRow(ctx,
		`INSERT INTO people (username, display_name, email, password_hash) VALUES ($1, $2, $3, $4) RETURNING id`,
		username, displayName, email, pwHash).Scan(&personID); err != nil {
		if constraintViolated(err, "people_username_key") {
			return 0, ErrUsernameTaken
		}
		if constraintViolated(err, "people_email_key") {
			return 0, ErrEmailTaken
		}
		return 0, err
	}

	if err := tx.Commit(ctx); err != nil {
		return 0, err
	}
	// RegisterForEvent runs its own transaction — fine, the account is
	// already committed and real at this point, same two-step shape
	// AddBandMember/CreatePersonalInvite already use elsewhere.
	if err := st.RegisterForEvent(ctx, eventID, personID, bandID); err != nil {
		return 0, err
	}
	return personID, nil
}

// EventRegistration is one row of ListEventRegistrations — who's
// attending/registered for an event, and as which band if any.
type EventRegistration struct {
	PersonID     int64
	DisplayName  string
	BandID       int64
	BandName     string
	RegisteredAt time.Time
}

// ListEventRegistrations lists everyone registered for eventID.
func (st *Store) ListEventRegistrations(ctx context.Context, eventID int64) ([]EventRegistration, error) {
	rows, err := st.db.Query(ctx, `
		SELECT p.id, p.display_name, coalesce(b.id,0), coalesce(b.name,''), r.registered_at
		FROM event_registrations r
		JOIN people p ON p.id = r.person_id
		LEFT JOIN bands b ON b.id = r.band_id
		WHERE r.event_id = $1 ORDER BY r.registered_at`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventRegistration
	for rows.Next() {
		var reg EventRegistration
		if err := rows.Scan(&reg.PersonID, &reg.DisplayName, &reg.BandID, &reg.BandName, &reg.RegisteredAt); err != nil {
			return nil, err
		}
		out = append(out, reg)
	}
	return out, rows.Err()
}

// ---- Song registration ("claims a setlist slot") -----------------------

// EventSongRegistration is one claim to play a specific song at an
// event — by a band, or by an individual musician, depending on how
// the event's audience_scope is set up.
type EventSongRegistration struct {
	ID            int64
	VariantID     int64
	Title, Artist string
	BandID        int64
	BandName      string
	PersonID      int64
	PersonName    string
	RegisteredAt  time.Time
}

// RegisterSongForEvent claims variantID at eventID, as bandID (band
// registration) or personID (individual musician) — exactly one of the
// two must be nonzero. Multiple registrants for the same song are
// allowed (no uniqueness enforced): who actually plays it is sorted out
// at the event, this just signals intent.
func (st *Store) RegisterSongForEvent(ctx context.Context, eventID, variantID, bandID, personID int64) (int64, error) {
	if (bandID == 0) == (personID == 0) {
		return 0, fmt.Errorf("exactly one of bandID or personID must be set")
	}
	var bandCol, personCol any
	if bandID != 0 {
		bandCol = bandID
	}
	if personID != 0 {
		personCol = personID
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO event_song_registrations (event_id, variant_id, band_id, person_id)
		VALUES ($1, $2, $3, $4) RETURNING id`,
		eventID, variantID, bandCol, personCol).Scan(&id)
	return id, err
}

// RemoveSongRegistration withdraws one song registration by id.
func (st *Store) RemoveSongRegistration(ctx context.Context, registrationID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM event_song_registrations WHERE id = $1`, registrationID)
	return err
}

// ListEventSongRegistrations lists every song registration for eventID,
// newest first.
func (st *Store) ListEventSongRegistrations(ctx context.Context, eventID int64) ([]EventSongRegistration, error) {
	rows, err := st.db.Query(ctx, `
		SELECT r.id, s.id, s.title, s.artist, coalesce(b.id,0), coalesce(b.name,''), coalesce(p.id,0), coalesce(p.display_name,''), r.registered_at
		FROM event_song_registrations r
		JOIN songs s ON s.id = r.variant_id
		LEFT JOIN bands b ON b.id = r.band_id
		LEFT JOIN people p ON p.id = r.person_id
		WHERE r.event_id = $1 ORDER BY r.registered_at DESC`, eventID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []EventSongRegistration
	for rows.Next() {
		var reg EventSongRegistration
		if err := rows.Scan(&reg.ID, &reg.VariantID, &reg.Title, &reg.Artist, &reg.BandID, &reg.BandName, &reg.PersonID, &reg.PersonName, &reg.RegisteredAt); err != nil {
			return nil, err
		}
		out = append(out, reg)
	}
	return out, rows.Err()
}
