package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// GetMixerPrefs returns a person's saved Mixer settings for a song, or an
// empty JSON object when there are none yet.
func (st *Store) GetMixerPrefs(ctx context.Context, personID, variantID int64) (json.RawMessage, error) {
	var raw []byte
	err := st.db.QueryRow(ctx, `SELECT prefs FROM mixer_prefs WHERE person_id = $1 AND variant_id = $2`, personID, variantID).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return json.RawMessage(`{}`), nil
	}
	if err != nil {
		return nil, err
	}
	return raw, nil
}

// SetMixerPrefs replaces a person's Mixer settings for a song. prefs must
// already be a validated JSON object.
func (st *Store) SetMixerPrefs(ctx context.Context, personID, variantID int64, prefs json.RawMessage) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO mixer_prefs (person_id, variant_id, prefs) VALUES ($1, $2, $3)
		ON CONFLICT (person_id, variant_id) DO UPDATE SET prefs = EXCLUDED.prefs, updated_at = now()`,
		personID, variantID, []byte(prefs))
	return err
}
