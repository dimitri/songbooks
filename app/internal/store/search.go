package store

import (
	"context"
	"time"
)

// The search palette's finds (menu proposal §3.8): songs, songbooks, set
// lists, the person's bands and communities, their events, and a coach's
// students — each kind only as far as the person may see it, by the same
// rules as the pages it opens (CanViewVariant, CanAccessSongbook,
// CanViewSetlistByID, the calendar's for events, a coaching community's
// for students).

// SearchHit is one find.
type SearchHit struct {
	Kind     string // "song", "songbook", "setlist", "band", "community", "event" or "student"
	ID       int64
	Title    string
	Subtitle string // the artist, the band or community, …
	// The space it belongs to, when it belongs to one: the palette puts
	// the current space's finds first.
	BandID, CommunityID int64
	// A songbook's: the band's or community's default one, a student's.
	IsDefault bool
	// An event's start and type ("lesson" opens the lessons' page).
	At        *time.Time
	EventType string
	// A student's songbook's student, or a student hit's person.
	PersonID int64
	// A band's song: the band's name ("Mercy" for Mercy's version).
	BandName string
}

// searchCandidates is how many rows each kind reads before the access
// checks keep at most `limit` of them.
const searchCandidates = 40

// searchScope is the person's spaces, for every kind's query: their
// current bands and communities, and the communities they manage.
const searchScope = `
	WITH my_bands AS (
	  SELECT band_id FROM band_members WHERE person_id = $1 AND upper_inf(validity)
	), my_communities AS (
	  SELECT community_id FROM community_members WHERE person_id = $1 AND upper_inf(validity)
	), my_managed AS (
	  SELECT cm.community_id FROM community_members cm
	  JOIN community_role_privileges crp ON crp.role_id = cm.role_id
	  WHERE cm.person_id = $1 AND upper_inf(cm.validity)
	    AND crp.privilege IN ('` + PrivManageMembers + `', '` + PrivManageEvents + `', '` + PrivManageSubmissions + `', '` + PrivManageSettings + `', '` + PrivManageFiles + `')
	)`

// Search finds what personID may see matching q (titles, names, artists;
// case, accents and punctuation folded as the library's matching does),
// at most limit of each kind, the ones starting with q first.
func (st *Store) Search(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	var norm string
	if err := st.db.QueryRow(ctx, `SELECT `+matchNormSQL(`$1::text`), q).Scan(&norm); err != nil {
		return nil, err
	}
	if len([]rune(norm)) < 2 {
		return nil, nil
	}
	var out []SearchHit
	for _, kind := range []func(context.Context, int64, string, int) ([]SearchHit, error){
		st.searchSongs, st.searchSongbooks, st.searchSetlists, st.searchGroups, st.searchEvents, st.searchStudents,
	} {
		hits, err := kind(ctx, personID, norm, limit)
		if err != nil {
			return nil, err
		}
		out = append(out, hits...)
	}
	return out, nil
}

// matchSQL is the WHERE test and the ORDER BY key for one column against
// the normalized query ($2): a word starting with it, first.
func matchSQL(col string) (where, order string) {
	n := matchNormSQL(col)
	return n + ` LIKE '%' || $2 || '%'`,
		`(` + n + ` LIKE $2 || '%') DESC, (' ' || ` + n + ` LIKE '% ' || $2 || '%') DESC`
}

func (st *Store) searchSongs(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	title, order := matchSQL("s.title")
	artist, _ := matchSQL("s.artist")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT s.id, s.title, s.artist, `+songKindSQL("s")+`, coalesce(s.owner_band_id, 0), coalesce(s.owner_community_id, 0),
		       coalesce((SELECT b.name FROM bands b WHERE b.id = s.owner_band_id), '')
		FROM songs s
		WHERE (`+title+` OR `+artist+`)
		  AND song_visible(s.id, $1)
		ORDER BY `+order+`, s.public, s.title, s.id
		LIMIT $3`, personID, q, searchCandidates)
	if err != nil {
		return nil, err
	}
	type cand struct {
		hit        SearchHit
		visibility string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		c.hit.Kind = "song"
		if err := rows.Scan(&c.hit.ID, &c.hit.Title, &c.hit.Subtitle, &c.visibility, &c.hit.BandID, &c.hit.CommunityID, &c.hit.BandName); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	// A band member finds their band's version of a song in place of the
	// catalogue's, one per band (BandVersionsOfCatalogSongs).
	var globals []int64
	for _, c := range cands {
		if c.visibility == "global" {
			globals = append(globals, c.hit.ID)
		}
	}
	versions, err := st.BandVersionsOfCatalogSongs(ctx, personID, globals)
	if err != nil {
		return nil, err
	}
	if len(versions) > 0 {
		listed := map[int64]bool{}
		for _, c := range cands {
			if versions[c.hit.ID] == nil {
				listed[c.hit.ID] = true
			}
		}
		var with []cand
		for _, c := range cands {
			refs := versions[c.hit.ID]
			if refs == nil {
				with = append(with, c)
				continue
			}
			for _, b := range refs {
				if listed[b.VariantID] {
					continue // found by its own title already
				}
				listed[b.VariantID] = true
				with = append(with, cand{hit: SearchHit{Kind: "song", ID: b.VariantID, Title: b.Title, Subtitle: b.Artist, BandID: b.BandID, BandName: b.BandName}, visibility: "band"})
			}
		}
		cands = with
	}
	var out []SearchHit
	for _, c := range cands {
		if len(out) == limit {
			break
		}
		out = append(out, c.hit)
	}
	return out, nil
}

func (st *Store) searchSongbooks(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	where, order := matchSQL("sb.name")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT sb.id, sb.name, coalesce(b.name, c.name, ''), coalesce(sb.owner_band_id, 0), coalesce(sb.owner_community_id, 0),
		       sb.is_default, sb.kind, coalesce(sb.student_person_id, 0)
		FROM songbooks sb
		LEFT JOIN bands b ON b.id = sb.owner_band_id
		LEFT JOIN communities c ON c.id = sb.owner_community_id
		WHERE ((sb.owner_kind = 'person' AND sb.owner_person_id = $1)
		       OR (sb.owner_kind = 'band' AND sb.owner_band_id IN (SELECT band_id FROM my_bands))
		       OR (sb.owner_kind = 'community' AND sb.owner_community_id IN (SELECT community_id FROM my_communities))
		       OR sb.public)
		  AND `+where+`
		ORDER BY `+order+`, sb.name, sb.id
		LIMIT $3`, personID, q, searchCandidates)
	if err != nil {
		return nil, err
	}
	type cand struct {
		hit  SearchHit
		kind string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		c.hit.Kind = "songbook"
		if err := rows.Scan(&c.hit.ID, &c.hit.Title, &c.hit.Subtitle, &c.hit.BandID, &c.hit.CommunityID, &c.hit.IsDefault, &c.kind, &c.hit.PersonID); err != nil {
			rows.Close()
			return nil, err
		}
		cands = append(cands, c)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []SearchHit
	for _, c := range cands {
		if len(out) == limit {
			break
		}
		// A student's songbook is on their page: theirs to find (the
		// coach finds the student), as the songbooks' page lists them.
		if c.kind == SongbookKindStudent && c.hit.PersonID != personID {
			continue
		}
		ok, err := st.CanAccessSongbook(ctx, personID, c.hit.ID)
		if err != nil {
			return nil, err
		}
		if ok {
			out = append(out, c.hit)
		}
	}
	return out, nil
}

func (st *Store) searchSetlists(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	where, order := matchSQL("sl.title")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT sl.id, sl.title, coalesce(b.name, c.name, ''), coalesce(sl.owner_band_id, 0), coalesce(sl.owner_community_id, 0)
		FROM setlists sl
		LEFT JOIN bands b ON b.id = sl.owner_band_id
		LEFT JOIN communities c ON c.id = sl.owner_community_id
		WHERE ((sl.scope = 'person' AND sl.owner_person_id = $1)
		       OR (sl.scope = 'band' AND sl.owner_band_id IN (SELECT band_id FROM my_bands))
		       OR (sl.scope = 'community' AND sl.owner_community_id IN (SELECT community_id FROM my_communities)))
		  AND `+where+`
		ORDER BY `+order+`, sl.title, sl.id
		LIMIT $3`, personID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "setlist"}
		if err := rows.Scan(&h.ID, &h.Title, &h.Subtitle, &h.BandID, &h.CommunityID); err != nil {
			return nil, err
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// searchGroups finds the person's own bands and communities.
func (st *Store) searchGroups(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	where, order := matchSQL("g.name")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT g.kind, g.id, g.name FROM (
		  SELECT 'band' AS kind, b.id, b.name FROM bands b WHERE b.id IN (SELECT band_id FROM my_bands)
		  UNION ALL
		  SELECT 'community', c.id, c.name FROM communities c WHERE c.id IN (SELECT community_id FROM my_communities)
		) g
		WHERE `+where+`
		ORDER BY `+order+`, g.name, g.id
		LIMIT $3`, personID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		var h SearchHit
		if err := rows.Scan(&h.Kind, &h.ID, &h.Title); err != nil {
			return nil, err
		}
		if h.Kind == "band" {
			h.BandID = h.ID
		} else {
			h.CommunityID = h.ID
		}
		out = append(out, h)
	}
	return out, rows.Err()
}

// searchEvents finds the events of the person's bands and communities, as
// their calendar shows them (a lesson: its coach's and students', and
// whoever manages the community), from two months back, the coming ones
// first.
func (st *Store) searchEvents(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	where, order := matchSQL("e.name")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT e.id, e.name, coalesce(b.name, c.name, ''), coalesce(e.owner_band_id, 0), coalesce(e.owner_community_id, 0),
		       e.starts_at, e.event_type::text
		FROM events e
		LEFT JOIN bands b ON b.id = e.owner_band_id
		LEFT JOIN communities c ON c.id = e.owner_community_id
		WHERE (e.owner_band_id IN (SELECT band_id FROM my_bands)
		       OR e.owner_community_id IN (SELECT community_id FROM my_communities))
		  AND (e.event_type <> 'lesson' OR e.created_by_person_id = $1
		       OR e.owner_community_id IN (SELECT community_id FROM my_managed)
		       OR EXISTS (SELECT 1 FROM event_registrations r WHERE r.event_id = e.id AND r.person_id = $1))
		  AND e.starts_at > now() - interval '60 days'
		  AND `+where+`
		ORDER BY `+order+`, (e.starts_at < now() - interval '6 hours'), abs(extract(epoch FROM e.starts_at - now())), e.id
		LIMIT $3`, personID, q, searchCandidates)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	// A weekly lesson or rehearsal is one find: its next date.
	type key struct {
		name       string
		band, comm int64
	}
	seen := map[key]bool{}
	var out []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "event"}
		var at time.Time
		if err := rows.Scan(&h.ID, &h.Title, &h.Subtitle, &h.BandID, &h.CommunityID, &at, &h.EventType); err != nil {
			return nil, err
		}
		k := key{h.Title, h.BandID, h.CommunityID}
		if seen[k] || len(out) == limit {
			continue
		}
		seen[k] = true
		h.At = &at
		out = append(out, h)
	}
	return out, rows.Err()
}

// searchStudents finds a coach's students: the current members, not
// staff, of the coaching communities they manage.
func (st *Store) searchStudents(ctx context.Context, personID int64, q string, limit int) ([]SearchHit, error) {
	where, order := matchSQL("p.display_name")
	rows, err := st.db.Query(ctx, searchScope+`
		SELECT p.id, p.display_name, c.name, c.id
		FROM community_members m
		JOIN people p ON p.id = m.person_id
		JOIN community_roles cr ON cr.id = m.role_id
		JOIN communities c ON c.id = m.community_id
		WHERE m.community_id IN (SELECT community_id FROM my_managed) AND c.kind = 'coaching'
		  AND upper_inf(m.validity) AND cr.name <> 'staff' AND m.person_id <> $1
		  AND `+where+`
		ORDER BY `+order+`, p.display_name, p.id
		LIMIT $3`, personID, q, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SearchHit
	for rows.Next() {
		h := SearchHit{Kind: "student"}
		if err := rows.Scan(&h.PersonID, &h.Title, &h.Subtitle, &h.CommunityID); err != nil {
			return nil, err
		}
		h.ID = h.PersonID
		out = append(out, h)
	}
	return out, rows.Err()
}
