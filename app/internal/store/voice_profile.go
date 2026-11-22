package store

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A coaching student's voice type, as the coach assesses it (migration
// 0108, docs/coaching.md "Voice types"): a classical type or none yet, a
// few words, the comfortable part of the range (the tessitura; the range
// itself is the registers', StudentRanges), and whether the student
// reads the assessment and its history. Every change is kept.

// VoiceTypes are the types a coach picks from, high to low; "" is none
// yet (many coaches of pop singers keep no label, or not yet).
var VoiceTypes = []string{"soprano", "mezzo", "contralto", "countertenor", "tenor", "baritone", "bass"}

// ValidVoiceType reports whether t is one of VoiceTypes, or "".
func ValidVoiceType(t string) bool {
	if t == "" {
		return true
	}
	for _, v := range VoiceTypes {
		if v == t {
			return true
		}
	}
	return false
}

// VoiceProfile is a student's voice type and the coach's assessment.
type VoiceProfile struct {
	VoiceType                   string
	Assessment                  string
	TessituraLow, TessituraHigh *int
	// Shared: the student reads the assessment and its history (they
	// always see their type and their range).
	Shared    bool
	UpdatedAt *time.Time
}

// VoiceChange is one change of the history.
type VoiceChange struct {
	ID        int64
	ChangedAt time.Time
	ChangedBy string // who, by name ("" when they're gone)
	Field     string // "voice_type", "tessitura", "assessment", "chest", "mixed", "head"
	Old, New  string
	Note      string
}

// rangeText is a range as the history keeps it, "low-high" in MIDI notes,
// "" for none.
func rangeText(low, high *int) string {
	if low == nil && high == nil {
		return ""
	}
	n := func(p *int) string {
		if p == nil {
			return ""
		}
		return strconv.Itoa(*p)
	}
	return n(low) + "-" + n(high)
}

// ParseRangeText is a history's range back: its two ends, nil when not
// noted.
func ParseRangeText(s string) (low, high *int) {
	a, b, ok := strings.Cut(s, "-")
	if !ok {
		return nil, nil
	}
	if n, err := strconv.Atoi(a); err == nil {
		low = &n
	}
	if n, err := strconv.Atoi(b); err == nil {
		high = &n
	}
	return low, high
}

// StudentVoiceProfile is a student's voice type and assessment, the zero
// value when the coach has noted none.
func (st *Store) StudentVoiceProfile(ctx context.Context, communityID, personID int64) (VoiceProfile, error) {
	var p VoiceProfile
	err := st.db.QueryRow(ctx, `
		SELECT voice_type, assessment, tessitura_low_midi, tessitura_high_midi, shared, updated_at
		FROM student_voice_profiles WHERE community_id = $1 AND person_id = $2`, communityID, personID).
		Scan(&p.VoiceType, &p.Assessment, &p.TessituraLow, &p.TessituraHigh, &p.Shared, &p.UpdatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return VoiceProfile{}, nil
	}
	return p, err
}

// CommunityVoiceTypes is every student's voice type in a community, by
// person (those noted only).
func (st *Store) CommunityVoiceTypes(ctx context.Context, communityID int64) (map[int64]string, error) {
	rows, err := st.db.Query(ctx, `SELECT person_id, voice_type FROM student_voice_profiles WHERE community_id = $1 AND voice_type <> ''`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]string{}
	for rows.Next() {
		var id int64
		var t string
		if err := rows.Scan(&id, &t); err != nil {
			return nil, err
		}
		out[id] = t
	}
	return out, rows.Err()
}

// SetStudentVoiceAssessment saves the coach's assessment: the type, the
// tessitura (its ends in order), and the note, which goes with every
// field it changes in the history (or alone, as "assessment", when only
// the words changed). Nothing changed, nothing kept.
func (st *Store) SetStudentVoiceAssessment(ctx context.Context, communityID, personID int64, voiceType string, tessLow, tessHigh *int, note string, byPersonID int64) error {
	if !ValidVoiceType(voiceType) {
		return fmt.Errorf("invalid voice type %q", voiceType)
	}
	if tessLow != nil && tessHigh != nil && *tessLow > *tessHigh {
		tessLow, tessHigh = tessHigh, tessLow
	}
	note = strings.TrimSpace(note)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var old VoiceProfile
	err = tx.QueryRow(ctx, `
		SELECT voice_type, assessment, tessitura_low_midi, tessitura_high_midi FROM student_voice_profiles
		WHERE community_id = $1 AND person_id = $2 FOR UPDATE`, communityID, personID).
		Scan(&old.VoiceType, &old.Assessment, &old.TessituraLow, &old.TessituraHigh)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	type change struct{ field, old, new string }
	var changes []change
	if old.VoiceType != voiceType {
		changes = append(changes, change{"voice_type", old.VoiceType, voiceType})
	}
	if o, n := rangeText(old.TessituraLow, old.TessituraHigh), rangeText(tessLow, tessHigh); o != n {
		changes = append(changes, change{"tessitura", o, n})
	}
	if len(changes) == 0 && note != old.Assessment {
		changes = append(changes, change{"assessment", old.Assessment, note})
	}
	if len(changes) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO student_voice_profiles (community_id, person_id, voice_type, assessment, tessitura_low_midi, tessitura_high_midi, updated_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
		ON CONFLICT (community_id, person_id) DO UPDATE SET
			voice_type = EXCLUDED.voice_type, assessment = EXCLUDED.assessment,
			tessitura_low_midi = EXCLUDED.tessitura_low_midi, tessitura_high_midi = EXCLUDED.tessitura_high_midi,
			updated_by_person_id = EXCLUDED.updated_by_person_id, updated_at = now()`,
		communityID, personID, voiceType, note, tessLow, tessHigh, nullID(byPersonID)); err != nil {
		return err
	}
	for _, c := range changes {
		if err := insertVoiceChange(ctx, tx, communityID, personID, byPersonID, c.field, c.old, c.new, note); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

func insertVoiceChange(ctx context.Context, q interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}, communityID, personID, byPersonID int64, field, old, new, note string) error {
	_, err := q.Exec(ctx, `
		INSERT INTO student_voice_history (community_id, person_id, changed_by_person_id, field, old_value, new_value, note)
		VALUES ($1, $2, $3, $4, $5, $6, $7)`, communityID, personID, nullID(byPersonID), field, old, new, note)
	return err
}

// SetStudentVoiceShared says whether the student reads the assessment and
// its history.
func (st *Store) SetStudentVoiceShared(ctx context.Context, communityID, personID int64, shared bool, byPersonID int64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO student_voice_profiles (community_id, person_id, shared, updated_by_person_id) VALUES ($1, $2, $3, $4)
		ON CONFLICT (community_id, person_id) DO UPDATE SET shared = EXCLUDED.shared`,
		communityID, personID, shared, nullID(byPersonID))
	return err
}

// StudentVoiceHistory is a student's voice changes, the latest first.
func (st *Store) StudentVoiceHistory(ctx context.Context, communityID, personID int64) ([]VoiceChange, error) {
	rows, err := st.db.Query(ctx, `
		SELECT h.id, h.changed_at, coalesce(p.display_name, ''), h.field, h.old_value, h.new_value, h.note
		FROM student_voice_history h LEFT JOIN people p ON p.id = h.changed_by_person_id
		WHERE h.community_id = $1 AND h.person_id = $2
		ORDER BY h.changed_at DESC, h.id DESC`, communityID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []VoiceChange
	for rows.Next() {
		var c VoiceChange
		if err := rows.Scan(&c.ID, &c.ChangedAt, &c.ChangedBy, &c.Field, &c.Old, &c.New, &c.Note); err != nil {
			return nil, err
		}
		out = append(out, c)
	}
	return out, rows.Err()
}
