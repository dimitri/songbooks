package store

import (
	"context"
	"strings"
	"unicode/utf8"
)

// MaxClicktrackNote caps a click-track note: a line or two read at a
// glance mid-song, not a page.
const MaxClicktrackNote = 300

// ListClicktrackNotes returns setlistID's click-track notes, keyed by the
// song (variant) they belong to.
func (st *Store) ListClicktrackNotes(ctx context.Context, setlistID int64) (map[int64]string, error) {
	rows, err := st.db.Query(ctx, `
		SELECT variant_id, note FROM setlist_clicktrack_notes WHERE setlist_id = $1`, setlistID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	notes := map[int64]string{}
	for rows.Next() {
		var id int64
		var note string
		if err := rows.Scan(&id, &note); err != nil {
			return nil, err
		}
		notes[id] = note
	}
	return notes, rows.Err()
}

// SetClicktrackNote saves the click-track note for one song of a set
// list and returns the note as stored (trimmed, cut to
// MaxClicktrackNote characters). An empty note removes it. The caller
// checks the person may edit the set list and that the song is in it.
func (st *Store) SetClicktrackNote(ctx context.Context, setlistID, variantID int64, note string, actorPersonID int64) (string, error) {
	note = strings.TrimSpace(note)
	if utf8.RuneCountInString(note) > MaxClicktrackNote {
		note = strings.TrimSpace(string([]rune(note)[:MaxClicktrackNote]))
	}
	if note == "" {
		_, err := st.db.Exec(ctx, `
			DELETE FROM setlist_clicktrack_notes WHERE setlist_id = $1 AND variant_id = $2`,
			setlistID, variantID)
		return "", err
	}
	var actor *int64
	if actorPersonID != 0 {
		actor = &actorPersonID
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO setlist_clicktrack_notes (setlist_id, variant_id, note, updated_by)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (setlist_id, variant_id)
		DO UPDATE SET note = EXCLUDED.note, updated_by = EXCLUDED.updated_by, updated_at = now()`,
		setlistID, variantID, note, actor)
	if err != nil {
		return "", err
	}
	return note, nil
}
