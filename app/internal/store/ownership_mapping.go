package store

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"unicode"

	"github.com/jackc/pgx/v5"
	"golang.org/x/text/unicode/norm"
)

// The mapping of today's data onto the ownership model
// (docs/ownership-model.md §9 and §10), as cmd/ownership runs it: who owns
// each song, what is public, the band copies of November 2026 undone, the
// songs that are catalogue material moved into the catalogue, the paid
// songbooks reported closed. MapOwnership does it all in one transaction,
// and rolls it back unless told to apply it: a dry run is the same work,
// not a guess at it. A second run finds nothing to do. Migration 0114
// (step 2) does the mechanical rows first, so that the app never sees a
// song without an owner: the catalogue's songs to The Big Songbook,
// public; the songbooks that weren't shared closed. What's left for this
// command is what needs a decision (§10).

// OwnershipOptions are the decisions the mapping takes from outside the
// data: which bands' and communities' songs are catalogue material, and
// which of those songs is the one the catalogue keeps where it already
// has the same song.
type OwnershipOptions struct {
	Apply bool
	// CatalogueBands are bands (by name) whose songs are catalogue songs
	// that happen to sit in the band: they move into the catalogue,
	// public, owned by The Big Songbook; the band links to them (§9:
	// What's Up, Koalition).
	CatalogueBands []string
	// BandSongWins names songs ("Title — Artist") of CatalogueBands that
	// replace the catalogue's song of the same title and artist: the
	// catalogue's is merged into the band's (its links, set list lines,
	// recordings moved; §10.2: The Final Countdown, What's Up's chart).
	// The others' duplicates are left for the de-duplication review (§7).
	BandSongWins []string
	// CatalogueCommunities are communities (by name) whose songs are
	// catalogue material: each replaces the catalogue's chart of the same
	// song (a new revision, the old chart in its history), its songbook
	// entries (their keys kept) and everything else moved to the
	// catalogue's song, and goes; a song the catalogue doesn't have moves
	// into it (§10.3: Chloé Paul).
	CatalogueCommunities []string
}

// DefaultOwnershipOptions are §10's decisions.
func DefaultOwnershipOptions() OwnershipOptions {
	return OwnershipOptions{
		CatalogueBands:       []string{"What's Up", "Koalition"},
		BandSongWins:         []string{"The Final Countdown — Europe"},
		CatalogueCommunities: []string{"Chloé Paul"},
	}
}

// OwnerCount is a count of songs for one band or community.
type OwnerCount struct {
	ID   int64
	Name string
	N    int
}

// BandCopies is §9's "bandsongs copies" row for one band.
type BandCopies struct {
	BandID   int64
	BandName string
	Removed  int // unchanged copies gone, the band linking the catalogue's song again
	Kept     int // copies changed since, the band's versions now
	Moved    RepointCounts
}

// SongMove is one song moved, merged or replaced, for -v and the totals.
type SongMove struct {
	SongID   int64
	Title    string
	Artist   string
	Owner    string // the band's or community's name it came from
	TargetID int64  // the catalogue's song it went into (merged, replaced), 0 when it moved itself
	What     string // "to the catalogue", "merged", "replaces the catalogue's chart", "removed (unchanged copy)", "kept (changed copy)"
	Moved    RepointCounts
}

// OwnershipReport is what MapOwnership did (or, dry, would do), per row
// of §9's table.
type OwnershipReport struct {
	Applied       bool
	BigSongbookID int64

	// global (catalogue) → The Big Songbook, public.
	CatalogueSongs      int // songs the catalogue has, at the end
	CatalogueGiven      int // ownerless songs given to The Big Songbook
	CatalogueMadePublic int

	// user → the person, private.
	PersonalSongs int

	// bandsongs copies, unchanged → removed.
	BandCopies []BandCopies

	// Band songs that stay the band's, private (versions and originals).
	BandSongs []OwnerCount

	// What's Up, Koalition → the catalogue; The Final Countdown merged.
	ToCatalogue []SongMove
	Merged      []SongMove
	// Duplicates is "Title — Artist" for songs moved into the catalogue
	// that the catalogue had already, left for the de-duplication review.
	Duplicates []string

	// Chloé Paul's charts.
	Replaced             []SongMove
	CommunityToCatalogue []SongMove

	// Communities' songs, private.
	CommunitySongs []OwnerCount

	// Songbooks: shared → closed or not.
	ClosedSongbooks []OwnerCount // ID, Name (the songbook's), N: its songs
	PublicSongbooks int

	// Set lists: their rows (setlist_items, migration 0115).
	Setlists SetlistCounts

	// Lines are every song's line, for -v.
	Lines []string
}

// Nothing reports whether the run had nothing to change.
func (r *OwnershipReport) Nothing() bool {
	n := r.CatalogueGiven + r.CatalogueMadePublic +
		len(r.ToCatalogue) + len(r.Merged) + len(r.Replaced) + len(r.CommunityToCatalogue)
	for _, b := range r.BandCopies {
		n += b.Removed
	}
	return n == 0
}

// MapOwnership maps today's data onto the ownership model (see the
// file's comment). Without opts.Apply it rolls everything back.
func (st *Store) MapOwnership(ctx context.Context, opts OwnershipOptions) (*OwnershipReport, error) {
	r := &OwnershipReport{Applied: opts.Apply}
	// The Big Songbook's row (migration 0113 made it; a database emptied
	// since gets it again), outside the transaction a dry run rolls back.
	if err := st.db.QueryRow(ctx, `SELECT big_songbook_id()`).Scan(&r.BigSongbookID); err != nil {
		return nil, err
	}
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	// One mapping at a time.
	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7240611)`); err != nil {
		return nil, err
	}
	steps := []func(context.Context, pgx.Tx, *OwnershipReport, OwnershipOptions) error{
		st.mapCatalogue,
		st.mapPersonal,
		st.mapCatalogueBands,
		st.mapBandCopies,
		st.mapCatalogueCommunities,
		st.mapCounts,
		st.mapSongbooks,
		st.mapSetlists,
	}
	for _, step := range steps {
		if err := step(ctx, tx, r, opts); err != nil {
			return nil, err
		}
	}
	if opts.Apply {
		if err := tx.Commit(ctx); err != nil {
			return nil, err
		}
	}
	return r, nil
}

// mapCatalogue gives The Big Songbook the catalogue's songs (no owner
// today) and makes them public.
func (st *Store) mapCatalogue(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	tag, err := tx.Exec(ctx, `
		UPDATE songs SET owner_community_id = $1, public = true
		WHERE owner_person_id IS NULL AND owner_band_id IS NULL AND owner_community_id IS NULL`, r.BigSongbookID)
	if err != nil {
		return err
	}
	r.CatalogueGiven = int(tag.RowsAffected())
	tag, err = tx.Exec(ctx, `
		UPDATE songs SET public = true
		WHERE owner_community_id = $1 AND NOT public`, r.BigSongbookID)
	if err != nil {
		return err
	}
	r.CatalogueMadePublic = int(tag.RowsAffected())
	return nil
}

// mapPersonal counts a person's own songs: theirs, private (public's
// default), nothing to change.
func (st *Store) mapPersonal(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	return tx.QueryRow(ctx, `SELECT count(*) FROM songs WHERE owner_person_id IS NOT NULL`).Scan(&r.PersonalSongs)
}

// ownedSong is one song as the mapping looks at it.
type ownedSong struct {
	id, workID    int64
	title, artist string
	forkedFrom    *int64
	owner         string
}

func scanOwnedSongs(rows pgx.Rows) ([]ownedSong, error) {
	defer rows.Close()
	var out []ownedSong
	for rows.Next() {
		var s ownedSong
		if err := rows.Scan(&s.id, &s.workID, &s.title, &s.artist, &s.forkedFrom, &s.owner); err != nil {
			return nil, err
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// mapCatalogueBands moves the songs of opts.CatalogueBands into the
// catalogue; for opts.BandSongWins, the catalogue's own song of the same
// title and artist is merged into the band's.
func (st *Store) mapCatalogueBands(ctx context.Context, tx pgx.Tx, r *OwnershipReport, opts OwnershipOptions) error {
	if len(opts.CatalogueBands) == 0 {
		return nil
	}
	wins := map[string]bool{}
	for _, w := range opts.BandSongWins {
		title, artist, _ := strings.Cut(w, " — ")
		wins[songIdentity(title, artist)] = true
	}
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.forked_from_id, b.name
		FROM songs s JOIN bands b ON b.id = s.owner_band_id
		WHERE b.name = ANY($1) AND s.forked_from_id IS NULL
		ORDER BY b.name, s.title, s.id`, opts.CatalogueBands)
	if err != nil {
		return err
	}
	songs, err := scanOwnedSongs(rows)
	if err != nil {
		return err
	}
	for _, s := range songs {
		dups, err := catalogueMatches(ctx, tx, r.BigSongbookID, s)
		if err != nil {
			return err
		}
		if wins[songIdentity(s.title, s.artist)] {
			for _, d := range dups {
				moved, err := st.repointSong(ctx, tx, d.id, s.id, nil)
				if err != nil {
					return err
				}
				if err := deleteSong(ctx, tx, d.id); err != nil {
					return err
				}
				m := SongMove{SongID: d.id, Title: d.title, Artist: d.artist, Owner: "the catalogue", TargetID: s.id, What: "merged", Moved: moved}
				r.Merged = append(r.Merged, m)
				r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, the catalogue's): merged into %s's song %d, which becomes the catalogue's (%s)",
					d.id, d.title, d.artist, s.owner, s.id, moved))
			}
		} else {
			for _, d := range dups {
				r.Duplicates = append(r.Duplicates, fmt.Sprintf("%s — %s: the catalogue's %d and %s's %d", s.title, s.artist, d.id, s.owner, s.id))
			}
		}
		if _, err := tx.Exec(ctx, `
			UPDATE songs SET owner_band_id = NULL, owner_community_id = $2, public = true
			WHERE id = $1`, s.id, r.BigSongbookID); err != nil {
			return err
		}
		r.ToCatalogue = append(r.ToCatalogue, SongMove{SongID: s.id, Title: s.title, Artist: s.artist, Owner: s.owner, What: "to the catalogue"})
		r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, %s's): moves to the catalogue, public; %s links to it", s.id, s.title, s.artist, s.owner, s.owner))
	}
	return nil
}

// catalogueMatches lists the catalogue's songs that are s: s's own work,
// or the same title and artist once normalised (songIdentity), oldest
// first, s itself left out.
func catalogueMatches(ctx context.Context, tx pgx.Tx, bigSongbookID int64, s ownedSong) ([]ownedSong, error) {
	rows, err := tx.Query(ctx, `
		SELECT id, work_id, title, artist, forked_from_id, ''
		FROM songs WHERE owner_community_id = $1 AND id <> $2
		ORDER BY id`, bigSongbookID, s.id)
	if err != nil {
		return nil, err
	}
	all, err := scanOwnedSongs(rows)
	if err != nil {
		return nil, err
	}
	want := songIdentity(s.title, s.artist)
	var out []ownedSong
	for _, c := range all {
		if c.workID == s.workID || songIdentity(c.title, c.artist) == want {
			out = append(out, c)
		}
	}
	return out, nil
}

// mapBandCopies undoes cmd/bandsongs where the band never changed its
// copy: a band's song made from a catalogue song (forked_from_id), still
// as it was made (no revision, version 1, the same text, no recording or
// Guitar Pro file the catalogue's song lacks) goes, everything of the
// band's that pointed at it pointing at the catalogue's song again. A
// copy changed since stays: the band's version.
func (st *Store) mapBandCopies(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	rows, err := tx.Query(ctx, `
		SELECT c.id, c.work_id, c.title, c.artist, c.forked_from_id, b.name, b.id,
		       c.version = 1
		       AND NOT EXISTS (SELECT 1 FROM revisions rv WHERE rv.entity_type = 'variant' AND rv.entity_id = c.id::text)
		       AND c.title = g.title AND c.artist = g.artist AND c.reference_url = g.reference_url AND c.body = g.body
		       AND NOT EXISTS (SELECT 1 FROM song_audio a WHERE a.variant_id = c.id
		                       AND NOT EXISTS (SELECT 1 FROM song_audio x WHERE x.variant_id = g.id AND x.sha256 = a.sha256))
		       AND NOT EXISTS (SELECT 1 FROM song_gp_source_file f WHERE f.variant_id = c.id
		                       AND NOT EXISTS (SELECT 1 FROM song_gp_source_file x WHERE x.variant_id = g.id AND x.sha256 = f.sha256)
		                       AND NOT EXISTS (SELECT 1 FROM song_gp_source_file_version x WHERE x.variant_id = g.id AND x.sha256 = f.sha256))
		FROM songs c
		JOIN songs g ON g.id = c.forked_from_id AND g.owner_community_id = $1
		JOIN bands b ON b.id = c.owner_band_id
		ORDER BY b.name, b.id, c.title, c.id`, r.BigSongbookID)
	if err != nil {
		return err
	}
	type copyRow struct {
		ownedSong
		bandID    int64
		unchanged bool
	}
	var copies []copyRow
	for rows.Next() {
		var c copyRow
		if err := rows.Scan(&c.id, &c.workID, &c.title, &c.artist, &c.forkedFrom, &c.owner, &c.bandID, &c.unchanged); err != nil {
			rows.Close()
			return err
		}
		copies = append(copies, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	byBand := map[int64]*BandCopies{}
	var order []int64
	for _, c := range copies {
		bc := byBand[c.bandID]
		if bc == nil {
			bc = &BandCopies{BandID: c.bandID, BandName: c.owner}
			byBand[c.bandID] = bc
			order = append(order, c.bandID)
		}
		if !c.unchanged {
			bc.Kept++
			r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, %s's copy of %d): changed since, stays %s's version, private", c.id, c.title, c.artist, c.owner, *c.forkedFrom, c.owner))
			continue
		}
		moved, err := st.repointSong(ctx, tx, c.id, *c.forkedFrom, nil)
		if err != nil {
			return err
		}
		if err := deleteSong(ctx, tx, c.id); err != nil {
			return err
		}
		bc.Removed++
		bc.Moved.Add(moved)
		r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, %s's unchanged copy of %d): removed; %s links to %d again (%s)",
			c.id, c.title, c.artist, c.owner, *c.forkedFrom, c.owner, *c.forkedFrom, moved))
	}
	for _, id := range order {
		r.BandCopies = append(r.BandCopies, *byBand[id])
	}
	return nil
}

// mapCatalogueCommunities puts opts.CatalogueCommunities' charts in the
// catalogue: each replaces the catalogue's chart of the same song, or
// moves into the catalogue when it has none.
func (st *Store) mapCatalogueCommunities(ctx context.Context, tx pgx.Tx, r *OwnershipReport, opts OwnershipOptions) error {
	if len(opts.CatalogueCommunities) == 0 {
		return nil
	}
	rows, err := tx.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.forked_from_id, c.name
		FROM songs s JOIN communities c ON c.id = s.owner_community_id
		WHERE c.name = ANY($1) AND c.kind <> 'global'
		ORDER BY c.name, s.title, s.id`, opts.CatalogueCommunities)
	if err != nil {
		return err
	}
	songs, err := scanOwnedSongs(rows)
	if err != nil {
		return err
	}
	for _, s := range songs {
		target, err := catalogueSongFor(ctx, tx, r.BigSongbookID, s)
		if err != nil {
			return err
		}
		if target == nil {
			if _, err := tx.Exec(ctx, `
				UPDATE songs SET owner_community_id = $2, public = true
				WHERE id = $1`, s.id, r.BigSongbookID); err != nil {
				return err
			}
			r.CommunityToCatalogue = append(r.CommunityToCatalogue, SongMove{SongID: s.id, Title: s.title, Artist: s.artist, Owner: s.owner, What: "to the catalogue"})
			r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, %s's): the catalogue has no such song; moves to the catalogue, public", s.id, s.title, s.artist, s.owner))
			continue
		}
		if err := replaceChart(ctx, st, tx, s.id, target.id); err != nil {
			return err
		}
		moved, err := st.repointSong(ctx, tx, s.id, target.id, nil)
		if err != nil {
			return err
		}
		if err := deleteSong(ctx, tx, s.id); err != nil {
			return err
		}
		r.Replaced = append(r.Replaced, SongMove{SongID: s.id, Title: s.title, Artist: s.artist, Owner: s.owner, TargetID: target.id, What: "replaces the catalogue's chart", Moved: moved})
		r.Lines = append(r.Lines, fmt.Sprintf("song %d (%s — %s, %s's): its chart replaces the catalogue's song %d (a new revision); %s's copy goes (%s)",
			s.id, s.title, s.artist, s.owner, target.id, s.owner, moved))
	}
	return nil
}

// catalogueSongFor is the catalogue's song s replaces: the one s was
// copied from when that's the catalogue's, else the oldest of the same
// work or the same title and artist; nil when the catalogue has none.
func catalogueSongFor(ctx context.Context, tx pgx.Tx, bigSongbookID int64, s ownedSong) (*ownedSong, error) {
	if s.forkedFrom != nil {
		var ok bool
		if err := tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM songs WHERE id = $1 AND owner_community_id = $2)`, *s.forkedFrom, bigSongbookID).Scan(&ok); err != nil {
			return nil, err
		}
		if ok {
			return &ownedSong{id: *s.forkedFrom}, nil
		}
	}
	m, err := catalogueMatches(ctx, tx, bigSongbookID, s)
	if err != nil || len(m) == 0 {
		return nil, err
	}
	return &m[0], nil
}

// replaceChart makes from's chart (its text, and its reference link when
// it has one) the song to's, a revision of to's chart recorded first.
func replaceChart(ctx context.Context, st *Store, tx pgx.Tx, from, to int64) error {
	var title, artist, ref, body, fromRef, fromBody string
	if err := tx.QueryRow(ctx, `SELECT title, artist, reference_url, body FROM songs WHERE id = $1 FOR UPDATE`, to).
		Scan(&title, &artist, &ref, &body); err != nil {
		return err
	}
	if err := tx.QueryRow(ctx, `SELECT reference_url, body FROM songs WHERE id = $1`, from).Scan(&fromRef, &fromBody); err != nil {
		return err
	}
	newRef := ref
	if fromRef != "" {
		newRef = fromRef
	}
	if fromBody == body && newRef == ref {
		return nil
	}
	if err := st.RecordRevision(ctx, tx, "variant", strconv.FormatInt(to, 10), nil, VariantSnapshot{
		Title: title, Artist: artist, ReferenceURL: ref, Body: body,
	}); err != nil {
		return err
	}
	_, err := tx.Exec(ctx, `UPDATE songs SET body = $2, reference_url = $3, version = version + 1 WHERE id = $1`, to, fromBody, newRef)
	return err
}

// mapCounts counts what stays: bands' and communities' songs, private;
// the catalogue's; set list and jam songs.
func (st *Store) mapCounts(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	if err := tx.QueryRow(ctx, `SELECT count(*) FROM songs WHERE owner_community_id = $1`, r.BigSongbookID).Scan(&r.CatalogueSongs); err != nil {
		return err
	}
	counts := func(q string, args ...any) ([]OwnerCount, error) {
		rows, err := tx.Query(ctx, q, args...)
		if err != nil {
			return nil, err
		}
		defer rows.Close()
		var out []OwnerCount
		for rows.Next() {
			var c OwnerCount
			if err := rows.Scan(&c.ID, &c.Name, &c.N); err != nil {
				return nil, err
			}
			out = append(out, c)
		}
		return out, rows.Err()
	}
	var err error
	if r.BandSongs, err = counts(`
		SELECT b.id, b.name, count(*) FROM songs s JOIN bands b ON b.id = s.owner_band_id
		GROUP BY b.id, b.name ORDER BY b.name, b.id`); err != nil {
		return err
	}
	if r.CommunitySongs, err = counts(`
		SELECT c.id, c.name, count(*) FROM songs s JOIN communities c ON c.id = s.owner_community_id
		WHERE c.id <> $1 GROUP BY c.id, c.name ORDER BY c.name, c.id`, r.BigSongbookID); err != nil {
		return err
	}
	return nil
}

// mapSongbooks reports the closed songbooks: the ones that weren't shared
// (migration 0072: a community's songbook whose songs can be opened, not
// copied — the paid ones), which migration 0114 closed when it dropped
// shared; none is public yet.
func (st *Store) mapSongbooks(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	rows, err := tx.Query(ctx, `
		SELECT sb.id, coalesce(c.name || ': ', '') || sb.name, (SELECT count(*) FROM songbook_entries se WHERE se.songbook_id = sb.id)
		FROM songbooks sb LEFT JOIN communities c ON c.id = sb.owner_community_id
		WHERE sb.closed ORDER BY sb.id`)
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var c OwnerCount
		if err := rows.Scan(&c.ID, &c.Name, &c.N); err != nil {
			return err
		}
		r.ClosedSongbooks = append(r.ClosedSongbooks, c)
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return tx.QueryRow(ctx, `SELECT count(*) FROM songbooks WHERE public`).Scan(&r.PublicSongbooks)
}

// SetlistCounts is what the set lists hold: Lists set lists, Songs song
// rows (Notes of them with a note), Sections sets; Outside is the song
// rows whose song isn't in one of the set list's owner's songbooks (a set
// list lists its owner's songs, §4: a band's set list adds what it lists
// to the band's songbook; a person's may list any song they see).
type SetlistCounts struct {
	Lists, Songs, Sections, Notes, Outside int
}

// mapSetlists counts the set lists' rows; nothing to change: migration
// 0115 moved their bodies into setlist_items.
func (st *Store) mapSetlists(ctx context.Context, tx pgx.Tx, r *OwnershipReport, _ OwnershipOptions) error {
	c := &r.Setlists
	return tx.QueryRow(ctx, `
		SELECT (SELECT count(*) FROM setlists),
		       count(*) FILTER (WHERE i.variant_id IS NOT NULL),
		       count(*) FILTER (WHERE i.section IS NOT NULL),
		       count(*) FILTER (WHERE i.note <> ''),
		       count(*) FILTER (WHERE i.variant_id IS NOT NULL AND NOT EXISTS (
		         SELECT 1 FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		         WHERE se.variant_id = i.variant_id
		           AND sb.owner_person_id IS NOT DISTINCT FROM sl.owner_person_id
		           AND sb.owner_band_id IS NOT DISTINCT FROM sl.owner_band_id
		           AND sb.owner_community_id IS NOT DISTINCT FROM sl.owner_community_id))
		FROM setlist_items i JOIN setlists sl ON sl.id = i.setlist_id`).
		Scan(&c.Lists, &c.Songs, &c.Sections, &c.Notes, &c.Outside)
}

// String is "N songbook entries, N set list lines, N recordings, ...".
func (c RepointCounts) String() string {
	parts := []string{
		plural(c.Entries, "songbook entry", "songbook entries"),
		plural(c.SetlistItems, "set list line", "set list lines"),
		plural(c.Recordings, "recording", "recordings"),
	}
	if c.Media > 0 {
		parts = append(parts, plural(c.Media, "media file moved", "media files moved"))
	}
	if c.Other > 0 {
		parts = append(parts, plural(c.Other, "other reference", "other references"))
	}
	return strings.Join(parts, ", ")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return strconv.Itoa(n) + " " + many
}

// songIdentity normalises a title and an artist for "is this the same
// song" (§7): case, accents, a leading "The", "&" for "and", punctuation
// and spacing don't count.
func songIdentity(title, artist string) string {
	return normIdentityPart(title) + "|" + normIdentityPart(artist)
}

func normIdentityPart(s string) string {
	s = norm.NFD.String(strings.ToLower(s))
	var b strings.Builder
	for _, r := range s {
		switch {
		case unicode.Is(unicode.Mn, r):
		case r == '&':
			b.WriteString(" and ")
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		default:
			b.WriteRune(' ')
		}
	}
	words := strings.Fields(b.String())
	if len(words) > 1 && words[0] == "the" {
		words = words[1:]
	}
	return strings.Join(words, " ")
}
