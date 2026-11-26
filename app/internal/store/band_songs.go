package store

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"app/internal/setlist"
)

// A band's songs: a band's songbooks hold its own songs (owner_band_id),
// which every current member changes in place, and links to public songs
// (docs/ownership-model.md §4). Edit on a link makes the band's version
// (MakeVersion), which takes the link's place; nothing is copied up front.

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
		JOIN songs bs ON bs.owner_band_id IS NOT NULL AND (bs.id = c.forked_from_id OR bs.work_id = c.work_id)
		JOIN bands b ON b.id = bs.owner_band_id
		JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		WHERE c.id = ANY($2) AND c.owner_person_id = $1
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

// BandArrangementOf is bandID's own arrangement of songID (a song of the
// band's made from it, forked_from_id), when personID is a current member
// of the band: the shared song's page, opened from the band, points to it.
// nil when there's none.
func (st *Store) BandArrangementOf(ctx context.Context, personID, bandID, songID int64) (*BandVersion, error) {
	v := BandVersion{VariantID: songID, BandID: bandID}
	err := st.db.QueryRow(ctx, `
		SELECT b.name, s.id FROM songs s
		JOIN bands b ON b.id = s.owner_band_id
		JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		WHERE s.owner_band_id = $2 AND s.forked_from_id = $3 AND s.frozen_at IS NULL
		ORDER BY s.id LIMIT 1`, personID, bandID, songID).Scan(&v.BandName, &v.BandVariantID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &v, nil
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
		                 WHERE c.owner_band_id = b.id AND c.forked_from_id = s.id), 0)
		FROM songs s
		JOIN bands b ON true
		JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		WHERE s.id = $2
		  AND s.owner_band_id IS DISTINCT FROM b.id
		  AND (EXISTS (SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		               WHERE sb.owner_kind = 'band' AND sb.owner_band_id = b.id AND se.variant_id = s.id)
		       OR EXISTS (SELECT 1 FROM setlists sl JOIN setlist_items i ON i.setlist_id = sl.id
		                  WHERE sl.scope = 'band' AND sl.owner_band_id = b.id AND i.variant_id = s.id))
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
		SELECT EXISTS(SELECT 1 FROM songs WHERE id = $2 AND owner_band_id = $1)`,
		bandID, variantID).Scan(&ok)
	return ok, err
}

// VariantOwner says whose song variantID is, for the page that explains
// why someone can't change it: Kind is the song's kind ('global' for The
// Big Songbook's, 'user', 'band', 'community'), Name the band's,
// community's or person's name ("" for the catalogue).
type VariantOwner struct {
	Kind string
	Name string
}

func (st *Store) VariantOwnerOf(ctx context.Context, variantID int64) (*VariantOwner, error) {
	var o VariantOwner
	err := st.db.QueryRow(ctx, `
		SELECT `+songKindSQL("s")+`,
		       coalesce(b.name, CASE WHEN c.kind = 'global' THEN '' ELSE c.name END, p.display_name, '')
		FROM songs s
		LEFT JOIN bands b ON b.id = s.owner_band_id
		LEFT JOIN communities c ON c.id = s.owner_community_id
		LEFT JOIN people p ON p.id = s.owner_person_id
		WHERE s.id = $1`, variantID).Scan(&o.Kind, &o.Name)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("no such variant %d: %w", variantID, ErrNotFound)
	}
	return &o, err
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

// BandSongRef is a band's own version of a work.
type BandSongRef struct {
	VariantID     int64
	BandID        int64
	BandName      string
	Title, Artist string
}

// BandVersionsOfCatalogSongs finds, for each public song
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
		  JOIN songs bs ON bs.owner_band_id IS NOT NULL AND bs.work_id = g.work_id
		  JOIN bands b ON b.id = bs.owner_band_id
		  JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		  WHERE g.id = ANY($2) AND g.public AND g.owner_band_id IS NULL AND ($3 = 0 OR b.id = $3)
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
