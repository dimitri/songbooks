package store

import (
	"context"

	"app/internal/song"

	"github.com/jackc/pgx/v5"
)

// keepEntryKeys keeps the keys songbooks hold a song in where they were
// when a save moves the song's own key (its key: header, or the chart
// transposed in place: song.KeyShift). A key is kept as semitones from the
// song's own (songbook_entries.semitones, event_program.semitones), so a
// song moved from G to A would take a student's "F (−2)" to G: each one
// is moved back by as much, in the save's transaction, wrapped by an
// octave when it would leave ±12.
//
// Which keys: every key someone chose — any entry not in the song's own
// key, in any songbook (a student's, a group's, a person's, a band's), a
// student's and a group's songbook whatever their key (the coach gave it
// to them in that key, even "original"), and every event program's song.
// An entry of any other songbook in the song's own key stays there: it
// follows the song (the repertoire's, the owner's own songbook, a link
// "in the original key"). It returns how many keys moved.
func keepEntryKeys(ctx context.Context, tx pgx.Tx, variantID int64, oldBody, newBody string) (int64, error) {
	d := song.KeyShift(oldBody, newBody)
	if d == 0 {
		return 0, nil
	}
	const back = `CASE WHEN semitones - $2 > 12 THEN semitones - $2 - 12
	                   WHEN semitones - $2 < -12 THEN semitones - $2 + 12
	                   ELSE semitones - $2 END`
	tag, err := tx.Exec(ctx, `
		UPDATE songbook_entries se SET semitones = `+back+`
		WHERE se.variant_id = $1
		  AND (se.semitones <> 0 OR EXISTS (SELECT 1 FROM songbooks sb WHERE sb.id = se.songbook_id AND sb.kind IN ('student', 'group')))`,
		variantID, d)
	if err != nil {
		return 0, err
	}
	n := tag.RowsAffected()
	tag, err = tx.Exec(ctx, `UPDATE event_program SET semitones = `+back+` WHERE variant_id = $1`, variantID, d)
	if err != nil {
		return 0, err
	}
	return n + tag.RowsAffected(), nil
}
