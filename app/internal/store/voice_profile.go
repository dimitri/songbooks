package store

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// A coaching student's voice type, as the coach assesses it (migration
// 0108, docs/coaching.md "Voice types"): a classical type or none yet, a
// few words, the comfortable part of the range (the tessitura; the range
// itself is the registers', StudentRanges). The student reads it all,
// its history too (migration 0109). Every change is kept.

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
	VoiceType string
	// Leaning: the type is the coach's working opinion ("leaning
	// baritone"), not her settled one.
	Leaning                     bool
	Assessment                  string
	TessituraLow, TessituraHigh *int
	// Timbre: "bright", "warm", "dark", or "" (not said).
	Timbre string
	// The voice's two passages (register breaks), MIDI notes, nil when
	// not noted.
	PassaggioLow, PassaggioHigh *int
	UpdatedAt                   *time.Time
	// LabelAt: when the type was last changed (its date beside it).
	LabelAt *time.Time
}

// VoiceAssessment is what the coach saves: the label, the comfortable
// zone, the descriptors, and her words.
type VoiceAssessment struct {
	VoiceType                   string
	Leaning                     bool
	TessituraLow, TessituraHigh *int
	Timbre                      string
	PassaggioLow, PassaggioHigh *int
	Note                        string
}

// Timbres are the timbre words a coach picks from.
var Timbres = []string{"bright", "warm", "dark"}

// labelText is a type as the history keeps it: "~baritone" when leaning.
func labelText(t string, leaning bool) string {
	if t != "" && leaning {
		return "~" + t
	}
	return t
}

// ParseLabelText is a history's type back: the type, and whether leaning.
func ParseLabelText(s string) (string, bool) {
	if strings.HasPrefix(s, "~") {
		return s[1:], true
	}
	return s, false
}

// VoiceChange is one change of the history.
type VoiceChange struct {
	ID        int64
	ChangedAt time.Time
	ChangedBy string // who, by name ("" when they're gone)
	Field     string // "voice_type", "tessitura", "timbre", "passaggi", "assessment", "chest", "mixed", "head"
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
		SELECT voice_type, voice_type_leaning, assessment, tessitura_low_midi, tessitura_high_midi,
		       timbre, passaggio_low_midi, passaggio_high_midi, updated_at,
		       (SELECT max(changed_at) FROM student_voice_history h
		        WHERE h.community_id = $1 AND h.person_id = $2 AND h.field = 'voice_type')
		FROM student_voice_profiles WHERE community_id = $1 AND person_id = $2`, communityID, personID).
		Scan(&p.VoiceType, &p.Leaning, &p.Assessment, &p.TessituraLow, &p.TessituraHigh,
			&p.Timbre, &p.PassaggioLow, &p.PassaggioHigh, &p.UpdatedAt, &p.LabelAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return VoiceProfile{}, nil
	}
	return p, err
}

// CommunityVoiceProfiles is every student's voice profile in a community,
// by person (those the coach noted).
func (st *Store) CommunityVoiceProfiles(ctx context.Context, communityID int64) (map[int64]VoiceProfile, error) {
	rows, err := st.db.Query(ctx, `
		SELECT person_id, voice_type, voice_type_leaning, tessitura_low_midi, tessitura_high_midi, timbre
		FROM student_voice_profiles WHERE community_id = $1`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]VoiceProfile{}
	for rows.Next() {
		var id int64
		var p VoiceProfile
		if err := rows.Scan(&id, &p.VoiceType, &p.Leaning, &p.TessituraLow, &p.TessituraHigh, &p.Timbre); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// SetStudentVoiceAssessment saves the coach's assessment: the label
// (firm or leaning), the comfortable zone and the passages (their ends in
// order), the timbre, and the note, which goes with every field it changes
// in the history (or alone, as "assessment", when only the words
// changed). Nothing changed, nothing kept.
func (st *Store) SetStudentVoiceAssessment(ctx context.Context, communityID, personID int64, a VoiceAssessment, byPersonID int64) error {
	if !ValidVoiceType(a.VoiceType) {
		return fmt.Errorf("invalid voice type %q", a.VoiceType)
	}
	if a.Timbre != "" && !slices.Contains(Timbres, a.Timbre) {
		return fmt.Errorf("invalid timbre %q", a.Timbre)
	}
	if a.VoiceType == "" {
		a.Leaning = false
	}
	order := func(lo, hi *int) (*int, *int) {
		if lo != nil && hi != nil && *lo > *hi {
			return hi, lo
		}
		return lo, hi
	}
	a.TessituraLow, a.TessituraHigh = order(a.TessituraLow, a.TessituraHigh)
	a.PassaggioLow, a.PassaggioHigh = order(a.PassaggioLow, a.PassaggioHigh)
	a.Note = strings.TrimSpace(a.Note)
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var old VoiceProfile
	err = tx.QueryRow(ctx, `
		SELECT voice_type, voice_type_leaning, assessment, tessitura_low_midi, tessitura_high_midi,
		       timbre, passaggio_low_midi, passaggio_high_midi
		FROM student_voice_profiles
		WHERE community_id = $1 AND person_id = $2 FOR UPDATE`, communityID, personID).
		Scan(&old.VoiceType, &old.Leaning, &old.Assessment, &old.TessituraLow, &old.TessituraHigh,
			&old.Timbre, &old.PassaggioLow, &old.PassaggioHigh)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return err
	}
	type change struct{ field, old, new string }
	var changes []change
	if o, n := labelText(old.VoiceType, old.Leaning), labelText(a.VoiceType, a.Leaning); o != n {
		changes = append(changes, change{"voice_type", o, n})
	}
	if o, n := rangeText(old.TessituraLow, old.TessituraHigh), rangeText(a.TessituraLow, a.TessituraHigh); o != n {
		changes = append(changes, change{"tessitura", o, n})
	}
	if old.Timbre != a.Timbre {
		changes = append(changes, change{"timbre", old.Timbre, a.Timbre})
	}
	if o, n := rangeText(old.PassaggioLow, old.PassaggioHigh), rangeText(a.PassaggioLow, a.PassaggioHigh); o != n {
		changes = append(changes, change{"passaggi", o, n})
	}
	if len(changes) == 0 && a.Note != old.Assessment {
		changes = append(changes, change{"assessment", old.Assessment, a.Note})
	}
	if len(changes) == 0 {
		return nil
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO student_voice_profiles (community_id, person_id, voice_type, voice_type_leaning, assessment,
			tessitura_low_midi, tessitura_high_midi, timbre, passaggio_low_midi, passaggio_high_midi, updated_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (community_id, person_id) DO UPDATE SET
			voice_type = EXCLUDED.voice_type, voice_type_leaning = EXCLUDED.voice_type_leaning, assessment = EXCLUDED.assessment,
			tessitura_low_midi = EXCLUDED.tessitura_low_midi, tessitura_high_midi = EXCLUDED.tessitura_high_midi,
			timbre = EXCLUDED.timbre, passaggio_low_midi = EXCLUDED.passaggio_low_midi, passaggio_high_midi = EXCLUDED.passaggio_high_midi,
			updated_by_person_id = EXCLUDED.updated_by_person_id, updated_at = now()`,
		communityID, personID, a.VoiceType, a.Leaning, a.Note, a.TessituraLow, a.TessituraHigh,
		a.Timbre, a.PassaggioLow, a.PassaggioHigh, nullID(byPersonID)); err != nil {
		return err
	}
	for _, c := range changes {
		if err := insertVoiceChange(ctx, tx, communityID, personID, byPersonID, c.field, c.old, c.new, a.Note); err != nil {
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
