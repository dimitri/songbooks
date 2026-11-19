package store

import (
	"context"
	"errors"
	"fmt"

	"app/internal/instruments"

	"github.com/jackc/pgx/v5"
)

// "Je joue": the instruments a person plays, as the song player knows them
// (migration 0100). The player shows one level per part they play, from
// the recording's parts: 0 takes it out of the recording, 100 leaves it.
//
// Which parts, for a song opened in a context (a band, a community, or
// none, "elsewhere"), the first that is set of:
//
//  1. their choice for that song in that context;
//  2. their choice for that context, all its songs;
//  3. their profile's instruments (people.instruments, free text,
//     normalised by internal/instruments);
//  4. the voice: nobody said, and this is a singing app.
//
// So: guitar with the band Mercy, voice in Chloé's coaching community,
// and voice and piano for that one song.

// Where a person's parts came from.
const (
	PartsFromSong    = "song"    // their choice for this song, here
	PartsFromContext = "context" // their choice for this context
	PartsFromProfile = "profile" // their profile's instruments
	PartsFromDefault = "default" // nothing said: the voice
)

// PartsContext is where a song is played: a band, a community, or none
// (Kind "", ID 0).
type PartsContext struct {
	Kind string // "", "band" or "community"
	ID   int64
}

func (c PartsContext) valid() bool {
	switch c.Kind {
	case "":
		return c.ID == 0
	case "band", "community":
		return c.ID > 0
	}
	return false
}

// nullableID is a context's id as stored: NULL for none.
func (c PartsContext) nullableID() *int64 {
	if c.Kind == "" {
		return nil
	}
	return &c.ID
}

// PlayerParts is a person's parts for a song in a context, and where they
// came from (PartsFrom*).
type PlayerParts struct {
	Instruments []string
	From        string
}

// ErrBadParts: an instrument that isn't one of internal/instruments', too
// many of them, or a context that isn't one.
var ErrBadParts = errors.New("bad player parts")

// maxParts: nobody plays more at once; the player has one slider each.
const maxParts = 6

// cleanParts checks and orders a list of instrument IDs.
func cleanParts(ids []string) ([]string, error) {
	out := []string{}
	seen := map[string]bool{}
	for _, id := range ids {
		if !instruments.Valid(id) {
			return nil, fmt.Errorf("%w: %q", ErrBadParts, id)
		}
		if !seen[id] {
			seen[id] = true
			out = append(out, id)
		}
	}
	if len(out) > maxParts {
		return nil, fmt.Errorf("%w: %d instruments", ErrBadParts, len(out))
	}
	instruments.Sort(out)
	return out, nil
}

// SetPlayerParts stores a person's parts for a context: for one song
// (variantID), or for all its songs (variantID 0). An empty list is
// "nothing: I listen".
func (st *Store) SetPlayerParts(ctx context.Context, personID int64, c PartsContext, variantID int64, ids []string) error {
	if !c.valid() {
		return fmt.Errorf("%w: context %q %d", ErrBadParts, c.Kind, c.ID)
	}
	parts, err := cleanParts(ids)
	if err != nil {
		return err
	}
	var variant *int64
	if variantID != 0 {
		variant = &variantID
	}
	_, err = st.db.Exec(ctx, `
		INSERT INTO player_parts (person_id, context_kind, context_id, variant_id, instruments)
		VALUES ($1, $2, $3, $4, $5)
		ON CONFLICT (person_id, context_kind, context_id, variant_id)
		DO UPDATE SET instruments = EXCLUDED.instruments, updated_at = now()`,
		personID, c.Kind, c.nullableID(), variant, parts)
	return err
}

// ClearPlayerParts forgets a person's choice for a context's song
// (variantID), or for the context itself (variantID 0): what's under it
// applies again.
func (st *Store) ClearPlayerParts(ctx context.Context, personID int64, c PartsContext, variantID int64) error {
	if !c.valid() {
		return fmt.Errorf("%w: context %q %d", ErrBadParts, c.Kind, c.ID)
	}
	var variant *int64
	if variantID != 0 {
		variant = &variantID
	}
	_, err := st.db.Exec(ctx, `
		DELETE FROM player_parts
		WHERE person_id = $1 AND context_kind = $2
		  AND context_id IS NOT DISTINCT FROM $3 AND variant_id IS NOT DISTINCT FROM $4`,
		personID, c.Kind, c.nullableID(), variant)
	return err
}

// ResolvePlayerParts is a person's parts for a song in a context (see the
// order above).
func (st *Store) ResolvePlayerParts(ctx context.Context, personID int64, c PartsContext, variantID int64) (PlayerParts, error) {
	if !c.valid() {
		return PlayerParts{}, fmt.Errorf("%w: context %q %d", ErrBadParts, c.Kind, c.ID)
	}
	var ids []string
	var forSong bool
	err := st.db.QueryRow(ctx, `
		SELECT instruments, variant_id IS NOT NULL FROM player_parts
		WHERE person_id = $1 AND context_kind = $2 AND context_id IS NOT DISTINCT FROM $3
		  AND (variant_id = $4 OR variant_id IS NULL)
		ORDER BY variant_id IS NULL
		LIMIT 1`, personID, c.Kind, c.nullableID(), variantID).Scan(&ids, &forSong)
	switch {
	case err == nil:
		// Only the IDs the app still knows (the list may lose one).
		out := []string{}
		for _, id := range ids {
			if instruments.Valid(id) {
				out = append(out, id)
			}
		}
		from := PartsFromContext
		if forSong {
			from = PartsFromSong
		}
		return PlayerParts{Instruments: out, From: from}, nil
	case !errors.Is(err, pgx.ErrNoRows):
		return PlayerParts{}, err
	}
	var profile []string
	if err := st.db.QueryRow(ctx, `SELECT instruments FROM people WHERE id = $1`, personID).Scan(&profile); err != nil {
		return PlayerParts{}, err
	}
	if ids := instruments.NormalizeAll(profile); len(ids) > 0 {
		instruments.Sort(ids)
		if len(ids) > maxParts {
			ids = ids[:maxParts]
		}
		return PlayerParts{Instruments: ids, From: PartsFromProfile}, nil
	}
	return PlayerParts{Instruments: []string{"voice"}, From: PartsFromDefault}, nil
}
