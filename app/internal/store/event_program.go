package store

import (
	"context"
	"errors"
	"sort"

	"github.com/jackc/pgx/v5"
)

// A coaching community's repertoire and its events' programs (migration
// 0082, docs/coaching.md): every song its students and groups have, each
// in their key, and, for an open mic or the end-of-year concert, the
// songs in running order, each in its key, sung by a lead and backing
// vocals.

// RepertoireSinger is a student or a group who has a song, in their key.
type RepertoireSinger struct {
	Kind      string // "student" or "group"
	PersonID  int64  // a student's
	GroupID   int64  // a group's
	Name      string
	Semitones int
}

// RepertoireSong is a song of a coaching community's students' and
// groups' songbooks, with who has it.
type RepertoireSong struct {
	VariantID int64
	Title     string
	Artist    string
	Key       string // its key: header, "" when none
	Singers   []RepertoireSinger
}

// CommunityRepertoire is every song a coaching community's students and
// groups have, by title, students first then groups in each.
func (st *Store) CommunityRepertoire(ctx context.Context, communityID int64) ([]RepertoireSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.title, s.artist, coalesce(substring(s.body from '(?n)^key:[ \t]*([^ \t\r\n]+)'), ''),
		       sb.kind, coalesce(sb.student_person_id, 0), coalesce(p.display_name, ''), coalesce(g.id, 0), coalesce(g.name, ''), e.semitones
		FROM songbooks sb
		JOIN songbook_entries e ON e.songbook_id = sb.id
		JOIN songs s ON s.id = e.variant_id
		LEFT JOIN people p ON p.id = sb.student_person_id
		LEFT JOIN coaching_groups g ON g.songbook_id = sb.id
		WHERE sb.owner_community_id = $1 AND sb.kind IN ('student', 'group')
		  AND (sb.kind = 'group' OR p.id IS NOT NULL)
		ORDER BY lower(s.title), s.id`, communityID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []RepertoireSong
	at := map[int64]int{}
	for rows.Next() {
		var s RepertoireSong
		var g RepertoireSinger
		var pName, gName string
		if err := rows.Scan(&s.VariantID, &s.Title, &s.Artist, &s.Key, &g.Kind, &g.PersonID, &pName, &g.GroupID, &gName, &g.Semitones); err != nil {
			return nil, err
		}
		if g.Kind == SongbookKindGroup {
			g.Name, g.PersonID = gName, 0
		} else {
			g.Name, g.GroupID = pName, 0
		}
		i, ok := at[s.VariantID]
		if !ok {
			i = len(out)
			at[s.VariantID] = i
			out = append(out, s)
		}
		out[i].Singers = append(out[i].Singers, g)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range out {
		sort.SliceStable(out[i].Singers, func(a, b int) bool {
			sa, sb := out[i].Singers[a], out[i].Singers[b]
			if sa.Kind != sb.Kind {
				return sa.Kind == "student"
			}
			return foldName(sa.Name) < foldName(sb.Name)
		})
	}
	return out, nil
}

// ProgramSinger is someone singing a program song.
type ProgramSinger struct {
	PersonID int64
	Name     string
}

// ProgramSong is a song of an event's program, in running order.
type ProgramSong struct {
	ID        int64
	Position  int
	VariantID int64
	Title     string
	Artist    string
	Key       string // its key: header
	Semitones int
	Lead      *ProgramSinger
	Backing   []ProgramSinger
}

// ListEventProgram is an event's program, in running order.
func (st *Store) ListEventProgram(ctx context.Context, eventID int64) ([]ProgramSong, error) {
	rows, err := st.db.Query(ctx, `
		SELECT ep.id, ep.position, s.id, s.title, s.artist, coalesce(substring(s.body from '(?n)^key:[ \t]*([^ \t\r\n]+)'), ''), ep.semitones
		FROM event_program ep JOIN songs s ON s.id = ep.variant_id
		WHERE ep.event_id = $1 ORDER BY ep.position, ep.id`, eventID)
	if err != nil {
		return nil, err
	}
	var out []ProgramSong
	at := map[int64]int{}
	for rows.Next() {
		var p ProgramSong
		if err := rows.Scan(&p.ID, &p.Position, &p.VariantID, &p.Title, &p.Artist, &p.Key, &p.Semitones); err != nil {
			rows.Close()
			return nil, err
		}
		at[p.ID] = len(out)
		out = append(out, p)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	srows, err := st.db.Query(ctx, `
		SELECT ps.program_id, ps.person_id, p.display_name, ps.role
		FROM event_program_singers ps
		JOIN event_program ep ON ep.id = ps.program_id
		JOIN people p ON p.id = ps.person_id
		WHERE ep.event_id = $1 ORDER BY lower(p.display_name)`, eventID)
	if err != nil {
		return nil, err
	}
	defer srows.Close()
	for srows.Next() {
		var pid int64
		var s ProgramSinger
		var role string
		if err := srows.Scan(&pid, &s.PersonID, &s.Name, &role); err != nil {
			return nil, err
		}
		i, ok := at[pid]
		if !ok {
			continue
		}
		if role == "lead" {
			lead := s
			out[i].Lead = &lead
		} else {
			out[i].Backing = append(out[i].Backing, s)
		}
	}
	for i := range out {
		sort.SliceStable(out[i].Backing, func(a, b int) bool { return foldName(out[i].Backing[a].Name) < foldName(out[i].Backing[b].Name) })
	}
	return out, srows.Err()
}

// AddProgramSongs appends songs to an event's program, in the order
// given, skipping those already in it.
func (st *Store) AddProgramSongs(ctx context.Context, eventID int64, variantIDs []int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var next int
	if err := tx.QueryRow(ctx, `SELECT coalesce(max(position), 0) + 1 FROM event_program WHERE event_id = $1`, eventID).Scan(&next); err != nil {
		return err
	}
	for _, vid := range variantIDs {
		tag, err := tx.Exec(ctx, `INSERT INTO event_program (event_id, position, variant_id) VALUES ($1, $2, $3) ON CONFLICT (event_id, variant_id) DO NOTHING`, eventID, next, vid)
		if err != nil {
			return err
		}
		if tag.RowsAffected() > 0 {
			next++
		}
	}
	return tx.Commit(ctx)
}

// ErrNotInProgram is a program song that isn't the event's.
var ErrNotInProgram = errors.New("not in this event's program")

// ProgramSongOf is one of an event's program songs.
func (st *Store) ProgramSongOf(ctx context.Context, eventID, programID int64) (variantID int64, err error) {
	err = st.db.QueryRow(ctx, `SELECT variant_id FROM event_program WHERE id = $1 AND event_id = $2`, programID, eventID).Scan(&variantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotInProgram
	}
	return variantID, err
}

// RemoveProgramSong takes a song out of an event's program.
func (st *Store) RemoveProgramSong(ctx context.Context, eventID, programID int64) error {
	_, err := st.db.Exec(ctx, `DELETE FROM event_program WHERE id = $1 AND event_id = $2`, programID, eventID)
	return err
}

// MoveProgramSong moves a song one place up (delta -1) or down (+1) in
// its event's running order.
func (st *Store) MoveProgramSong(ctx context.Context, eventID, programID int64, delta int) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	rows, err := tx.Query(ctx, `SELECT id FROM event_program WHERE event_id = $1 ORDER BY position, id`, eventID)
	if err != nil {
		return err
	}
	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return err
		}
		ids = append(ids, id)
	}
	rows.Close()
	at := -1
	for i, id := range ids {
		if id == programID {
			at = i
		}
	}
	if at < 0 {
		return ErrNotInProgram
	}
	to := at + delta
	if to < 0 || to >= len(ids) {
		return nil
	}
	ids[at], ids[to] = ids[to], ids[at]
	for i, id := range ids {
		if _, err := tx.Exec(ctx, `UPDATE event_program SET position = $1 WHERE id = $2`, i+1, id); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetProgramKey sets the key a program song is sung in.
func (st *Store) SetProgramKey(ctx context.Context, eventID, programID int64, semitones int) error {
	_, err := st.db.Exec(ctx, `UPDATE event_program SET semitones = $3 WHERE id = $1 AND event_id = $2`, programID, eventID, semitones)
	return err
}

// SetProgramLead makes personID a program song's lead singer (0: none),
// in place of the one before; a backing singer made lead stops backing.
func (st *Store) SetProgramLead(ctx context.Context, programID, personID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM event_program_singers WHERE program_id = $1 AND (role = 'lead' OR person_id = $2)`, programID, personID); err != nil {
		return err
	}
	if personID != 0 {
		if _, err := tx.Exec(ctx, `INSERT INTO event_program_singers (program_id, person_id, role) VALUES ($1, $2, 'lead')`, programID, personID); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// SetProgramBacking adds or takes off a backing singer; the lead stays
// the lead.
func (st *Store) SetProgramBacking(ctx context.Context, programID, personID int64, on bool) error {
	if on {
		_, err := st.db.Exec(ctx, `INSERT INTO event_program_singers (program_id, person_id, role) VALUES ($1, $2, 'backing') ON CONFLICT (program_id, person_id) DO NOTHING`, programID, personID)
		return err
	}
	_, err := st.db.Exec(ctx, `DELETE FROM event_program_singers WHERE program_id = $1 AND person_id = $2 AND role = 'backing'`, programID, personID)
	return err
}

// ProgramKeyOf is the key a song is sung in at an event: ok false when
// it isn't in the event's program.
func (st *Store) ProgramKeyOf(ctx context.Context, eventID, variantID int64) (semitones int, ok bool, err error) {
	err = st.db.QueryRow(ctx, `SELECT semitones FROM event_program WHERE event_id = $1 AND variant_id = $2`, eventID, variantID).Scan(&semitones)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, nil
	}
	return semitones, err == nil, err
}

// ProgramNeighbors are the songs before and after variantID in an
// event's running order (0 when none).
func (st *Store) ProgramNeighbors(ctx context.Context, eventID, variantID int64) (prev, next int64, err error) {
	err = st.db.QueryRow(ctx, `
		WITH o AS (SELECT variant_id, lag(variant_id) OVER w AS p, lead(variant_id) OVER w AS n
		           FROM event_program WHERE event_id = $1 WINDOW w AS (ORDER BY position, id))
		SELECT coalesce(p, 0), coalesce(n, 0) FROM o WHERE variant_id = $2`, eventID, variantID).Scan(&prev, &next)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, nil
	}
	return prev, next, err
}
