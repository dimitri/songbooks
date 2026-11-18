package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// A coaching community (docs/coaching.md, migration 0079): a voice coach
// (its staff) and their students (its other members). The coach notes
// each student's range per register, keeps a songbook per student where
// each song has the student's key, and gives lessons (events of type
// "lesson", the students registered by the coach).

// Registers are a voice's three parts, low to high.
var Registers = []string{"chest", "mixed", "head"}

// VoiceRange is a student's range in one register, MIDI note numbers,
// nil when not noted yet.
type VoiceRange struct {
	Register  string
	Low, High *int
	Note      string
}

// SetCommunityKind makes a community a coaching one, or back.
func (st *Store) SetCommunityKind(ctx context.Context, communityID int64, kind string) error {
	if kind != "community" && kind != "coaching" {
		return fmt.Errorf("invalid community kind %q", kind)
	}
	_, err := st.db.Exec(ctx, `UPDATE communities SET kind = $2 WHERE id = $1`, communityID, kind)
	return err
}

// StudentRanges is a student's three registers, in order, noted or not.
func (st *Store) StudentRanges(ctx context.Context, communityID, personID int64) ([]VoiceRange, error) {
	rows, err := st.db.Query(ctx, `
		SELECT register, low_midi, high_midi, note FROM student_voice_ranges
		WHERE community_id = $1 AND person_id = $2`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	got := map[string]VoiceRange{}
	for rows.Next() {
		var r VoiceRange
		if err := rows.Scan(&r.Register, &r.Low, &r.High, &r.Note); err != nil {
			return nil, err
		}
		got[r.Register] = r
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]VoiceRange, len(Registers))
	for i, reg := range Registers {
		out[i] = got[reg]
		out[i].Register = reg
	}
	return out, nil
}

// SetStudentRange notes a student's range in one register.
func (st *Store) SetStudentRange(ctx context.Context, communityID, personID int64, r VoiceRange, byPersonID int64) error {
	valid := false
	for _, reg := range Registers {
		valid = valid || reg == r.Register
	}
	if !valid {
		return fmt.Errorf("invalid register %q", r.Register)
	}
	if r.Low != nil && r.High != nil && *r.Low > *r.High {
		r.Low, r.High = r.High, r.Low
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO student_voice_ranges (community_id, person_id, register, low_midi, high_midi, note, updated_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (community_id, person_id, register) DO UPDATE SET
			low_midi = EXCLUDED.low_midi, high_midi = EXCLUDED.high_midi, note = EXCLUDED.note,
			updated_by_person_id = EXCLUDED.updated_by_person_id, updated_at = now()`,
		communityID, personID, r.Register, r.Low, r.High, r.Note, nullID(byPersonID))
	return err
}

// StudentNotes is the coach's free text on a student.
func (st *Store) StudentNotes(ctx context.Context, communityID, personID int64) (string, error) {
	var notes string
	err := st.db.QueryRow(ctx, `SELECT notes FROM student_notes WHERE community_id = $1 AND person_id = $2`, communityID, personID).Scan(&notes)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return notes, err
}

// SetStudentNotes replaces the coach's notes on a student.
func (st *Store) SetStudentNotes(ctx context.Context, communityID, personID int64, notes string, byPersonID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO student_notes (community_id, person_id, notes, updated_by_person_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (community_id, person_id) DO UPDATE SET
			notes = EXCLUDED.notes, updated_by_person_id = EXCLUDED.updated_by_person_id, updated_at = now()`,
		communityID, personID, notes, nullID(byPersonID))
	return err
}

// StudentSongbook is the songbook the community keeps for a student, made
// (with name) the first time; only the student (and the staff) open it.
func (st *Store) StudentSongbook(ctx context.Context, communityID, personID int64, name string, byPersonID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT id FROM songbooks WHERE owner_community_id = $1 AND student_person_id = $2`, communityID, personID).Scan(&id)
	if err == nil {
		return id, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return 0, err
	}
	err = st.db.QueryRow(ctx, `
		INSERT INTO songbooks (owner_kind, owner_community_id, name, is_default, kind, student_person_id, requires_grant)
		VALUES ('community', $1, $2, false, 'student', $3, true)
		ON CONFLICT (owner_community_id, student_person_id) WHERE student_person_id IS NOT NULL DO UPDATE SET name = songbooks.name
		RETURNING id`, communityID, name, personID).Scan(&id)
	if err != nil {
		return 0, err
	}
	return id, st.GrantSongbookAccess(ctx, id, personID, byPersonID)
}

// FindStudentSongbook is a student's songbook in a community, 0 when
// there's none yet.
func (st *Store) FindStudentSongbook(ctx context.Context, communityID, personID int64) (int64, error) {
	var id int64
	err := st.db.QueryRow(ctx, `SELECT id FROM songbooks WHERE owner_community_id = $1 AND student_person_id = $2`, communityID, personID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// EntrySemitones is a song's key in a songbook, 0 when it isn't there.
func (st *Store) EntrySemitones(ctx context.Context, songbookID, variantID int64) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `SELECT semitones FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, songbookID, variantID).Scan(&n)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return n, err
}

// SetEntrySemitones sets a song's key in a songbook (a student's key).
func (st *Store) SetEntrySemitones(ctx context.Context, songbookID, variantID int64, semitones int) error {
	if semitones < -12 || semitones > 12 {
		return fmt.Errorf("key out of range: %d", semitones)
	}
	tag, err := st.db.Exec(ctx, `UPDATE songbook_entries SET semitones = $3 WHERE songbook_id = $1 AND variant_id = $2`, songbookID, variantID, semitones)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// StudentSong is a song of a student's songbook, with the student's key.
type StudentSong struct {
	VariantID int64
	Title     string
	Artist    string
	Semitones int
	Key       string // the song's own key (its key: header), "" when none
}

// ListStudentSongs is a student songbook's songs, by title.
func (st *Store) ListStudentSongs(ctx context.Context, songbookID int64) ([]StudentSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.title, s.artist, se.semitones, coalesce(substring(s.body from '(?n)^key:[ \t]*([^ \t\r\n]+)'), '') FROM songbook_entries se
		JOIN songs s ON s.id = se.variant_id
		WHERE se.songbook_id = $1 ORDER BY lower(s.title)`, songbookID)
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

// Lesson events -----------------------------------------------------------

// CoachingLesson is a lesson on the coach's calendar, with its students.
type CoachingLesson struct {
	Event    Event
	Students []EventRegistration
}

// ListCoachingLessons is a community's lessons from since on, soonest
// first; with personID, only that student's.
func (st *Store) ListCoachingLessons(ctx context.Context, communityID int64, since time.Time, personID int64) ([]CoachingLesson, error) {
	events, err := st.ListEventsForCommunity(ctx, communityID)
	if err != nil {
		return nil, err
	}
	var out []CoachingLesson
	for _, ev := range events {
		if ev.EventType != "lesson" || (ev.EndsAt != nil && ev.EndsAt.Before(since)) || (ev.EndsAt == nil && ev.StartsAt.Before(since)) {
			continue
		}
		regs, err := st.ListEventRegistrations(ctx, ev.ID)
		if err != nil {
			return nil, err
		}
		if personID != 0 {
			mine := false
			for _, r := range regs {
				mine = mine || r.PersonID == personID
			}
			if !mine {
				continue
			}
		}
		out = append(out, CoachingLesson{Event: ev, Students: regs})
	}
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j].Event.StartsAt.Before(out[j-1].Event.StartsAt); j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out, nil
}

// UnregisterFromEvent takes a person off an event's registrations (a
// student off a lesson).
func (st *Store) UnregisterFromEvent(ctx context.Context, eventID, personID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM event_registrations WHERE event_id = $1 AND person_id = $2`, eventID, personID)
	return err
}

// DeleteEvent removes an event and what hangs on it.
func (st *Store) DeleteEvent(ctx context.Context, eventID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM events WHERE id = $1`, eventID)
	return err
}

// KeyTarget is someone a coach can give a song's key to: a student of a
// coaching community the song belongs to, or one of its groups, with the
// key the song has in their songbook (InSongbook false when it isn't
// there yet). SongbookID is 0 for a student who has no songbook yet.
type KeyTarget struct {
	CommunityID   int64  `json:"communityId"`
	CommunityName string `json:"communityName"`
	Kind          string `json:"kind"` // "student" or "group"
	PersonID      int64  `json:"personId,omitempty"`
	GroupID       int64  `json:"groupId,omitempty"`
	Name          string `json:"name"`
	SongbookID    int64  `json:"songbookId"`
	InSongbook    bool   `json:"inSongbook"`
	Semitones     int    `json:"semitones"`
}

// CoachingCommunitiesOfSong are the coaching communities that have a song
// in one of their songbooks.
func (st *Store) CoachingCommunitiesOfSong(ctx context.Context, variantID int64) ([]Community, error) {
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT c.id, c.name FROM communities c
		JOIN songbooks sb ON sb.owner_community_id = c.id
		JOIN songbook_entries e ON e.songbook_id = sb.id
		WHERE c.kind = 'coaching' AND e.variant_id = $1 ORDER BY c.name`, variantID)
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

// KeyTargets lists a coaching community's students and groups for a song:
// students first, by name, then groups.
func (st *Store) KeyTargets(ctx context.Context, community Community, variantID int64) ([]KeyTarget, error) {
	members, err := st.ListCommunityMembers(ctx, community.ID, false)
	if err != nil {
		return nil, err
	}
	var out []KeyTarget
	for _, m := range members {
		if m.IsStaff() {
			continue
		}
		t := KeyTarget{CommunityID: community.ID, CommunityName: community.Name, Kind: "student", PersonID: m.PersonID, Name: m.DisplayName}
		if t.SongbookID, err = st.FindStudentSongbook(ctx, community.ID, m.PersonID); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	sort.SliceStable(out, func(i, j int) bool { return foldName(out[i].Name) < foldName(out[j].Name) })
	groups, err := st.ListCoachingGroups(ctx, community.ID, 0)
	if err != nil {
		return nil, err
	}
	for _, g := range groups {
		out = append(out, KeyTarget{CommunityID: community.ID, CommunityName: community.Name, Kind: "group", GroupID: g.ID, Name: g.Name, SongbookID: g.SongbookID})
	}
	for i := range out {
		if out[i].SongbookID == 0 {
			continue
		}
		err := st.db.QueryRow(ctx, `SELECT semitones FROM songbook_entries WHERE songbook_id = $1 AND variant_id = $2`, out[i].SongbookID, variantID).Scan(&out[i].Semitones)
		switch {
		case err == nil:
			out[i].InSongbook = true
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
	}
	return out, nil
}

// StudentKeyOf is the key a student was given for a song, in the first
// of their coaching songbooks that has it: ok false when none does.
func (st *Store) StudentKeyOf(ctx context.Context, personID, variantID int64) (songbookID int64, semitones int, ok bool, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT sb.id, e.semitones FROM songbooks sb
		JOIN songbook_entries e ON e.songbook_id = sb.id
		WHERE sb.kind = 'student' AND sb.student_person_id = $1 AND e.variant_id = $2
		ORDER BY sb.id LIMIT 1`, personID, variantID).Scan(&songbookID, &semitones)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	return songbookID, semitones, err == nil, err
}

// SongBody is a song's text.
func (st *Store) SongBody(ctx context.Context, variantID int64) (string, error) {
	var b string
	err := st.db.QueryRow(ctx, `SELECT body FROM songs WHERE id = $1`, variantID).Scan(&b)
	return b, err
}

// foldName is a name to sort by: lower case, accents off ("Léa" with
// "Lea", before "Lucas").
// FoldName is a name to sort people by: accents and case left out.
func FoldName(s string) string { return foldName(s) }

func foldName(s string) string {
	var b strings.Builder
	for _, r := range norm.NFD.String(strings.ToLower(s)) {
		if !unicode.Is(unicode.Mn, r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}
