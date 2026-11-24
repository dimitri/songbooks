package store

import (
	"context"
	"sort"
	"strings"
)

// Finding songs (docs/ownership-model.md §1, §3, §6; steps 4–5): one
// search over everything a person sees — their own songs, their bands'
// and communities', every public one — each song with whose it is and
// who sees it ("Mercy · private", "Other Lies · original · public",
// "Yours · private", "The Big Songbook · public"), the versions of one
// work together. The catalogue page, the Add / Create dialog, the Add
// to… picker and The Big Songbook's page all read it.

// Find scopes: what FindFilter.Scope narrows the listing to.
const (
	FindAll         = ""
	FindMine        = "mine"        // the person's own songs
	FindBands       = "bands"       // their bands' songs
	FindCommunities = "communities" // their communities' songs (not The Big Songbook's)
	FindPublic      = "public"      // every public song
)

// The Big Songbook's page: where its public songs come from.
const (
	FromCatalogue   = "catalogue"   // The Big Songbook's own (the catalogue)
	FromBands       = "bands"       // a band's public songs
	FromCommunities = "communities" // a community's public songs
)

// FindFilter narrows FindSongs. Zero values: no filter.
type FindFilter struct {
	// Query: words of the title or the artist (case, accents and
	// punctuation folded, as the search palette folds them), or of the
	// lyrics (full text).
	Query string
	Scope string // FindAll, FindMine, FindBands, FindCommunities, FindPublic
	// OnlyPublic with From: The Big Songbook's page (every public song,
	// FromCatalogue / FromBands / FromCommunities, "" all).
	OnlyPublic bool
	From       string
	Artist     string // exact artist
	Genre      string // the work's genre
	Sort       string // "artist": by artist then title; else by title
	// Songbook: the songbook the search is for (the Add / Create dialog):
	// each song says whether it's in it already and whether it may be
	// linked there (SongRights.CanLinkFrom for its owner); 0 none.
	Songbook *Songbook
	// Limit caps the works listed (0: all).
	Limit int
}

// FoundSong is one song found, with what its badges say.
type FoundSong struct {
	ID          int64  `json:"id"`
	WorkID      int64  `json:"workId"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	VersionName string `json:"versionName,omitempty"`
	// Whose it is: "person", "band" or "community", its id and name.
	OwnerKind string `json:"ownerKind"`
	OwnerID   int64  `json:"ownerId"`
	OwnerName string `json:"ownerName"`
	// Mine: the person's own song ("Yours"); Catalogue: The Big
	// Songbook's; Original: a band of originals' own song (not a version
	// of anything).
	Mine      bool `json:"mine,omitempty"`
	Catalogue bool `json:"catalogue,omitempty"`
	Original  bool `json:"original,omitempty"`
	Public    bool `json:"public"`
	// Closed: in a closed (paid) songbook of its owner's: it goes nowhere
	// else (§2).
	Closed       bool   `json:"closed,omitempty"`
	ForkedFromID *int64 `json:"forkedFromId,omitempty"`
	Genre        string `json:"genre,omitempty"`
	Album        string `json:"album,omitempty"`
	// For FindFilter.Songbook: in it already; may be linked there (else
	// why not: "closed" or "private").
	InSongbook bool   `json:"inSongbook,omitempty"`
	Linkable   bool   `json:"linkable"`
	Refusal    string `json:"refusal,omitempty"`
	// RecordingFit: does the song follow its reference recording (§3,
	// flags only): "follows", "differs" or "" (not known); set by the
	// pages that show it.
	RecordingFit string `json:"recordingFit,omitempty"`
	FitDetail    string `json:"fitDetail,omitempty"`
	rank         int
}

// FoundWork is one work found and its versions, the one a person most
// likely wants first.
type FoundWork struct {
	WorkID int64       `json:"workId"`
	Title  string      `json:"title"`
	Artist string      `json:"artist"`
	Songs  []FoundSong `json:"songs"`
}

// FindSongs lists what personID sees (song_visible), as filter narrows
// it, by work: the works in title (or artist) order, the ones whose title
// starts with the query first.
func (st *Store) FindSongs(ctx context.Context, personID int64, filter FindFilter) ([]FoundWork, error) {
	q := strings.TrimSpace(filter.Query)
	var norm string
	if q != "" {
		if err := st.db.QueryRow(ctx, `SELECT `+matchNormSQL(`$1::text`), q).Scan(&norm); err != nil {
			return nil, err
		}
	}
	var sbID int64
	if filter.Songbook != nil {
		sbID = filter.Songbook.ID
	}
	title, artist := matchNormSQL("s.title"), matchNormSQL("s.artist")
	rows, err := st.db.Query(ctx, `
		WITH my_bands AS (
		  SELECT band_id FROM band_members WHERE person_id = $1 AND upper_inf(validity)
		), my_communities AS (
		  SELECT community_id FROM community_members WHERE person_id = $1 AND upper_inf(validity)
		)
		SELECT s.id, s.work_id, s.title, s.artist, s.version_name, s.owner_kind,
		       coalesce(s.owner_person_id, s.owner_band_id, s.owner_community_id),
		       coalesce(p.display_name, b.name, c.name, ''),
		       s.owner_person_id IS NOT DISTINCT FROM $1, coalesce(c.kind = 'global', false),
		       coalesce(b.music_type = 'original', false) AND s.forked_from_id IS NULL,
		       s.public, song_closed(s.id), s.forked_from_id, coalesce(w.genre, ''), coalesce(w.album, ''),
		       $9 > 0 AND EXISTS (SELECT 1 FROM songbook_entries se WHERE se.songbook_id = $9 AND se.variant_id = s.id),
		       CASE WHEN $2 = '' THEN 0
		            WHEN `+title+` LIKE $2 || '%' THEN 3
		            WHEN ' ' || `+title+` LIKE '% ' || $2 || '%' THEN 2
		            WHEN `+title+` LIKE '%' || $2 || '%' OR `+artist+` LIKE '%' || $2 || '%' THEN 1
		            ELSE 0 END
		FROM songs s
		JOIN works w ON w.id = s.work_id
		LEFT JOIN people p ON p.id = s.owner_person_id
		LEFT JOIN bands b ON b.id = s.owner_band_id
		LEFT JOIN communities c ON c.id = s.owner_community_id
		WHERE song_visible(s.id, $1)
		  AND ($2 = '' OR `+title+` LIKE '%' || $2 || '%' OR `+artist+` LIKE '%' || $2 || '%'
		       OR s.search @@ plainto_tsquery('english', $3))
		  AND CASE $4
		        WHEN 'mine' THEN s.owner_person_id = $1
		        WHEN 'bands' THEN s.owner_band_id IN (SELECT band_id FROM my_bands)
		        WHEN 'communities' THEN s.owner_community_id IN (SELECT community_id FROM my_communities)
		        WHEN 'public' THEN s.public
		        ELSE true END
		  AND (NOT $5 OR s.public)
		  AND CASE $6
		        WHEN 'catalogue' THEN coalesce(c.kind = 'global', false)
		        WHEN 'bands' THEN s.owner_band_id IS NOT NULL
		        WHEN 'communities' THEN s.owner_community_id IS NOT NULL AND c.kind <> 'global'
		        ELSE true END
		  AND ($7 = '' OR s.artist = $7)
		  AND ($8 = '' OR w.genre = $8)`,
		personID, norm, q, filter.Scope, filter.OnlyPublic, filter.From, filter.Artist, filter.Genre, sbID)
	if err != nil {
		return nil, err
	}
	var songs []FoundSong
	for rows.Next() {
		var f FoundSong
		if err := rows.Scan(&f.ID, &f.WorkID, &f.Title, &f.Artist, &f.VersionName, &f.OwnerKind, &f.OwnerID, &f.OwnerName,
			&f.Mine, &f.Catalogue, &f.Original, &f.Public, &f.Closed, &f.ForkedFromID, &f.Genre, &f.Album,
			&f.InSongbook, &f.rank); err != nil {
			rows.Close()
			return nil, err
		}
		songs = append(songs, f)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if filter.Songbook != nil {
		target := filter.Songbook.Owner()
		for i := range songs {
			songs[i].Linkable, songs[i].Refusal = linkableFrom(songs[i], target)
		}
	}
	return groupFound(songs, filter, personID), nil
}

// linkableFrom is SongRights.CanLinkFrom for a song found (seen, then):
// its own owner's songbooks hold it; a closed songbook's song goes
// nowhere else; a band or a community links public songs, a person
// anything they see.
func linkableFrom(f FoundSong, target Owner) (bool, string) {
	r := SongRights{See: true, Owner: Owner{Kind: f.OwnerKind, ID: f.OwnerID}, Public: f.Public, Closed: f.Closed}
	if r.CanLinkFrom(target) {
		return true, ""
	}
	if f.Closed {
		return false, "closed"
	}
	return false, "private"
}

// groupFound puts the songs of one work together: the works by their best
// match, then title (or artist); in a work, the one in the songbook
// first, then the person's own, their groups', The Big Songbook's, the
// other public ones.
func groupFound(songs []FoundSong, filter FindFilter, personID int64) []FoundWork {
	byWork := map[int64]*FoundWork{}
	var order []int64
	best := map[int64]int{}
	for _, s := range songs {
		w := byWork[s.WorkID]
		if w == nil {
			w = &FoundWork{WorkID: s.WorkID}
			byWork[s.WorkID] = w
			order = append(order, s.WorkID)
		}
		w.Songs = append(w.Songs, s)
		if s.rank > best[s.WorkID] {
			best[s.WorkID] = s.rank
		}
	}
	weight := func(s FoundSong) int {
		switch {
		case s.InSongbook:
			return 0
		case s.Mine:
			return 1
		case !s.Public && !s.Catalogue:
			return 2 // a band's or a community's of the person's
		case s.Catalogue:
			return 3
		}
		return 4
	}
	out := make([]FoundWork, 0, len(order))
	for _, id := range order {
		w := byWork[id]
		sort.SliceStable(w.Songs, func(i, j int) bool {
			a, b := w.Songs[i], w.Songs[j]
			if weight(a) != weight(b) {
				return weight(a) < weight(b)
			}
			if a.rank != b.rank {
				return a.rank > b.rank
			}
			if a.Artist != b.Artist {
				return strings.ToLower(a.Artist) < strings.ToLower(b.Artist)
			}
			return a.ID < b.ID
		})
		w.Title, w.Artist = w.Songs[0].Title, w.Songs[0].Artist
		out = append(out, *w)
	}
	key := func(w FoundWork) string {
		if filter.Sort == "artist" {
			return strings.ToLower(w.Artist + "\x00" + w.Title)
		}
		return strings.ToLower(w.Title + "\x00" + w.Artist)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if bi, bj := best[out[i].WorkID], best[out[j].WorkID]; bi != bj {
			return bi > bj
		}
		ki, kj := key(out[i]), key(out[j])
		if ki != kj {
			return ki < kj
		}
		return out[i].WorkID < out[j].WorkID
	})
	if filter.Limit > 0 && len(out) > filter.Limit {
		out = out[:filter.Limit]
	}
	return out
}

// SongbookTarget is a songbook a person may add songs to: the Add /
// Create dialog's "Goes into", the Add to… picker.
type SongbookTarget struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	OwnerKind string `json:"ownerKind"`
	OwnerID   int64  `json:"ownerId"`
	OwnerName string `json:"ownerName"`
	Mine      bool   `json:"mine,omitempty"` // the person's own
	IsDefault bool   `json:"isDefault,omitempty"`
	Closed    bool   `json:"closed,omitempty"`
	Kind      string `json:"kind,omitempty"`
	// CanPublish: a song made here may be public ("Who sees it"):
	// SongbookRights.Publish — the person's own; a band's core members; a
	// community's staff; never a closed songbook.
	CanPublish bool `json:"canPublish"`
	// For a song (SongbookTargetsFor): in it already; may be linked
	// there (else why not).
	InSongbook bool   `json:"inSongbook,omitempty"`
	Linkable   bool   `json:"linkable"`
	Refusal    string `json:"refusal,omitempty"`
}

// SongbookTargets lists the songbooks personID may add to (CanEditSongbook):
// their own, their bands', their communities' where they edit the
// repertoire. A student's songbook (a coach's) only when it is keep
// (the one the dialog was opened from); keep 0 none.
func (st *Store) SongbookTargets(ctx context.Context, personID, keep int64) ([]SongbookTarget, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sb.id, sb.name, sb.owner_kind, coalesce(sb.owner_person_id, sb.owner_band_id, sb.owner_community_id),
		       coalesce(p.display_name, b.name, c.name, ''), sb.is_default, sb.closed, sb.kind
		FROM songbooks sb
		LEFT JOIN people p ON p.id = sb.owner_person_id
		LEFT JOIN bands b ON b.id = sb.owner_band_id
		LEFT JOIN communities c ON c.id = sb.owner_community_id
		WHERE (sb.owner_kind = 'person' AND sb.owner_person_id = $1)
		   OR (sb.owner_kind = 'band' AND has_band_privilege(sb.owner_band_id, $1, $4))
		   OR (sb.owner_kind = 'community' AND sb.owner_community_id IN (
		         SELECT cm.community_id FROM community_members cm
		         JOIN community_role_privileges crp ON crp.role_id = cm.role_id
		         WHERE cm.person_id = $1 AND upper_inf(cm.validity) AND crp.privilege = $2)
		       AND (sb.kind <> 'student' OR sb.id = $3))
		ORDER BY (sb.owner_kind = 'person') DESC, sb.owner_kind, 5, NOT sb.is_default, sb.name, sb.id`,
		personID, PrivEditRepertoire, keep, BandPrivEditSongbook)
	if err != nil {
		return nil, err
	}
	var cands []SongbookTarget
	for rows.Next() {
		var t SongbookTarget
		if err := rows.Scan(&t.ID, &t.Name, &t.OwnerKind, &t.OwnerID, &t.OwnerName, &t.IsDefault, &t.Closed, &t.Kind); err != nil {
			rows.Close()
			return nil, err
		}
		t.Mine = t.OwnerKind == "person"
		cands = append(cands, t)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []SongbookTarget
	for _, t := range cands {
		r, err := st.SongbookRightsFor(ctx, personID, t.ID)
		if err != nil {
			return nil, err
		}
		if !r.Edit {
			continue
		}
		t.CanPublish = r.Publish
		out = append(out, t)
	}
	return out, nil
}

// SongbookTargetsFor is SongbookTargets for one song: each songbook says
// whether it holds the song already and whether it may (CanLinkFrom).
func (st *Store) SongbookTargetsFor(ctx context.Context, personID, songID, keep int64) ([]SongbookTarget, error) {
	r, err := st.SongRightsFor(ctx, personID, songID)
	if err != nil {
		return nil, err
	}
	if !r.See {
		return nil, ErrForbidden
	}
	targets, err := st.SongbookTargets(ctx, personID, keep)
	if err != nil {
		return nil, err
	}
	in := map[int64]bool{}
	rows, err := st.db.Query(ctx, `SELECT songbook_id FROM songbook_entries WHERE variant_id = $1`, songID)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			rows.Close()
			return nil, err
		}
		in[id] = true
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for i := range targets {
		t := &targets[i]
		t.InSongbook = in[t.ID]
		t.Linkable = r.CanLinkFrom(Owner{Kind: t.OwnerKind, ID: t.OwnerID})
		if !t.Linkable {
			t.Refusal = "private"
			if r.Closed {
				t.Refusal = "closed"
			}
		}
	}
	return targets, nil
}

// CreateSongIn makes a new song in songbookID (§1: a song is always
// created in a songbook of its owner): owned by the songbook's owner,
// private unless public is asked and personID may publish there
// (SongbookRights.Publish; never in a closed songbook), added to that
// songbook only. ErrForbidden when personID can't add to it.
func (st *Store) CreateSongIn(ctx context.Context, personID, songbookID int64, title, artist, referenceURL string, public bool) (int64, error) {
	r, err := st.SongbookRightsFor(ctx, personID, songbookID)
	if err != nil {
		return 0, err
	}
	if !r.Edit {
		return 0, ErrForbidden
	}
	if public && !r.Publish {
		return 0, ErrForbidden
	}
	sb := r.Songbook
	tx, err := st.db.Begin(ctx)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(ctx)
	workID, err := st.findOrCreateWork(ctx, tx, title, artist)
	if err != nil {
		return 0, err
	}
	var b strings.Builder
	b.WriteString("title: " + title + "\n")
	if artist != "" {
		b.WriteString("artist: " + artist + "\n")
	}
	if referenceURL != "" {
		b.WriteString("reference: " + referenceURL + "\n")
	}
	b.WriteString("\n[Verse]\n")
	var person, band, community *int64
	switch o := sb.Owner(); o.Kind {
	case "band":
		band = &o.ID
	case "community":
		community = &o.ID
	default:
		person = &o.ID
	}
	var id int64
	if err := tx.QueryRow(ctx, `
		INSERT INTO songs (title, artist, reference_url, body, work_id, owner_person_id, owner_band_id, owner_community_id,
		                   created_by_person_id, public)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10) RETURNING id`,
		title, artist, referenceURL, b.String(), workID, person, band, community, personID, public).Scan(&id); err != nil {
		return 0, err
	}
	if _, err := tx.Exec(ctx, `
		INSERT INTO songbook_entries (songbook_id, variant_id, added_by_person_id) VALUES ($1, $2, $3)`,
		songbookID, id, personID); err != nil {
		return 0, err
	}
	return id, tx.Commit(ctx)
}

// SetCatalogueReference gives a song just made (no revision yet) its
// reference video: the column and the chart's header line.
func (st *Store) SetCatalogueReference(ctx context.Context, songID int64, referenceURL string) error {
	_, err := st.db.Exec(ctx, `
		UPDATE songs SET reference_url = $2,
		       body = regexp_replace(body, '^((title|artist): [^\n]*\n)*', '\&reference: ' || $2 || E'\n')
		WHERE id = $1`, songID, referenceURL)
	return err
}
