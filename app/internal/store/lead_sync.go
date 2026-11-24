package store

import (
	"context"

	"app/internal/song"

	"github.com/jackc/pgx/v5"
)

// shiftLeadSync keeps a song's recordings in step with its "lead:" line
// (song.Song.Lead): the lead's bars are the song's bars 0…K-1, so a save
// changing K moves every bar after them by the difference. What's stored
// by bar number — the per-bar sync points of every recording of the song
// (gp_bar_sync_point: a Guitar Pro file's own sync, the Mixer's flags, an
// auto-align) — moves with them, in the save's transaction; a point that
// ends up before bar 0 (a lead taken off) keeps its negative bar, so
// putting the lead back brings it back. The section markers are times
// (song_audio_markers): the first section's stays where it is in the
// recording, the lead taking the time before it, so they don't move. It
// returns how many points moved.
func shiftLeadSync(ctx context.Context, tx pgx.Tx, variantID int64, oldBody, newBody string) (int64, error) {
	delta := song.Parse(newBody).LeadIn() - song.Parse(oldBody).LeadIn()
	if delta == 0 {
		return 0, nil
	}
	// In two steps, through a range no bar uses: a one-step shift would
	// trip the (recording, bar) key on the way (bar 1 moved onto bar 2
	// before bar 2 moved on).
	const far = 1000000
	tag, err := tx.Exec(ctx, `
		UPDATE gp_bar_sync_point SET bar_index = bar_index + $2 + $3
		WHERE song_audio_id IN (SELECT id FROM song_audio WHERE variant_id = $1)`, variantID, delta, far)
	if err != nil {
		return 0, err
	}
	if tag.RowsAffected() == 0 {
		return 0, nil
	}
	if _, err := tx.Exec(ctx, `
		UPDATE gp_bar_sync_point SET bar_index = bar_index - $2
		WHERE song_audio_id IN (SELECT id FROM song_audio WHERE variant_id = $1) AND bar_index >= $2 / 2`, variantID, far); err != nil {
		return 0, err
	}
	return tag.RowsAffected(), nil
}
