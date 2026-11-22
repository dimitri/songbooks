package store

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strconv"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// A band's songs: a band's repertoire and its set
// lists hold the band's own songs (visibility 'band', owner_band_id), which
// every current member changes in place, for the whole band. A catalogue
// song, or someone's own song, that a band uses becomes the band's through
// AdoptIntoBand: a copy for the band (forked_from_id the original, its
// media copied), and the band's songbooks, set lists and recordings
// pointed at the copy. The original stays as it is, with its history.

// BandVersion is a band's own song of the same piece as a person's own
// copy: what the copy's page and the person's songbooks point out, so a
// member doesn't keep working on a copy while the band plays its version.
type BandVersion struct {
	VariantID     int64 // the person's copy
	BandID        int64
	BandName      string
	BandVariantID int64 // the band's song
}

// BandVersionsOf finds, for each of variantIDs that is personID's own song
// ('user', owner_person_id), the song of a band personID currently belongs
// to that it was copied from (forked_from_id), else one of the same work.
// Variants with no such band song are absent from the map.
func (st *Store) BandVersionsOf(ctx context.Context, personID int64, variantIDs []int64) (map[int64]*BandVersion, error) {
	out := map[int64]*BandVersion{}
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT DISTINCT ON (c.id) c.id, b.id, b.name, bs.id
		FROM songs c
		JOIN songs bs ON bs.visibility = 'band' AND (bs.id = c.forked_from_id OR bs.work_id = c.work_id)
		JOIN bands b ON b.id = bs.owner_band_id
		JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		WHERE c.id = ANY($2) AND c.visibility = 'user' AND c.owner_person_id = $1
		ORDER BY c.id, (bs.id = c.forked_from_id) DESC, b.name, bs.id`, personID, variantIDs)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var v BandVersion
		if err := rows.Scan(&v.VariantID, &v.BandID, &v.BandName, &v.BandVariantID); err != nil {
			return nil, err
		}
		out[v.VariantID] = &v
	}
	return out, rows.Err()
}

// BandVersionOf is BandVersionsOf for one song: nil when there's none.
func (st *Store) BandVersionOf(ctx context.Context, personID, variantID int64) (*BandVersion, error) {
	m, err := st.BandVersionsOf(ctx, personID, []int64{variantID})
	if err != nil {
		return nil, err
	}
	return m[variantID], nil
}

// BandHold is a band that uses a song it doesn't own: the song is in one
// of the band's songbooks or set lists. OwnSongID is the band's own copy
// of it when the band already has one (forked_from_id), else 0.
type BandHold struct {
	BandID    int64
	BandName  string
	OwnSongID int64
}

// BandsHolding lists the bands personID currently belongs to that use
// variantID without owning it, by name.
func (st *Store) BandsHolding(ctx context.Context, personID, variantID int64) ([]BandHold, error) {
	rows, err := st.db.Query(ctx, `
		SELECT b.id, b.name,
		       coalesce((SELECT min(c.id) FROM songs c
		                 WHERE c.visibility = 'band' AND c.owner_band_id = b.id AND c.forked_from_id = s.id), 0)
		FROM songs s
		JOIN bands b ON true
		JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		WHERE s.id = $2
		  AND NOT (s.visibility = 'band' AND s.owner_band_id = b.id)
		  AND (EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		               WHERE sb.owner_kind = 'band' AND sb.owner_band_id = b.id AND se.variant_id = s.id)
		       OR EXISTS (SELECT 1 FROM setlists sl
		                  WHERE sl.scope = 'band' AND sl.owner_band_id = b.id
		                    AND sl.body ~ ('variant:\s*' || s.id::text || '(\D|$)')))
		ORDER BY b.name, b.id`, personID, variantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []BandHold
	for rows.Next() {
		var h BandHold
		if err := rows.Scan(&h.BandID, &h.BandName, &h.OwnSongID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// IsBandSong reports whether variantID is bandID's own song.
func (st *Store) IsBandSong(ctx context.Context, bandID, variantID int64) (bool, error) {
	var ok bool
	err := st.db.QueryRow(ctx, `
		SELECT EXISTS(SELECT 1 FROM songs WHERE id = $2 AND visibility = 'band' AND owner_band_id = $1)`,
		bandID, variantID).Scan(&ok)
	return ok, err
}

// VariantOwner says whose song variantID is, for the page that explains
// why someone can't change it: Kind is the song's visibility ('global',
// 'user', 'band', 'community', 'setlist'), Name the band's, community's,
// person's or set list's name ("" for the catalogue).
type VariantOwner struct {
	Kind string
	Name string
}

func (st *Store) VariantOwnerOf(ctx context.Context, variantID int64) (*VariantOwner, error) {
	var o VariantOwner
	err := st.db.QueryRow(ctx, `
		SELECT s.visibility::text,
		       coalesce(b.name, c.name, p.display_name, sl.title, '')
		FROM songs s
		LEFT JOIN bands b ON s.visibility = 'band' AND b.id = s.owner_band_id
		LEFT JOIN communities c ON s.visibility = 'community' AND c.id = s.owner_community_id
		LEFT JOIN people p ON s.visibility = 'user' AND p.id = s.owner_person_id
		LEFT JOIN setlists sl ON s.visibility = 'setlist' AND sl.id = s.owner_setlist_id
		WHERE s.id = $1`, variantID).Scan(&o.Kind, &o.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	return &o, err
}

// AdoptIntoBand makes variantID the band's own song and returns that
// song's id: variantID itself when the band owns it already; else the
// band's copy of it when it has one (forked_from_id); else a new copy for
// the band (content and media, as ForkVariant copies them), from the
// catalogue's song or a person's. Another band's song is refused
// (*OtherBandSongError): a band never takes another band's song, neither
// as a pointer nor as a copy — an original band's songs stay theirs, and
// covers come from the catalogue. Then, when the
// band's song isn't variantID, everything of the band's that pointed at
// variantID points at the band's song: its songbooks' entries (in place,
// keeping their order, key and lesson), its set lists' items (a revision
// recorded for each set list changed), its recordings matched to the song.
// The band's song is in the band's default songbook at the end. created
// says a copy was made. One transaction.
//
// No permission check: the HTTP layer checks that actorPersonID is a
// current member who may copy the song (CanCopyVariant), and
// cmd/bandsongs runs it as an administrator. actorPersonID 0 is "no one"
// (the migration).
func (st *Store) AdoptIntoBand(ctx context.Context, bandID, variantID, actorPersonID int64) (bandSongID int64, created bool, err error) {
	defaultSb, err := st.GetDefaultSongbook(ctx, "band", bandID)
	if err != nil {
		return 0, false, err
	}
	var actor *int64
	if actorPersonID != 0 {
		actor = &actorPersonID
	}

	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, false, err
	}
	defer tx.Rollback(ctx)

	var visibility, title, artist, referenceURL, body string
	var ownerBandID *int64
	var workID int64
	err = tx.QueryRow(ctx, `
		SELECT visibility::text, owner_band_id, work_id, title, artist, reference_url, body
		FROM songs WHERE id = $1 FOR UPDATE`, variantID).
		Scan(&visibility, &ownerBandID, &workID, &title, &artist, &referenceURL, &body)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, false, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return 0, false, err
	}

	switch {
	case visibility == "band" && ownerBandID != nil && *ownerBandID == bandID:
		bandSongID = variantID
	case visibility == "band":
		tx.Rollback(ctx)
		return 0, false, st.otherBandSongError(ctx, variantID)
	default:
		err = tx.QueryRow(ctx, `
			SELECT id FROM songs WHERE visibility = 'band' AND owner_band_id = $1 AND forked_from_id = $2
			ORDER BY id LIMIT 1`, bandID, variantID).Scan(&bandSongID)
		if errors.Is(err, pgx.ErrNoRows) {
			err = tx.QueryRow(ctx, `
				INSERT INTO songs (title, artist, reference_url, body, work_id,
				                   visibility, owner_band_id, forked_from_id, created_by_person_id)
				VALUES ($1, $2, $3, $4, $5, 'band', $6, $7, $8)
				RETURNING id`,
				title, artist, referenceURL, body, workID, bandID, variantID, actor).Scan(&bandSongID)
			if err != nil {
				return 0, false, err
			}
			if err := copySongMedia(ctx, tx, variantID, bandSongID); err != nil {
				return 0, false, err
			}
			created = true
		} else if err != nil {
			return 0, false, err
		}
	}

	if bandSongID != variantID {
		if err := repointBandSong(ctx, st, tx, bandID, variantID, bandSongID, actor); err != nil {
			return 0, false, err
		}
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)
		ON CONFLICT (songbook_id, variant_id) DO NOTHING`, defaultSb, bandSongID, actor); err != nil {
		return 0, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, false, err
	}
	return bandSongID, created, nil
}

// OtherBandSongError is AdoptIntoBand refusing another band's song: whose
// it is, and the catalogue's version of the same work when there is one
// (CatalogVariantID, 0 for an original), which the band may take instead.
// It wraps ErrForbidden.
type OtherBandSongError struct {
	BandName         string
	CatalogVariantID int64
}

func (e *OtherBandSongError) Error() string {
	return fmt.Sprintf("this is %s's song: a band doesn't take another band's song", e.BandName)
}

func (e *OtherBandSongError) Unwrap() error { return ErrForbidden }

// BandMayTake is nil when bandID may take variantID into its songbook or
// set lists (its own song, the catalogue's, a person's), else AdoptIntoBand's
// refusal: *OtherBandSongError for another band's song.
func (st *Store) BandMayTake(ctx context.Context, bandID, variantID int64) error {
	var visibility string
	var owner *int64
	err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_band_id FROM songs WHERE id = $1`, variantID).Scan(&visibility, &owner)
	if errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	if err != nil {
		return err
	}
	if visibility == "band" && (owner == nil || *owner != bandID) {
		return st.otherBandSongError(ctx, variantID)
	}
	return nil
}

// otherBandSongError says whose band song variantID is, and its work's
// catalogue version: the one it was copied from when that's the
// catalogue's, else the oldest of the work's.
func (st *Store) otherBandSongError(ctx context.Context, variantID int64) error {
	e := &OtherBandSongError{}
	err := st.db.QueryRow(ctx, `
		SELECT coalesce(b.name, ''),
		       coalesce((SELECT g.id FROM songs g
		                 WHERE g.visibility = 'global' AND (g.id = s.forked_from_id OR g.work_id = s.work_id)
		                 ORDER BY (g.id = s.forked_from_id) DESC, g.id LIMIT 1), 0)
		FROM songs s LEFT JOIN bands b ON b.id = s.owner_band_id
		WHERE s.id = $1`, variantID).Scan(&e.BandName, &e.CatalogVariantID)
	if err != nil {
		return err
	}
	return e
}

// repointBandSong points everything of bandID's at from to `to` instead
// (AdoptIntoBand's second half).
func repointBandSong(ctx context.Context, st *Store, tx pgx.Tx, bandID, from, to int64, actor *int64) error {
	// Songbook entries: a songbook that already holds `to` just loses its
	// entry for `from`; the others' entries change song in place.
	if _, err := tx.Exec(ctx, `
		DELETE FROM songbook_entries se USING songbooks sb
		WHERE se.songbook_id = sb.id AND sb.owner_kind = 'band' AND sb.owner_band_id = $1
		  AND se.variant_id = $2
		  AND EXISTS (SELECT 1 FROM songbook_entries x WHERE x.songbook_id = se.songbook_id AND x.variant_id = $3)`,
		bandID, from, to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE songbook_entries se SET variant_id = $3
		FROM songbooks sb
		WHERE se.songbook_id = sb.id AND sb.owner_kind = 'band' AND sb.owner_band_id = $1 AND se.variant_id = $2`,
		bandID, from, to); err != nil {
		return err
	}
	// The band's recordings of the song, and its choice among them.
	if _, err := tx.Exec(ctx, `UPDATE band_recordings SET variant_id = $3 WHERE band_id = $1 AND variant_id = $2`, bandID, from, to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE band_song_recordings SET variant_id = $3
		WHERE band_id = $1 AND variant_id = $2
		  AND NOT EXISTS (SELECT 1 FROM band_song_recordings x WHERE x.band_id = $1 AND x.variant_id = $3)`,
		bandID, from, to); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE band_recording_fixes SET to_value = $3::bigint::text
		WHERE band_id = $1 AND kind = 'title' AND to_value = $2::bigint::text`, bandID, from, to); err != nil {
		return err
	}
	// The band's set lists.
	rows, err := tx.Query(ctx, `
		SELECT id, scope::text, title, notes, body, owner_person_id, owner_band_id
		FROM setlists WHERE scope = 'band' AND owner_band_id = $1
		  AND body ~ ('variant:\s*' || $2::bigint::text || '(\D|$)')
		ORDER BY id FOR UPDATE`, bandID, from)
	if err != nil {
		return err
	}
	type slRow struct {
		id                        int64
		scope, title, notes, body string
		ownerPerson, ownerBand    *int64
	}
	var lists []slRow
	for rows.Next() {
		var r slRow
		if err := rows.Scan(&r.id, &r.scope, &r.title, &r.notes, &r.body, &r.ownerPerson, &r.ownerBand); err != nil {
			rows.Close()
			return err
		}
		lists = append(lists, r)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, r := range lists {
		l := setlist.Parse(r.body)
		if !RepointSetlist(l, from, to) {
			continue
		}
		if err := st.RecordRevision(ctx, tx, "setlist", strconv.FormatInt(r.id, 10), actor, SetlistSnapshot{
			Exists: true, Scope: r.scope, Title: r.title, Notes: r.notes,
			Body: r.body, OwnerPersonID: r.ownerPerson, OwnerBandID: r.ownerBand,
		}); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `UPDATE setlists SET body = $1, version = version + 1 WHERE id = $2`,
			setlist.Write(l), r.id); err != nil {
			return err
		}
	}
	return nil
}

// RepointSetlist changes every item of l that is the song `from` to the
// song `to`, and says whether any was.
func RepointSetlist(l *setlist.Setlist, from, to int64) bool {
	changed := false
	for i := range l.Sets {
		for j := range l.Sets[i].Entries {
			if l.Sets[i].Entries[j].VariantID == from {
				l.Sets[i].Entries[j].VariantID = to
				changed = true
			}
		}
	}
	return changed
}

// SetlistVariantIDs lists the songs l's items point at, each once, in
// order.
func SetlistVariantIDs(l *setlist.Setlist) []int64 {
	seen := map[int64]bool{}
	var out []int64
	for _, s := range l.Sets {
		for _, e := range s.Entries {
			if e.IsVariant() && !seen[e.VariantID] {
				seen[e.VariantID] = true
				out = append(out, e.VariantID)
			}
		}
	}
	return out
}

// BandAdoption is one song a band uses without owning it, as cmd/bandsongs
// plans it: Action is "copy" (a new copy for the band), "reuse" (the band
// already has its copy; only the pointers move), or "skip" (another
// band's song — a band never takes another band's song, a person decides
// what to do with it — or a community's, whose songbooks may be private
// or sold).
type BandAdoption struct {
	BandID       int64
	BandName     string
	VariantID    int64
	Visibility   string
	BandSongID   int64 // "reuse": the band's copy
	Entries      int   // songbook entries that point at it
	SetlistItems int   // set list items that point at it
	Recordings   int   // the band's recordings matched to it
	MediaBytes   int64 // "copy": the audio and Guitar Pro files the copy duplicates
	Action       string
}

// PlanBandAdoptions lists every song a band's songbooks or set lists hold
// that isn't the band's own, with what AdoptIntoBand would do with it. It
// only reads.
func (st *Store) PlanBandAdoptions(ctx context.Context) ([]BandAdoption, error) {
	// Songbook entries.
	rows, err := st.db.Query(ctx, `
		SELECT b.id, b.name, s.id, s.visibility::text, count(*)
		FROM songbook_entries se
		JOIN songbooks sb ON sb.id = se.songbook_id AND sb.owner_kind = 'band'
		JOIN bands b ON b.id = sb.owner_band_id
		JOIN songs s ON s.id = se.variant_id
		WHERE NOT (s.visibility = 'band' AND s.owner_band_id = b.id)
		GROUP BY b.id, b.name, s.id, s.visibility`)
	if err != nil {
		return nil, err
	}
	type key struct{ band, variant int64 }
	plan := map[key]*BandAdoption{}
	for rows.Next() {
		var a BandAdoption
		if err := rows.Scan(&a.BandID, &a.BandName, &a.VariantID, &a.Visibility, &a.Entries); err != nil {
			rows.Close()
			return nil, err
		}
		plan[key{a.BandID, a.VariantID}] = &a
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}

	// Set list items.
	rows, err = st.db.Query(ctx, `
		SELECT sl.owner_band_id, b.name, sl.body
		FROM setlists sl JOIN bands b ON b.id = sl.owner_band_id
		WHERE sl.scope = 'band'`)
	if err != nil {
		return nil, err
	}
	type item struct {
		band     int64
		name     string
		variants []int64
	}
	var items []item
	for rows.Next() {
		var it item
		var body string
		if err := rows.Scan(&it.band, &it.name, &body); err != nil {
			rows.Close()
			return nil, err
		}
		for _, s := range setlist.Parse(body).Sets {
			for _, e := range s.Entries {
				if e.IsVariant() {
					it.variants = append(it.variants, e.VariantID)
				}
			}
		}
		items = append(items, it)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for _, it := range items {
		for _, v := range it.variants {
			var visibility string
			var ownerBand *int64
			if err := st.db.QueryRow(ctx, `SELECT visibility::text, owner_band_id FROM songs WHERE id = $1`, v).
				Scan(&visibility, &ownerBand); errors.Is(err, pgx.ErrNoRows) {
				continue // a deleted song: nothing to point at
			} else if err != nil {
				return nil, err
			}
			if visibility == "band" && ownerBand != nil && *ownerBand == it.band {
				continue
			}
			a := plan[key{it.band, v}]
			if a == nil {
				a = &BandAdoption{BandID: it.band, BandName: it.name, VariantID: v, Visibility: visibility}
				plan[key{it.band, v}] = a
			}
			a.SetlistItems++
		}
	}

	out := make([]BandAdoption, 0, len(plan))
	for _, a := range plan {
		if err := st.db.QueryRow(ctx, `SELECT count(*) FROM band_recordings WHERE band_id = $1 AND variant_id = $2`,
			a.BandID, a.VariantID).Scan(&a.Recordings); err != nil {
			return nil, err
		}
		switch a.Visibility {
		case "global", "user":
			err := st.db.QueryRow(ctx, `
				SELECT id FROM songs WHERE visibility = 'band' AND owner_band_id = $1 AND forked_from_id = $2
				ORDER BY id LIMIT 1`, a.BandID, a.VariantID).Scan(&a.BandSongID)
			switch {
			case errors.Is(err, pgx.ErrNoRows):
				a.Action = "copy"
				if err := st.db.QueryRow(ctx, `
					SELECT coalesce((SELECT sum(byte_size) FROM song_audio WHERE variant_id = $1), 0)
					     + coalesce((SELECT sum(byte_size) FROM song_gp_source_file WHERE variant_id = $1), 0)
					     + coalesce((SELECT sum(byte_size) FROM song_gp_source_file_version WHERE variant_id = $1), 0)`,
					a.VariantID).Scan(&a.MediaBytes); err != nil {
					return nil, err
				}
			case err != nil:
				return nil, err
			default:
				a.Action = "reuse"
			}
		default:
			a.Action = "skip"
		}
		out = append(out, *a)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].BandName != out[j].BandName {
			return out[i].BandName < out[j].BandName
		}
		if out[i].BandID != out[j].BandID {
			return out[i].BandID < out[j].BandID
		}
		return out[i].VariantID < out[j].VariantID
	})
	return out, nil
}

// BandSongRef is a band's own version of a work.
type BandSongRef struct {
	VariantID     int64
	BandID        int64
	BandName      string
	Title, Artist string
}

// BandVersionsOfCatalogSongs finds, for each catalogue ('global') song
// among variantIDs, the versions of its work that the bands personID
// currently belongs to have: one per band (the band's copy of that very
// song first), by band name. Songs without any are absent from the map.
// Search and the library show a member these in place of the catalogue's
// song (PreferBandVersions): the band's version is the one they play.
func (st *Store) BandVersionsOfCatalogSongs(ctx context.Context, personID int64, variantIDs []int64) (map[int64][]BandSongRef, error) {
	return st.BandVersionsOfCatalogSongsFor(ctx, personID, variantIDs, 0)
}

// BandVersionsOfCatalogSongsFor is BandVersionsOfCatalogSongs for one band
// only when onlyBand isn't 0: what a band's add dialogs show (its own
// version of a song, else the catalogue's — never another band's).
func (st *Store) BandVersionsOfCatalogSongsFor(ctx context.Context, personID int64, variantIDs []int64, onlyBand int64) (map[int64][]BandSongRef, error) {
	out := map[int64][]BandSongRef{}
	if len(variantIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT g, id, band_id, band_name, title, artist FROM (
		  SELECT DISTINCT ON (g.id, b.id) g.id AS g, bs.id, b.id AS band_id, b.name AS band_name, bs.title, bs.artist
		  FROM songs g
		  JOIN songs bs ON bs.visibility = 'band' AND bs.work_id = g.work_id
		  JOIN bands b ON b.id = bs.owner_band_id
		  JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		  WHERE g.id = ANY($2) AND g.visibility = 'global' AND ($3 = 0 OR b.id = $3)
		  ORDER BY g.id, b.id, (bs.forked_from_id IS NOT DISTINCT FROM g.id) DESC, bs.id
		) v
		ORDER BY g, band_name, band_id`, personID, variantIDs, onlyBand)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var g int64
		var r BandSongRef
		if err := rows.Scan(&g, &r.VariantID, &r.BandID, &r.BandName, &r.Title, &r.Artist); err != nil {
			return nil, err
		}
		out[g] = append(out[g], r)
	}
	return out, rows.Err()
}

// PreferBandVersions is vs with each catalogue song that a band of
// personID's has its own version of replaced by those versions, each
// labelled with its band (BandName), in its place; a song already listed
// isn't listed twice.
func (st *Store) PreferBandVersions(ctx context.Context, personID int64, vs []CatalogVariant) ([]CatalogVariant, error) {
	return st.PreferBandVersionsFor(ctx, personID, vs, 0)
}

// PreferBandVersionsFor is PreferBandVersions for one band only when
// onlyBand isn't 0 (BandVersionsOfCatalogSongsFor).
func (st *Store) PreferBandVersionsFor(ctx context.Context, personID int64, vs []CatalogVariant, onlyBand int64) ([]CatalogVariant, error) {
	ids := make([]int64, len(vs))
	for i, v := range vs {
		ids[i] = v.VariantID
	}
	versions, err := st.BandVersionsOfCatalogSongsFor(ctx, personID, ids, onlyBand)
	if err != nil || len(versions) == 0 {
		return vs, err
	}
	seen := map[int64]bool{}
	for _, v := range vs {
		if versions[v.VariantID] == nil {
			seen[v.VariantID] = true
		}
	}
	out := make([]CatalogVariant, 0, len(vs))
	for _, v := range vs {
		refs := versions[v.VariantID]
		if refs == nil {
			out = append(out, v)
			continue
		}
		for _, r := range refs {
			if seen[r.VariantID] {
				continue
			}
			seen[r.VariantID] = true
			band := r.BandID
			out = append(out, CatalogVariant{
				VariantID: r.VariantID, WorkID: v.WorkID, Title: r.Title, Artist: r.Artist,
				Visibility: "band", OwnerBandID: &band, Genre: v.Genre, Album: v.Album, BandName: r.BandName,
			})
		}
	}
	return out, nil
}
