package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// AudioCategory is one of exactly three reference-audio kinds a song can
// have — "a personal choice of reference version (yt video, band
// recording, gp backing track)," not an open-ended list of takes.
// AudioGP is the one case that's NOT a manually-tapped personal choice:
// it comes from a Guitar Pro import's own embedded backing track,
// already bar-synced (see GPBarSyncPoint) — the player defaults to it
// over the other two when present, since it's the only one guaranteed
// to line up with the chart's own bars.
type AudioCategory string

const (
	AudioYouTube AudioCategory = "youtube"
	AudioBand    AudioCategory = "band"
	AudioGP      AudioCategory = "gp"
)

// SongAudio is one reference-recording MP3 for a song variant, in one of
// the two categories above — see app/docs/mp3-reference-audio.md for why
// this is stored as bytea in Postgres rather than a filesystem volume or
// object storage.
type SongAudio struct {
	ID                int64
	VariantID         int64
	Category          AudioCategory
	ContentType       string
	ByteSize          int64
	SHA256            string
	SourceURL         string
	CreatedAt         time.Time
	CreatedByPersonID *int64
	// Kind: "full", "backing" or "" (unknown) — see migration 0063.
	Kind string
}

// AudioMarker ties a playback timestamp to one of the song's own sections
// (by index into its parsed Section list — see song_audio_markers' own
// migration comment for why there's no separate per-section id).
type AudioMarker struct {
	ID           int64
	SectionIndex int
	TimeSeconds  float64
	// Origin is SyncAuto when a program placed it and nobody has checked
	// it yet, SyncSet when a person placed or confirmed it (migration
	// 0094). "" means SyncSet when writing.
	Origin string
}

// A sync's origin: estimated by a program, or set by a person.
const (
	SyncAuto = "auto"
	SyncSet  = "set"
)

func syncOrigin(o string) string {
	if o == SyncAuto {
		return SyncAuto
	}
	return SyncSet
}

// ListSongAudio loads every category a variant has reference audio for
// (0, 1, or 2 rows — see AudioCategory's own doc comment), WITHOUT the
// bytes themselves, for pages that just need to know "what's available."
func (st *Store) ListSongAudio(ctx context.Context, variantID int64) ([]SongAudio, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, variant_id, category, content_type, byte_size, sha256, source_url, created_at, created_by_person_id, content_kind
		FROM song_audio WHERE variant_id = $1 ORDER BY category`, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SongAudio
	for rows.Next() {
		var a SongAudio
		if err := rows.Scan(&a.ID, &a.VariantID, &a.Category, &a.ContentType, &a.ByteSize, &a.SHA256, &a.SourceURL, &a.CreatedAt, &a.CreatedByPersonID, &a.Kind); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// GetSongAudioMeta loads one category's audio row for a variant, without
// its bytes.
func (st *Store) GetSongAudioMeta(ctx context.Context, variantID int64, category AudioCategory) (*SongAudio, error) {
	var a SongAudio
	err := st.db.QueryRow(ctx, `
		SELECT id, variant_id, category, content_type, byte_size, sha256, source_url, created_at, created_by_person_id
		FROM song_audio WHERE variant_id = $1 AND category = $2`, variantID, category).
		Scan(&a.ID, &a.VariantID, &a.Category, &a.ContentType, &a.ByteSize, &a.SHA256, &a.SourceURL, &a.CreatedAt, &a.CreatedByPersonID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	return &a, nil
}

// GetSongAudioData loads one audio row's actual bytes, by its own id (not
// variant_id/category) — used by the serving handler, which already
// resolved the id from GetSongAudioMeta and wants a second, cheap
// round-trip rather than ever holding every variant's audio bytes in
// memory at once.
func (st *Store) GetSongAudioData(ctx context.Context, audioID int64) ([]byte, string, error) {
	var data []byte
	var contentType string
	err := st.db.QueryRow(ctx, `SELECT data, content_type FROM song_audio WHERE id = $1`, audioID).Scan(&data, &contentType)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, "", ErrNotFound
	}
	return data, contentType, err
}

// SetSongAudio replaces (or creates) a variant's reference audio FOR ONE
// CATEGORY — one row per (variant, category) (song_audio_variant_category_idx),
// so re-uploading the same category overwrites it, but uploading the
// other category doesn't touch this one.
func (st *Store) SetSongAudio(ctx context.Context, variantID int64, category AudioCategory, data []byte, contentType, sha256, sourceURL string, createdByPersonID int64) (int64, error) {
	// createdByPersonID==0 means "unknown/unrecorded" (every normal
	// caller is a real logged-in actorID(r), never 0 — only
	// cmd/songimport restoring an old row whose creator didn't resolve
	// hits this) — inserted as a literal 0 this violated
	// song_audio_created_by_person_id_fkey outright, since the column is
	// a nullable FK, not a "0 means none" convention. Same class of bug
	// as CreatePerson's own email-uniqueness fix.
	var creator *int64
	if createdByPersonID != 0 {
		creator = &createdByPersonID
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		INSERT INTO song_audio (variant_id, category, content_type, byte_size, sha256, source_url, data, created_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		ON CONFLICT (variant_id, category) DO UPDATE SET
			content_type = EXCLUDED.content_type, byte_size = EXCLUDED.byte_size,
			sha256 = EXCLUDED.sha256, source_url = EXCLUDED.source_url,
			data = EXCLUDED.data, created_at = now(), created_by_person_id = EXCLUDED.created_by_person_id
		RETURNING id`,
		variantID, category, contentType, len(data), sha256, sourceURL, data, creator).Scan(&id)
	return id, err
}

// DeleteSongAudio removes one category of a variant's reference audio
// (and, via ON DELETE CASCADE, its markers).
func (st *Store) DeleteSongAudio(ctx context.Context, variantID int64, category AudioCategory) error {
	_, err := st.db.Exec(ctx, `DELETE FROM song_audio WHERE variant_id = $1 AND category = $2`, variantID, category)
	return err
}

// ListAudioMarkers returns a song_audio row's markers, ordered by
// playback position — the order the structure rail / highlight-tracking
// in the player actually wants them in.
func (st *Store) ListAudioMarkers(ctx context.Context, songAudioID int64) ([]AudioMarker, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id, section_index, time_seconds, origin FROM song_audio_markers
		WHERE song_audio_id = $1 ORDER BY time_seconds`, songAudioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []AudioMarker
	for rows.Next() {
		var m AudioMarker
		if err := rows.Scan(&m.ID, &m.SectionIndex, &m.TimeSeconds, &m.Origin); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetAudioMarker sets (or moves) the marker for one section index —
// one marker per section per audio row (song_audio_markers_unique), so
// re-tapping "set marker here" on the same section just updates its time.
// A person placed it: its origin becomes SyncSet.
func (st *Store) SetAudioMarker(ctx context.Context, songAudioID int64, sectionIndex int, timeSeconds float64) error {
	_, err := st.db.Exec(ctx, `
		INSERT INTO song_audio_markers (song_audio_id, section_index, time_seconds, origin)
		VALUES ($1, $2, $3, 'set')
		ON CONFLICT (song_audio_id, section_index) DO UPDATE SET time_seconds = EXCLUDED.time_seconds, origin = 'set'`,
		songAudioID, sectionIndex, timeSeconds)
	return err
}

// ReplaceAudioMarkers makes markers a recording's whole sync, in one
// transaction: markers not in the list are removed. The sync it replaces
// is kept in song_audio_sync_history (the last syncHistoryKeep of them)
// when anything changed. savedBy 0 is unknown.
func (st *Store) ReplaceAudioMarkers(ctx context.Context, songAudioID int64, markers []AudioMarker, savedBy int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	// One save at a time per recording: two phones saving together
	// mustn't interleave their rows.
	if _, err := tx.Exec(ctx, `SELECT 1 FROM song_audio WHERE id = $1 FOR UPDATE`, songAudioID); err != nil {
		return err
	}
	var old string
	if err := tx.QueryRow(ctx, `
		SELECT coalesce(jsonb_agg(jsonb_build_object('sectionIndex', section_index, 'timeSeconds', time_seconds, 'origin', origin) ORDER BY section_index), '[]'::jsonb)::text
		FROM song_audio_markers WHERE song_audio_id = $1`, songAudioID).Scan(&old); err != nil {
		return err
	}
	keep := make([]int32, 0, len(markers))
	for _, m := range markers {
		keep = append(keep, int32(m.SectionIndex))
	}
	tag, err := tx.Exec(ctx, `DELETE FROM song_audio_markers WHERE song_audio_id = $1 AND NOT (section_index = ANY($2))`, songAudioID, keep)
	if err != nil {
		return err
	}
	changed := tag.RowsAffected() > 0
	for _, m := range markers {
		tag, err := tx.Exec(ctx, `
			INSERT INTO song_audio_markers (song_audio_id, section_index, time_seconds, origin)
			VALUES ($1, $2, $3, $4)
			ON CONFLICT (song_audio_id, section_index) DO UPDATE SET time_seconds = EXCLUDED.time_seconds, origin = EXCLUDED.origin
			WHERE song_audio_markers.time_seconds IS DISTINCT FROM EXCLUDED.time_seconds OR song_audio_markers.origin IS DISTINCT FROM EXCLUDED.origin`,
			songAudioID, m.SectionIndex, m.TimeSeconds, syncOrigin(m.Origin))
		if err != nil {
			return err
		}
		changed = changed || tag.RowsAffected() > 0
	}
	if changed && old != "[]" {
		var by *int64
		if savedBy != 0 {
			by = &savedBy
		}
		if _, err := tx.Exec(ctx, `INSERT INTO song_audio_sync_history (song_audio_id, saved_by, markers) VALUES ($1, $2, $3::jsonb)`, songAudioID, by, old); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `
			DELETE FROM song_audio_sync_history WHERE song_audio_id = $1 AND id NOT IN (
				SELECT id FROM song_audio_sync_history WHERE song_audio_id = $1 ORDER BY saved_at DESC, id DESC LIMIT $2)`,
			songAudioID, syncHistoryKeep); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// syncHistoryKeep is how many replaced syncs a recording keeps.
const syncHistoryKeep = 20

// SyncHistoryEntry is one sync a save replaced: the markers as they were
// until SavedAt, when SavedBy saved another.
type SyncHistoryEntry struct {
	ID      int64
	SavedAt time.Time
	SavedBy string // the person's display name, "" when unknown
	Markers []AudioMarker
}

// ListSyncHistory returns a recording's replaced syncs, newest first.
func (st *Store) ListSyncHistory(ctx context.Context, songAudioID int64) ([]SyncHistoryEntry, error) {
	rows, err := st.db.Query(ctx, `
		SELECT h.id, h.saved_at, coalesce(p.display_name, ''), h.markers::text
		FROM song_audio_sync_history h LEFT JOIN people p ON p.id = h.saved_by
		WHERE h.song_audio_id = $1 ORDER BY h.saved_at DESC, h.id DESC`, songAudioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SyncHistoryEntry
	for rows.Next() {
		var e SyncHistoryEntry
		var raw string
		if err := rows.Scan(&e.ID, &e.SavedAt, &e.SavedBy, &raw); err != nil {
			return nil, err
		}
		var ms []struct {
			SectionIndex int     `json:"sectionIndex"`
			TimeSeconds  float64 `json:"timeSeconds"`
			Origin       string  `json:"origin"`
		}
		if err := json.Unmarshal([]byte(raw), &ms); err != nil {
			return nil, err
		}
		for _, m := range ms {
			e.Markers = append(e.Markers, AudioMarker{SectionIndex: m.SectionIndex, TimeSeconds: m.TimeSeconds, Origin: syncOrigin(m.Origin)})
		}
		out = append(out, e)
	}
	return out, rows.Err()
}

// DeleteAudioMarker removes one section's marker.
func (st *Store) DeleteAudioMarker(ctx context.Context, songAudioID int64, sectionIndex int) error {
	_, err := st.db.Exec(ctx, `DELETE FROM song_audio_markers WHERE song_audio_id = $1 AND section_index = $2`, songAudioID, sectionIndex)
	return err
}

// GPBarSyncPoint is one bar's real audio-frame-derived timestamp — see
// migration 0056's own doc comment for why this is a separate, finer-
// grained table from song_audio_markers (per bar, not per section; only
// ever populated from a GP import, never hand-tapped).
type GPBarSyncPoint struct {
	BarIndex    int
	TimeSeconds float64
	Origin      string // SyncAuto or SyncSet ("" is SyncSet when writing)
}

// ListGPBarSyncPoints returns a song_audio row's bar sync points, ordered
// by bar — the sequence the chart's own click-to-play handler walks.
func (st *Store) ListGPBarSyncPoints(ctx context.Context, songAudioID int64) ([]GPBarSyncPoint, error) {
	rows, err := st.db.Query(ctx, `
		SELECT bar_index, time_seconds, origin FROM gp_bar_sync_point
		WHERE song_audio_id = $1 ORDER BY bar_index`, songAudioID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []GPBarSyncPoint
	for rows.Next() {
		var p GPBarSyncPoint
		if err := rows.Scan(&p.BarIndex, &p.TimeSeconds, &p.Origin); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// SetGPBarSyncPoints replaces a song_audio row's entire set of bar sync
// points in one transaction — always written as a whole batch (one GP
// import, or a re-run of the import wizard), never point-edited one bar
// at a time the way a hand-tapped section marker is.
func (st *Store) SetGPBarSyncPoints(ctx context.Context, songAudioID int64, points []GPBarSyncPoint) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `DELETE FROM gp_bar_sync_point WHERE song_audio_id = $1`, songAudioID); err != nil {
		return err
	}
	for _, p := range points {
		if _, err := tx.Exec(ctx, `INSERT INTO gp_bar_sync_point (song_audio_id, bar_index, time_seconds, origin) VALUES ($1, $2, $3, $4)`,
			songAudioID, p.BarIndex, p.TimeSeconds, syncOrigin(p.Origin)); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}
