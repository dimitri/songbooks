package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

// A band's pools of songs to listen to, besides its proposals to vote on
// (docs/band-votes.md): Discover — songs a member shares for the band to
// hear, no vote, until someone proposes one — and Influences — the
// artists and songs that shape the band's sound, with what in them
// ("the drum feel"), tagged. Each is a YouTube link, its title and
// artist read from the video; a member whose role grants propose (by
// default, any member) adds; who added a song removes it, and so does a
// member whose role grants curate_listen (by default, core) — anyone's.
// An archived band's pools are read only.

// The pools.
const (
	PoolDiscover  = "discover"
	PoolInfluence = "influence"
)

// BandListen is one song of a pool.
type BandListen struct {
	ID          int64
	BandID      int64
	Pool        string
	URL         string
	VideoID     string
	Title       string
	Artist      string
	Note        string
	Tags        []string
	AddedBy     int64
	AddedByName string
	CreatedAt   time.Time
	ProposalID  int64 // proposed for a vote since, 0 when not
}

// NewBandListen is what a member adds to a pool.
type NewBandListen struct {
	Pool, URL, Title, Artist, Note string
	Tags                           []string
}

// cleanTags is tags trimmed, lowercased, without empties or doubles, 8
// at most.
func cleanTags(tags []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, t := range tags {
		t = strings.ToLower(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "#")))
		if t == "" || len(t) > 30 || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
		if len(out) == 8 {
			break
		}
	}
	return out
}

// AddBandListen adds a song to bandID's pool, by personID (a current
// member). The link must be a YouTube video's (it plays in the band's
// player).
func (st *Store) AddBandListen(ctx context.Context, bandID, personID int64, n NewBandListen) (int64, error) {
	if n.Pool != PoolDiscover && n.Pool != PoolInfluence {
		return 0, fmt.Errorf("invalid pool %q: %w", n.Pool, ErrInvalid)
	}
	vid := YouTubeID(n.URL)
	if vid == "" {
		return 0, fmt.Errorf("not a YouTube video: %w", ErrInvalid)
	}
	n.Title, n.Artist, n.Note = strings.TrimSpace(n.Title), strings.TrimSpace(n.Artist), strings.TrimSpace(n.Note)
	if len(n.Note) > 500 || len(n.Title) > 200 || len(n.Artist) > 200 {
		return 0, fmt.Errorf("too long: %w", ErrInvalid)
	}
	ok, err := st.MayShareListen(ctx, bandID, personID)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, ErrForbidden
	}
	var id int64
	err = st.db.QueryRow(ctx, `
		INSERT INTO band_listens (band_id, pool, url, video_id, title, artist, note, tags, added_by_person_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9) RETURNING id`,
		bandID, n.Pool, "https://www.youtube.com/watch?v="+vid, vid, n.Title, n.Artist, n.Note, cleanTags(n.Tags), personID).Scan(&id)
	return id, err
}

// ListBandListens is bandID's pool, newest first.
func (st *Store) ListBandListens(ctx context.Context, bandID int64, pool string) ([]BandListen, error) {
	rows, err := st.db.Query(ctx, `
		SELECT l.id, l.band_id, l.pool, l.url, l.video_id, l.title, l.artist, l.note, l.tags,
		       coalesce(l.added_by_person_id, 0), coalesce(p.display_name, ''),
		       l.created_at, coalesce(l.proposal_id, 0)
		FROM band_listens l LEFT JOIN people p ON p.id = l.added_by_person_id
		WHERE l.band_id = $1 AND l.pool = $2
		ORDER BY l.created_at DESC, l.id DESC`, bandID, pool)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandListen
	for rows.Next() {
		var l BandListen
		if err := rows.Scan(&l.ID, &l.BandID, &l.Pool, &l.URL, &l.VideoID, &l.Title, &l.Artist, &l.Note, &l.Tags,
			&l.AddedBy, &l.AddedByName, &l.CreatedAt, &l.ProposalID); err != nil {
			return nil, err
		}
		out = append(out, l)
	}
	return out, rows.Err()
}

// DeleteBandListen removes a song from bandID's pools: the one who added
// it, or anyone's for a curator (curate: their role grants curate_listen,
// or a site admin — the HTTP layer's bandPrivilege); ErrForbidden
// otherwise, and in an archived band.
func (st *Store) DeleteBandListen(ctx context.Context, bandID, listenID, personID int64, curate bool) error {
	tag, err := st.db.Exec(ctx, `
		DELETE FROM band_listens l WHERE l.id = $1 AND l.band_id = $2 AND (l.added_by_person_id = $3 OR $4)
		  AND NOT EXISTS (SELECT 1 FROM bands b WHERE b.id = l.band_id AND b.archived_at IS NOT NULL)`, listenID, bandID, personID, curate)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err := st.db.QueryRow(ctx, `SELECT EXISTS (SELECT 1 FROM band_listens WHERE id = $1 AND band_id = $2)`, listenID, bandID).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return fmt.Errorf("no such song %d in band %d: %w", listenID, bandID, ErrNotFound)
		}
		return fmt.Errorf("only who shared it, or a member whose role curates the band's lists, removes it: %w", ErrForbidden)
	}
	return nil
}

// SetBandListenProposal says a shared song was proposed for a vote since
// (proposalID, of the same band).
func (st *Store) SetBandListenProposal(ctx context.Context, bandID, listenID, proposalID int64) error {
	var band int64
	err := st.db.QueryRow(ctx, `SELECT band_id FROM band_proposals WHERE id = $1`, proposalID).Scan(&band)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && band != bandID) {
		return fmt.Errorf("no such proposal %d in band %d: %w", proposalID, bandID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `UPDATE band_listens SET proposal_id = $3 WHERE id = $1 AND band_id = $2`, listenID, bandID, proposalID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such song %d in band %d: %w", listenID, bandID, ErrNotFound)
	}
	return nil
}
