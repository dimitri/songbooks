package store

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"app/internal/i18n"
)

// Frozen copies (docs/ownership-model.md §5, migration 0117): making a
// song private, or deleting it, while songbooks of other owners link to it
// gives each of those owners a frozen copy of it as it was — theirs,
// private, its forked_from_id the song (NULL once the song is deleted),
// marked frozen_from "Gin Lane — Mercy" — in the link's place in their
// songbooks (its position and key kept) and set lists; a band's
// recordings of it follow. Nobody else finds it any more. A closed
// songbook's song has none to make: nothing links to it.
//
// Making it private, an owner keeps the link when they still see the
// song: a person who is one of its owner's members (a band's member who
// links the band's song in their own songbook). A band or a community
// links only public songs and its own, so it always gets its copy.
//
// A set list's songs are setlist_items rows (step 3, migration 0115):
// the owner's rows that are the song are repointed at its copy, a
// revision of each set list recorded (repointOwnerSetlists).

// SongUses says who would lose a song made private or deleted: what the
// confirmation says before.
type SongUses struct {
	// Songbooks: other owners' songbooks linking it, each of whose owners
	// keeps a frozen copy.
	Songbooks int
	// Setlists: those owners' set lists listing it.
	Setlists int
	// Owners: who gets a frozen copy.
	Owners []Owner
	// Other owners' own versions of it, by their kind: they keep them.
	BandVersions, PersonVersions, CommunityVersions int
}

// Any reports whether anyone but the song's owner uses it.
func (u *SongUses) Any() bool {
	return u.Songbooks+u.Setlists+u.BandVersions+u.PersonVersions+u.CommunityVersions > 0
}

// Sentence says it in lang: "3 songbooks link to it and 1 band has its
// own version"; "" when nobody uses it.
func (u *SongUses) Sentence(lang string) string {
	var parts []string
	if u.Songbooks > 0 {
		parts = append(parts, i18n.N(lang, u.Songbooks, "%d songbook links to it", "%d songbooks link to it"))
	}
	if u.Setlists > 0 {
		parts = append(parts, i18n.N(lang, u.Setlists, "%d set list lists it", "%d set lists list it"))
	}
	if u.BandVersions > 0 {
		parts = append(parts, i18n.N(lang, u.BandVersions, "%d band has its own arrangement", "%d bands have their own arrangement"))
	}
	if u.PersonVersions > 0 {
		parts = append(parts, i18n.N(lang, u.PersonVersions, "%d person has their own arrangement", "%d people have their own arrangement"))
	}
	if u.CommunityVersions > 0 {
		parts = append(parts, i18n.N(lang, u.CommunityVersions, "%d community has its own arrangement", "%d communities have their own arrangement"))
	}
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return parts[0]
	}
	return i18n.T(lang, "%s and %s", strings.Join(parts[:len(parts)-1], ", "), parts[len(parts)-1])
}

// SongUsesFor is who would lose songID made private (forDelete false) or
// deleted.
func (st *Store) SongUsesFor(ctx context.Context, songID int64, forDelete bool) (*SongUses, error) {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx) // read only: the private song is only tried
	if !forDelete {
		if _, err := tx.Exec(ctx, `UPDATE songs SET public = false WHERE id = $1`, songID); err != nil {
			return nil, err
		}
	}
	return songUses(ctx, tx, songID, forDelete)
}

func songUses(ctx context.Context, tx pgx.Tx, songID int64, forDelete bool) (*SongUses, error) {
	var owner Owner
	var closed bool
	err := tx.QueryRow(ctx, `
		SELECT owner_kind, coalesce(owner_person_id, owner_band_id, owner_community_id), song_closed(id)
		FROM songs WHERE id = $1`, songID).Scan(&owner.Kind, &owner.ID, &closed)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil {
		return nil, err
	}
	u := &SongUses{}
	if err := tx.QueryRow(ctx, `
		SELECT count(*) FILTER (WHERE owner_kind = 'band'), count(*) FILTER (WHERE owner_kind = 'person'),
		       count(*) FILTER (WHERE owner_kind = 'community')
		FROM songs WHERE forked_from_id = $1 AND frozen_at IS NULL
		  AND (owner_kind <> $2 OR coalesce(owner_person_id, owner_band_id, owner_community_id) <> $3)`,
		songID, owner.Kind, owner.ID).Scan(&u.BandVersions, &u.PersonVersions, &u.CommunityVersions); err != nil {
		return nil, err
	}
	if closed {
		return u, nil
	}
	// Every other owner holding it: in a songbook, or a set list.
	rows, err := tx.Query(ctx, `
		WITH holders AS (
		  SELECT sb.owner_kind AS kind, coalesce(sb.owner_person_id, sb.owner_band_id, sb.owner_community_id) AS id,
		         1 AS books, 0 AS lists
		  FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		  WHERE se.variant_id = $1
		  UNION ALL
		  SELECT sl.scope::text, coalesce(sl.owner_person_id, sl.owner_band_id, sl.owner_community_id), 0, 1
		  FROM setlists sl WHERE EXISTS (SELECT 1 FROM setlist_items i WHERE i.setlist_id = sl.id AND i.variant_id = $1)
		)
		SELECT kind, id, sum(books)::int, sum(lists)::int FROM holders
		WHERE NOT (kind = $2 AND id = $3)
		  AND ($4 OR kind <> 'person' OR NOT song_visible($1, id))
		GROUP BY kind, id ORDER BY kind, id`, songID, owner.Kind, owner.ID, forDelete)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var o Owner
		var books, lists int
		if err := rows.Scan(&o.Kind, &o.ID, &books, &lists); err != nil {
			return nil, err
		}
		u.Owners = append(u.Owners, o)
		u.Songbooks += books
		u.Setlists += lists
	}
	return u, rows.Err()
}

// freezeLinks gives each owner SongUses names a frozen copy of songID in
// its place, in tx (songID made private in it already, when it is).
// Returns the copies, by owner.
func (st *Store) freezeLinks(ctx context.Context, tx pgx.Tx, songID int64, forDelete bool, actorID int64) (map[Owner]int64, error) {
	u, err := songUses(ctx, tx, songID, forDelete)
	if err != nil || len(u.Owners) == 0 {
		return nil, err
	}
	var title, artist string
	if err := tx.QueryRow(ctx, `SELECT title, artist FROM songs WHERE id = $1 FOR UPDATE`, songID).Scan(&title, &artist); err != nil {
		return nil, err
	}
	from := title
	if artist != "" {
		from += " — " + artist
	}
	out := map[Owner]int64{}
	for _, o := range u.Owners {
		var copyID int64
		if err := tx.QueryRow(ctx, `
			INSERT INTO songs (title, artist, reference_url, body, work_id, version_name,
			                   owner_person_id, owner_band_id, owner_community_id, forked_from_id, created_by_person_id,
			                   frozen_from, frozen_at)
			SELECT title, artist, reference_url, body, work_id, version_name,
			       CASE WHEN $2 = 'person' THEN $3::bigint END, CASE WHEN $2 = 'band' THEN $3::bigint END,
			       CASE WHEN $2 = 'community' THEN $3::bigint END, id, $4, $5, now()
			FROM songs WHERE id = $1
			RETURNING id`, songID, o.Kind, o.ID, nullID(actorID), from).Scan(&copyID); err != nil {
			return nil, err
		}
		if err := copySongMedia(ctx, tx, songID, copyID); err != nil {
			return nil, err
		}
		// In the link's place in its songbooks: position, key, lesson kept.
		rows, err := tx.Query(ctx, `
			SELECT sb.id FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
			WHERE se.variant_id = $1 AND sb.owner_kind = $2
			  AND coalesce(sb.owner_person_id, sb.owner_band_id, sb.owner_community_id) = $3
			ORDER BY sb.id`, songID, o.Kind, o.ID)
		if err != nil {
			return nil, err
		}
		var books []int64
		for rows.Next() {
			var id int64
			if err := rows.Scan(&id); err != nil {
				rows.Close()
				return nil, err
			}
			books = append(books, id)
		}
		rows.Close()
		if err := rows.Err(); err != nil {
			return nil, err
		}
		for _, sbID := range books {
			if _, err := tx.Exec(ctx, `UPDATE songbook_entries SET variant_id = $3 WHERE songbook_id = $1 AND variant_id = $2`,
				sbID, songID, copyID); err != nil {
				return nil, err
			}
			if err := replaceInSongOrder(ctx, tx, sbID, songID, copyID); err != nil {
				return nil, err
			}
		}
		if err := st.repointOwnerSetlists(ctx, tx, o, songID, copyID, actorID); err != nil {
			return nil, err
		}
		if o.Kind == "band" {
			// The band's recordings of it, and its choice among them.
			for _, q := range []string{
				`UPDATE band_recordings SET variant_id = $3 WHERE band_id = $1 AND variant_id = $2`,
				`UPDATE band_song_recordings SET variant_id = $3 WHERE band_id = $1 AND variant_id = $2`,
				`UPDATE band_recording_fixes SET to_value = $3::bigint::text WHERE band_id = $1 AND kind = 'title' AND to_value = $2::bigint::text`,
			} {
				if _, err := tx.Exec(ctx, q, o.ID, songID, copyID); err != nil {
					return nil, err
				}
			}
		}
		out[o] = copyID
	}
	return out, nil
}

// keepPractice keeps the practice of songID, about to be deleted: a
// person's days of it (practice_logs) and checked takes (practice_takes)
// go to the frozen copy made for that person (copies); the days of
// everyone else keep the song's title and artist (migration 0119), their
// takes go with the song — the day keeps how many there were and the last
// scores. Notes on the song (annotations) go with it.
func keepPractice(ctx context.Context, tx pgx.Tx, songID int64, copies map[Owner]int64) error {
	for o, copyID := range copies {
		if o.Kind != "person" {
			continue
		}
		for _, q := range []string{
			`UPDATE practice_logs SET variant_id = $3 WHERE variant_id = $1 AND person_id = $2`,
			`UPDATE practice_takes SET variant_id = $3 WHERE variant_id = $1 AND person_id = $2`,
		} {
			if _, err := tx.Exec(ctx, q, songID, o.ID, copyID); err != nil {
				return err
			}
		}
	}
	_, err := tx.Exec(ctx, `
		UPDATE practice_logs l SET song_title = s.title, song_artist = s.artist
		FROM songs s WHERE s.id = $1 AND l.variant_id = s.id`, songID)
	return err
}

// DeleteSong deletes songID for actorID (who may: the router's check),
// every other owner linking it keeping a frozen copy first, and the
// practice of it kept (keepPractice).
func (st *Store) DeleteSong(ctx context.Context, songID, actorID int64) error {
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	copies, err := st.freezeLinks(ctx, tx, songID, true, actorID)
	if err != nil {
		return err
	}
	if err := keepPractice(ctx, tx, songID, copies); err != nil {
		return err
	}
	if err := deleteSong(ctx, tx, songID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// Frozen is what a frozen copy was made from, and when.
type Frozen struct {
	From string // "Gin Lane — Mercy"
	At   time.Time
	// OriginalID is the song it was frozen from, while that song exists.
	OriginalID int64
}

// FrozenOf is songID's Frozen, nil when it isn't a frozen copy.
func (st *Store) FrozenOf(ctx context.Context, songID int64) (*Frozen, error) {
	var f Frozen
	var at *time.Time
	var orig *int64
	err := st.db.QueryRow(ctx, `SELECT frozen_from, frozen_at, forked_from_id FROM songs WHERE id = $1`, songID).Scan(&f.From, &at, &orig)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such song %d: %w", songID, ErrNotFound)
	}
	if err != nil || at == nil {
		return nil, err
	}
	f.At = *at
	if orig != nil {
		f.OriginalID = *orig
	}
	return &f, nil
}
