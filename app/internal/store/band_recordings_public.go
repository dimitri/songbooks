package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// Band recordings, public (docs/ownership-model.md §8, migration 0117).
// Everything goes public with a song (§2): a band's recordings of its own
// public song are heard by everyone signed in. A band may also make one
// recording public, one by one, on a public song it only links (the
// catalogue's, another's): its core members decide, as they decide what
// the band publishes. A private song's recordings are never public: who
// can't open the song can't hear them.
//
// The recordings play from the band's pCloud folder, as the band's own
// pages play them: the browser asks pCloud for the file with the folder's
// public link (docs/band-recordings.md).

// publicRecordingSQL is "r is heard by everyone signed in" (s its song).
const publicRecordingSQL = `(s.public AND NOT r.gone AND (s.owner_band_id = r.band_id OR r.public))`

// PlayedBy is a band whose recordings of a song everyone signed in hears:
// "played by: Mercy (2 recordings)".
type PlayedBy struct {
	BandID     int64
	BandName   string
	Recordings int
}

// PlayedByFor maps each of songIDs to the bands whose recordings of it
// are public, the most recorded first.
func (st *Store) PlayedByFor(ctx context.Context, songIDs []int64) (map[int64][]PlayedBy, error) {
	out := map[int64][]PlayedBy{}
	if len(songIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT r.variant_id, b.id, b.name, count(*)::int
		FROM band_recordings r JOIN songs s ON s.id = r.variant_id JOIN bands b ON b.id = r.band_id
		WHERE r.variant_id = ANY($1) AND `+publicRecordingSQL+`
		GROUP BY r.variant_id, b.id, b.name
		ORDER BY r.variant_id, count(*) DESC, b.name`, songIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var p PlayedBy
		if err := rows.Scan(&id, &p.BandID, &p.BandName, &p.Recordings); err != nil {
			return nil, err
		}
		out[id] = append(out[id], p)
	}
	return out, rows.Err()
}

// PublicRecording is one recording everyone signed in hears, ready to
// play from the band's pCloud folder.
type PublicRecording struct {
	ID          int64
	FileID      int64
	Kind        string
	Name        string
	Size        int64
	SessionDate *time.Time
	Take        *int
	APIHost     string
	Code        string
}

// PublicBandRecordings is one band's public recordings of a song.
type PublicBandRecordings struct {
	BandID     int64
	BandName   string
	Recordings []PublicRecording
}

// PublicSongRecordings is songID's public recordings, by band (the most
// recorded first), each band's latest first.
func (st *Store) PublicSongRecordings(ctx context.Context, songID int64) ([]PublicBandRecordings, error) {
	rows, err := st.db.Query(ctx, `
		SELECT b.id, b.name, r.id, r.file_id, r.kind, r.name, r.size, r.session_date, r.take, src.api_host, src.code
		FROM band_recordings r JOIN songs s ON s.id = r.variant_id JOIN bands b ON b.id = r.band_id
		JOIN band_recording_sources src ON src.id = r.source_id
		WHERE r.variant_id = $1 AND `+publicRecordingSQL+`
		ORDER BY count(*) OVER (PARTITION BY b.id) DESC, b.name, b.id, r.session_date DESC NULLS LAST, r.take NULLS FIRST, r.id`, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []PublicBandRecordings
	for rows.Next() {
		var bandID int64
		var bandName string
		var r PublicRecording
		if err := rows.Scan(&bandID, &bandName, &r.ID, &r.FileID, &r.Kind, &r.Name, &r.Size, &r.SessionDate, &r.Take, &r.APIHost, &r.Code); err != nil {
			return nil, err
		}
		if len(out) == 0 || out[len(out)-1].BandID != bandID {
			out = append(out, PublicBandRecordings{BandID: bandID, BandName: bandName})
		}
		out[len(out)-1].Recordings = append(out[len(out)-1].Recordings, r)
	}
	return out, rows.Err()
}

// RecordingPublicity says where one of a band's recordings stands: Own,
// its song is the band's (public with it, or private with it); Allowed,
// it can be made public one by one (a public song the band links);
// Public, everyone signed in hears it.
type RecordingPublicity struct {
	Own     bool
	Allowed bool
	Public  bool
}

// RecordingPublicities is each of bandID's recordings of songID's
// standing, by recording id.
func (st *Store) RecordingPublicities(ctx context.Context, bandID, songID int64) (map[int64]RecordingPublicity, error) {
	rows, err := st.db.Query(ctx, `
		SELECT r.id, coalesce(s.owner_band_id = r.band_id, false), s.public AND NOT coalesce(s.owner_band_id = r.band_id, false),
		       `+publicRecordingSQL+`
		FROM band_recordings r JOIN songs s ON s.id = r.variant_id
		WHERE r.band_id = $1 AND r.variant_id = $2`, bandID, songID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]RecordingPublicity{}
	for rows.Next() {
		var id int64
		var p RecordingPublicity
		if err := rows.Scan(&id, &p.Own, &p.Allowed, &p.Public); err != nil {
			return nil, err
		}
		out[id] = p
	}
	return out, rows.Err()
}

// SetRecordingPublic makes one of bandID's recordings public, or not, on
// a public song the band links (its own song's are public with it: there
// it's the song that is made public). The caller checks the person is one
// of the band's core members.
func (st *Store) SetRecordingPublic(ctx context.Context, bandID, recordingID int64, public bool) error {
	var songID *int64
	var songPublic, own bool
	err := st.db.QueryRow(ctx, `
		SELECT r.variant_id, coalesce(s.public, false), coalesce(s.owner_band_id = r.band_id, false)
		FROM band_recordings r LEFT JOIN songs s ON s.id = r.variant_id
		WHERE r.id = $1 AND r.band_id = $2`, recordingID, bandID).Scan(&songID, &songPublic, &own)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such recording %d: %w", recordingID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	switch {
	case public && songID == nil:
		return fmt.Errorf("this recording is of no song: %w", ErrInvalid)
	case public && own:
		return fmt.Errorf("this is the band's own song: its recordings are public with it: %w", ErrInvalid)
	case public && !songPublic:
		return fmt.Errorf("this song is private: its recordings can't be public: %w", ErrInvalid)
	}
	_, err = st.db.Exec(ctx, `UPDATE band_recordings SET public = $3 WHERE id = $1 AND band_id = $2`, recordingID, bandID, public)
	return err
}
