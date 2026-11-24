package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/instruments"
)

// PersonInstrument is one instrument on a person's profile (migration
// 0101): a canonical instrument (internal/instruments), the person's own
// word for it when it's "other", their level if they've said, and where
// it was collected from.
type PersonInstrument struct {
	ID         int64
	PersonID   int64
	Instrument string // instruments.All ID
	OtherName  string // for instruments.Other: the person's word ("theremin")
	Level      string // instruments.Levels ID, "" not said (or not shown to this viewer)
	Source     string // InstrumentFromSelf, …Band, …Community, …Event
	// SourceName is the band, community or event it came from, when it
	// still exists ("" for self).
	SourceName string
	SourceID   int64 // that band's, community's or event's id
	Original   string
	UpdatedAt  time.Time
}

// Name is the instrument's name in lang: the person's own word for an
// "other".
func (pi PersonInstrument) Name(lang string) string {
	if pi.Instrument == instruments.Other && pi.OtherName != "" {
		return pi.OtherName
	}
	return instruments.Label(pi.Instrument, lang)
}

// Where an instrument on a profile came from.
const (
	InstrumentFromSelf      = "self"
	InstrumentFromBand      = "band"
	InstrumentFromCommunity = "community"
	InstrumentFromEvent     = "event"
)

// InstrumentSource is where collected instruments come from: Kind is one
// of the InstrumentFrom* constants, ID the band, community or event.
type InstrumentSource struct {
	Kind string
	ID   int64
}

// maxOtherName caps an "other" instrument's own word.
const maxOtherName = 40

// ListPersonInstruments is personID's instruments, the ones they haven't
// taken off, in the instruments' display order.
func (st *Store) ListPersonInstruments(ctx context.Context, personID int64) ([]PersonInstrument, error) {
	m, err := listPersonInstruments(ctx, st.db, []int64{personID})
	if err != nil {
		return nil, err
	}
	return m[personID], nil
}

func listPersonInstruments(ctx context.Context, q dbtx, personIDs []int64) (map[int64][]PersonInstrument, error) {
	rows, err := q.Query(ctx, `
		SELECT pi.id, pi.person_id, pi.instrument, pi.other_name, coalesce(pi.level, ''), pi.source,
		       coalesce(b.name, c.name, e.name, ''), coalesce(pi.source_band_id, pi.source_community_id, pi.source_event_id, 0),
		       pi.original, pi.updated_at
		FROM person_instruments pi
		LEFT JOIN bands b ON b.id = pi.source_band_id
		LEFT JOIN communities c ON c.id = pi.source_community_id
		LEFT JOIN events e ON e.id = pi.source_event_id
		WHERE pi.person_id = ANY($1) AND pi.removed_at IS NULL
		ORDER BY pi.person_id, pi.id`, personIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64][]PersonInstrument{}
	for rows.Next() {
		var pi PersonInstrument
		if err := rows.Scan(&pi.ID, &pi.PersonID, &pi.Instrument, &pi.OtherName, &pi.Level, &pi.Source,
			&pi.SourceName, &pi.SourceID, &pi.Original, &pi.UpdatedAt); err != nil {
			return nil, err
		}
		out[pi.PersonID] = append(out[pi.PersonID], pi)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for id, list := range out {
		sortPersonInstruments(list)
		out[id] = list
	}
	return out, nil
}

func sortPersonInstruments(list []PersonInstrument) {
	ids := make([]string, len(list))
	byKey := map[string]PersonInstrument{}
	for i, pi := range list {
		key := pi.Instrument
		if pi.Instrument == instruments.Other {
			// "other" entries after the canonical ones, by their word
			key = "~" + strings.ToLower(pi.OtherName)
		}
		ids[i] = key
		byKey[key] = pi
	}
	instruments.Sort(ids)
	for i, k := range ids {
		list[i] = byKey[k]
	}
}

// AddInstrument puts an instrument on personID's own profile (source
// self if it's new), at level ("" leaves the level as it is). Adding one
// the person took off brings it back. otherName is their word for an
// instruments.Other and ignored otherwise.
func (st *Store) AddInstrument(ctx context.Context, personID int64, instrument, otherName, level string) (int64, error) {
	instrument, otherName, err := cleanInstrument(instrument, otherName)
	if err != nil {
		return 0, err
	}
	if level != "" && !instruments.ValidLevel(level) {
		return 0, fmt.Errorf("unknown level %q", level)
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var id int64
	err = tx.QueryRow(ctx, `
		INSERT INTO person_instruments (person_id, instrument, other_name, level, source, original)
		VALUES ($1, $2, $3, nullif($4, ''), 'self', $3)
		ON CONFLICT (person_id, instrument, lower(other_name)) DO UPDATE
		   SET removed_at = NULL,
		       level = coalesce(nullif($4, ''), person_instruments.level),
		       updated_at = now()
		RETURNING id`, personID, instrument, otherName, level).Scan(&id)
	if err != nil {
		return 0, err
	}
	if err := syncPeopleInstruments(ctx, tx, personID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

func cleanInstrument(instrument, otherName string) (string, string, error) {
	if !instruments.Valid(instrument) {
		return "", "", fmt.Errorf("unknown instrument %q", instrument)
	}
	if instrument != instruments.Other {
		return instrument, "", nil
	}
	otherName = strings.Join(strings.Fields(otherName), " ")
	if r := []rune(otherName); len(r) > maxOtherName {
		otherName = strings.TrimSpace(string(r[:maxOtherName]))
	}
	if otherName == "" {
		return "", "", fmt.Errorf("say which instrument")
	}
	// Typed into "other" but one we know: file it there.
	if ids, unknown := instruments.Parse(otherName); len(ids) == 1 && len(unknown) == 0 {
		return ids[0], "", nil
	}
	return instrument, otherName, nil
}

// SetInstrumentLevel sets the level (or "" to unsay it) of one of
// personID's own instruments. ErrNotFound if it isn't theirs.
func (st *Store) SetInstrumentLevel(ctx context.Context, personID, id int64, level string) error {
	if level != "" && !instruments.ValidLevel(level) {
		return fmt.Errorf("unknown level %q", level)
	}
	tag, err := st.db.Exec(ctx, `
		UPDATE person_instruments SET level = nullif($3, ''), updated_at = now()
		WHERE id = $1 AND person_id = $2 AND removed_at IS NULL`, id, personID, level)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no instrument %d on this profile: %w", id, ErrNotFound)
	}
	return nil
}

// RemoveInstrument takes one of personID's instruments off their
// profile. It's kept, hidden, so that it isn't collected again from the
// band or event it came from; adding it back by hand restores it.
func (st *Store) RemoveInstrument(ctx context.Context, personID, id int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `
		UPDATE person_instruments SET removed_at = now(), updated_at = now()
		WHERE id = $1 AND person_id = $2 AND removed_at IS NULL`, id, personID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no instrument %d on this profile: %w", id, ErrNotFound)
	}
	if err := syncPeopleInstruments(ctx, tx, personID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// CollectInstruments adds the instruments in texts (free text, read with
// instruments.Parse) to personID's profile as collected from src. Only
// what's missing is added: an instrument already there keeps its level
// and source, and one the person took off stays off.
func (st *Store) CollectInstruments(ctx context.Context, personID int64, texts []string, src InstrumentSource) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if err := collectInstruments(ctx, tx, personID, texts, src, true); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// collectInstruments is CollectInstruments in a transaction; sync also
// rewrites people.instruments when something was added (the backfill
// doesn't, to keep the text people typed where it's shown today).
func collectInstruments(ctx context.Context, q dbtx, personID int64, texts []string, src InstrumentSource, sync bool) error {
	var bandID, communityID, eventID any
	switch src.Kind {
	case InstrumentFromBand:
		bandID = nullableID(src.ID)
	case InstrumentFromCommunity:
		communityID = nullableID(src.ID)
	case InstrumentFromEvent:
		eventID = nullableID(src.ID)
	case InstrumentFromSelf:
	default:
		return fmt.Errorf("unknown instrument source %q", src.Kind)
	}
	added := false
	for _, text := range texts {
		text = strings.TrimSpace(text)
		if text == "" {
			continue
		}
		ids, unknown := instruments.Parse(text)
		type entry struct{ id, other string }
		var entries []entry
		for _, id := range ids {
			entries = append(entries, entry{id, ""})
		}
		for _, u := range unknown {
			if _, other, err := cleanInstrument(instruments.Other, u); err == nil {
				entries = append(entries, entry{instruments.Other, other})
			}
		}
		for _, e := range entries {
			tag, err := q.Exec(ctx, `
				INSERT INTO person_instruments (person_id, instrument, other_name, source, source_band_id, source_community_id, source_event_id, original)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
				ON CONFLICT (person_id, instrument, lower(other_name)) DO NOTHING`,
				personID, e.id, e.other, src.Kind, bandID, communityID, eventID, text)
			if err != nil {
				return err
			}
			added = added || tag.RowsAffected() > 0
		}
	}
	if added && sync {
		return syncPeopleInstruments(ctx, q, personID)
	}
	return nil
}

// UpdateOwnInstruments is the welcome wizard's "What do you play?":
// add is what the person ticked or typed (free text, read with
// instruments.Parse), put on the profile or brought back; takeOff is the
// canonical instruments the wizard showed ticked that the person
// unticked, and only those are taken off. Nothing else on the profile
// changes: an instrument the wizard didn't show ticked (an "other", one
// collected from a band or an event since the page was drawn) stays, and
// so do levels. ErrNotFound if there's no such person.
func (st *Store) UpdateOwnInstruments(ctx context.Context, personID int64, add, takeOff []string) error {
	type entry struct{ id, other, text string }
	var want []entry
	for _, text := range add {
		text = strings.TrimSpace(text)
		ids, unknown := instruments.Parse(text)
		for _, id := range ids {
			want = append(want, entry{id, "", text})
		}
		for _, u := range unknown {
			if id, other, err := cleanInstrument(instruments.Other, u); err == nil {
				want = append(want, entry{id, other, text})
			}
		}
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var exists bool
	if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people WHERE id = $1)`, personID).Scan(&exists); err != nil {
		return err
	}
	if !exists {
		return fmt.Errorf("no such person id %d: %w", personID, ErrNotFound)
	}
	added := map[string]bool{}
	for _, e := range want {
		if _, err := tx.Exec(ctx, `
			INSERT INTO person_instruments (person_id, instrument, other_name, source, original)
			VALUES ($1, $2, $3, 'self', $4)
			ON CONFLICT (person_id, instrument, lower(other_name)) DO UPDATE
			   SET removed_at = NULL,
			       updated_at = CASE WHEN person_instruments.removed_at IS NULL THEN person_instruments.updated_at ELSE now() END`,
			personID, e.id, e.other, e.text); err != nil {
			return err
		}
		if e.other == "" {
			added[e.id] = true
		}
	}
	var drop []string
	for _, id := range takeOff {
		if instruments.Valid(id) && id != instruments.Other && !added[id] {
			drop = append(drop, id)
		}
	}
	if len(drop) > 0 {
		if _, err := tx.Exec(ctx, `
			UPDATE person_instruments SET removed_at = now(), updated_at = now()
			WHERE person_id = $1 AND instrument = ANY($2) AND other_name = '' AND removed_at IS NULL`,
			personID, drop); err != nil {
			return err
		}
	}
	if err := syncPeopleInstruments(ctx, tx, personID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// syncPeopleInstruments writes personID's instruments back into
// people.instruments, the plain list the band roster and member lists
// read: each canonical ID, or the person's word for an "other".
func syncPeopleInstruments(ctx context.Context, q dbtx, personID int64) error {
	m, err := listPersonInstruments(ctx, q, []int64{personID})
	if err != nil {
		return err
	}
	list := []string{}
	for _, pi := range m[personID] {
		if pi.Instrument == instruments.Other {
			list = append(list, pi.OtherName)
		} else {
			list = append(list, pi.Instrument)
		}
	}
	_, err = q.Exec(ctx, `UPDATE people SET instruments = $1 WHERE id = $2`, list, personID)
	return err
}

// BackfillPersonInstruments collects, for everyone, the instruments the
// app already knew about before profiles had their own: band roster
// slots a person claimed (source: that band), their singing parts in an
// event's program (voice, source: that event), what they said they'd
// play when registering for an event, then people.instruments (source:
// self). Only what's missing is added, and people.instruments isn't
// rewritten, so running it again changes nothing. It returns how many
// rows it added.
func (st *Store) BackfillPersonInstruments(ctx context.Context) (int, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	var before int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM person_instruments`).Scan(&before); err != nil {
		return 0, err
	}
	type item struct {
		person int64
		text   string
		src    InstrumentSource
	}
	var items []item
	queries := []struct {
		kind string
		sql  string
	}{
		{InstrumentFromBand, `
			SELECT s.claimed_by_person_id, s.instrument, gi.band_id
			FROM group_invite_slots s JOIN group_invites gi ON gi.id = s.group_invite_id
			WHERE s.claimed_by_person_id IS NOT NULL AND gi.band_id IS NOT NULL AND coalesce(s.instrument, '') <> ''
			ORDER BY s.claimed_at, s.id`},
		{InstrumentFromEvent, `
			SELECT ps.person_id, 'voice', p.event_id
			FROM event_program_singers ps JOIN event_program p ON p.id = ps.program_id
			ORDER BY p.event_id, ps.program_id`},
		{InstrumentFromEvent, `
			SELECT r.person_id, i, r.event_id
			FROM event_registrations r, unnest(r.instruments) i
			ORDER BY r.registered_at, r.id`},
		{InstrumentFromSelf, `
			SELECT p.id, i, 0::bigint FROM people p, unnest(p.instruments) WITH ORDINALITY AS u(i, n)
			ORDER BY p.id, n`},
	}
	for _, qq := range queries {
		rows, err := tx.Query(ctx, qq.sql)
		if err != nil {
			return 0, err
		}
		for rows.Next() {
			var it item
			if err := rows.Scan(&it.person, &it.text, &it.src.ID); err != nil {
				rows.Close()
				return 0, err
			}
			it.src.Kind = qq.kind
			items = append(items, it)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return 0, err
		}
	}
	for _, it := range items {
		if err := collectInstruments(ctx, tx, it.person, []string{it.text}, it.src, false); err != nil {
			return 0, err
		}
	}
	var after int
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM person_instruments`).Scan(&after); err != nil {
		return 0, err
	}
	return after - before, tx.Commit(ctx)
}

// SetEventRegistrationInstruments records what personID says they'll
// play at eventID they're registered for (canonical IDs; others are
// dropped), and collects those instruments onto their profile from the
// event. ErrNotFound if they aren't registered.
func (st *Store) SetEventRegistrationInstruments(ctx context.Context, eventID, personID int64, ids []string) error {
	clean := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if instruments.Valid(id) && id != instruments.Other && !seen[id] {
			seen[id] = true
			clean = append(clean, id)
		}
	}
	instruments.Sort(clean)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	tag, err := tx.Exec(ctx, `UPDATE event_registrations SET instruments = $3 WHERE event_id = $1 AND person_id = $2`, eventID, personID, clean)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("person %d isn't registered for event %d: %w", personID, eventID, ErrNotFound)
	}
	if err := collectInstruments(ctx, tx, personID, clean, InstrumentSource{InstrumentFromEvent, eventID}, true); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// InstrumentLevelsShared is whether personID lets their bandmates and
// organisers see their levels (people.share_instrument_levels).
func (st *Store) InstrumentLevelsShared(ctx context.Context, personID int64) (bool, error) {
	var on bool
	err := st.db.QueryRow(ctx, `SELECT share_instrument_levels FROM people WHERE id = $1`, personID).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return on, err
}

// SetInstrumentLevelsShared turns that on or off.
func (st *Store) SetInstrumentLevelsShared(ctx context.Context, personID int64, on bool) error {
	_, err := st.db.Exec(ctx, `UPDATE people SET share_instrument_levels = $2 WHERE id = $1`, personID, on)
	return err
}

// levelsVisibleSQL is true when viewer $1 may see person p's levels:
// themselves; an app admin; or, if p shares them, someone in a band with
// p now, someone who runs (members or events) a community p is in now,
// or a manager of an event p registered for. Nobody else: fellow members
// of a big community and the public don't.
const levelsVisibleSQL = `(
	p.id = $1
	OR EXISTS (SELECT 1 FROM people v WHERE v.id = $1 AND v.is_admin)
	OR (p.share_instrument_levels AND (
		EXISTS (SELECT 1 FROM band_members a JOIN band_members b ON b.band_id = a.band_id
		        WHERE a.person_id = $1 AND b.person_id = p.id AND upper_inf(a.validity) AND upper_inf(b.validity))
		OR EXISTS (SELECT 1 FROM community_members a
		           JOIN community_role_privileges crp ON crp.role_id = a.role_id AND crp.privilege IN ('manage_members', 'manage_events')
		           JOIN community_members b ON b.community_id = a.community_id
		           WHERE a.person_id = $1 AND b.person_id = p.id AND upper_inf(a.validity) AND upper_inf(b.validity))
		OR EXISTS (SELECT 1 FROM event_managers m JOIN event_registrations r ON r.event_id = m.event_id
		           WHERE m.person_id = $1 AND r.person_id = p.id)
	))
)`

// CanSeeInstruments reports whether viewerID may see which instruments
// personID plays (not their levels): themselves, an app admin, someone
// in a band or a community with them now, or a manager of an event they
// registered for. viewerID 0 is a visitor: never.
func (st *Store) CanSeeInstruments(ctx context.Context, viewerID, personID int64) (bool, error) {
	if viewerID == 0 {
		return false, nil
	}
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT p.id = $1
		    OR EXISTS (SELECT 1 FROM people v WHERE v.id = $1 AND v.is_admin)
		    OR EXISTS (SELECT 1 FROM band_members a JOIN band_members b ON b.band_id = a.band_id
		               WHERE a.person_id = $1 AND b.person_id = p.id AND upper_inf(a.validity) AND upper_inf(b.validity))
		    OR EXISTS (SELECT 1 FROM community_members a JOIN community_members b ON b.community_id = a.community_id
		               WHERE a.person_id = $1 AND b.person_id = p.id AND upper_inf(a.validity) AND upper_inf(b.validity))
		    OR EXISTS (SELECT 1 FROM event_managers m JOIN event_registrations r ON r.event_id = m.event_id
		               WHERE m.person_id = $1 AND r.person_id = p.id)
		FROM people p WHERE p.id = $2`, viewerID, personID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return ok, err
}

// CanSeeInstrumentLevels reports whether viewerID may see personID's
// levels (see levelsVisibleSQL). viewerID 0 is a visitor: never.
func (st *Store) CanSeeInstrumentLevels(ctx context.Context, viewerID, personID int64) (bool, error) {
	if viewerID == 0 {
		return false, nil
	}
	var ok bool
	err := st.db.QueryRow(ctx, `SELECT `+levelsVisibleSQL+` FROM people p WHERE p.id = $2`, viewerID, personID).Scan(&ok)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, ErrNotFound
	}
	return ok, err
}

// InstrumentsSeenBy is personIDs' instruments as viewerID may see them:
// every instrument, but the level only where CanSeeInstrumentLevels
// allows (blanked otherwise). viewerID 0 is a visitor.
func (st *Store) InstrumentsSeenBy(ctx context.Context, viewerID int64, personIDs []int64) (map[int64][]PersonInstrument, error) {
	if len(personIDs) == 0 {
		return map[int64][]PersonInstrument{}, nil
	}
	m, err := listPersonInstruments(ctx, st.db, personIDs)
	if err != nil {
		return nil, err
	}
	visible := map[int64]bool{}
	if viewerID != 0 {
		rows, err := st.db.Query(ctx, `SELECT p.id FROM people p WHERE p.id = ANY($2) AND `+levelsVisibleSQL, viewerID, personIDs)
		if err != nil {
			return nil, err
		}
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			visible[id] = true
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
	}
	for id, list := range m {
		if visible[id] {
			continue
		}
		for i := range list {
			list[i].Level = ""
		}
	}
	return m, nil
}
