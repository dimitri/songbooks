package store

import (
	"context"
	"strings"
)

// LibraryMatch is one "this song may already be in the library" hit for
// a new-song form: the variant plus the few facts that tell two versions
// of the same song apart.
type LibraryMatch struct {
	VariantID  int64
	Title      string
	Artist     string
	Key        string // from the song's own "key:" header line; "" if unset
	Visibility string // "global", or "user" for the viewer's own private song
	MadeBy     string // owner's username, else whoever created it; "" if unknown
	Bands      int    // how many bands have it in their repertoire
	Songbooks  int    // how many songbooks (any owner) contain it
	Exact      bool   // normalized title is identical, not just a prefix/word match
}

// accentFold pairs accented Latin letters with the plain letter they fold
// to. Built into translate()'s two argument strings below so they always
// line up character for character.
var accentFold = []string{
	"àáâãäåāăą", "a", "çćĉč", "c", "ď", "d", "èéêëēĕėęě", "e", "ìíîïĩīį", "i",
	"ñńň", "n", "òóôõöøōŏő", "o", "ùúûüũūŭůűų", "u", "ýÿ", "y", "źżž", "z",
	"śšşß", "s", "ğ", "g",
}

var accentFrom, accentTo = func() (string, string) {
	var from, to strings.Builder
	for i := 0; i < len(accentFold); i += 2 {
		for _, r := range accentFold[i] {
			from.WriteRune(r)
			to.WriteString(accentFold[i+1])
		}
	}
	return from.String(), to.String()
}()

// matchNormSQL folds a SQL text expression to the key titles and artists
// are compared on: lower case, accents stripped, every run of punctuation
// or spaces collapsed to one space, and a single leading article dropped
// ("The", and French "le/la/les/l'" since most people here are French).
// An English contraction's apostrophe is dropped rather than spaced
// ("Ain't" → "aint", how people type it), a French elision's isn't
// ("l'amour" → "l amour", so the article can go).
// Done in SQL rather than Go so the typed text and the stored titles go
// through the very same function — no unaccent extension needed.
func matchNormSQL(expr string) string {
	return `regexp_replace(btrim(regexp_replace(translate(replace(replace(regexp_replace(lower(` + expr + `), '[''’](t|s|re|ll|ve|d|m)\y', '\1', 'g'), 'œ', 'oe'), 'æ', 'ae'), '` +
		accentFrom + `', '` + accentTo + `'), '[^a-z0-9]+', ' ', 'g')), '^(the|le|la|les|l) ', '')`
}

// MatchLibrary finds songs in the library (every global song, plus
// viewerID's own private ones — the same scope as ListCatalog) whose
// title matches title once both are normalized: identical first, then
// "starts with" (someone still typing), then a word inside the title.
// artist, when given, only boosts — a cover credited to someone else is
// still the same song. Unlike ListCatalog's full-text search this never
// matches on lyrics, which made "is this already here?" noisy.
func (st *Store) MatchLibrary(ctx context.Context, viewerID int64, title, artist string, limit int) ([]LibraryMatch, error) {
	rows, err := st.db.Query(ctx, `
		WITH q AS (SELECT `+matchNormSQL("$2::text")+` AS t, `+matchNormSQL("$3::text")+` AS a),
		c AS (
		  SELECT s.id, `+matchNormSQL("s.title")+` AS nt, `+matchNormSQL("s.artist")+` AS na
		  FROM songs s
		  WHERE s.visibility = 'global' OR (s.visibility = 'user' AND s.owner_person_id = $1)
		),
		scored AS (
		  SELECT c.id,
		    CASE WHEN c.nt = q.t THEN 100
		         WHEN c.nt LIKE q.t || '%' THEN 70
		         WHEN position(' ' || q.t IN ' ' || c.nt) > 0 THEN 45
		         WHEN length(c.nt) >= 4 AND q.t LIKE c.nt || ' %' THEN 40
		         ELSE 0 END AS ts,
		    CASE WHEN q.a = '' OR c.na = '' THEN 0
		         WHEN c.na = q.a THEN 30
		         WHEN position(' ' || q.a IN ' ' || c.na) > 0 OR position(' ' || c.na IN ' ' || q.a) > 0 THEN 15
		         ELSE 0 END AS ars
		  FROM c, q
		  WHERE length(q.t) >= 2
		),
		top AS (
		  SELECT id, ts, ars FROM scored WHERE ts > 0
		  ORDER BY ts + ars DESC, id
		  LIMIT $4
		)
		SELECT s.id, s.title, s.artist,
		       btrim(coalesce(substring(s.body from '(?n)^key:[ \t]*([^\r\n]*)'), '')),
		       s.visibility::text,
		       coalesce(po.username, pc.username, ''),
		       (SELECT count(DISTINCT sb.owner_band_id) FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		         WHERE se.variant_id = s.id AND sb.owner_kind = 'band')::int AS bands,
		       (SELECT count(*) FROM songbook_entries se WHERE se.variant_id = s.id)::int,
		       top.ts = 100
		FROM top
		JOIN songs s ON s.id = top.id
		LEFT JOIN people po ON po.id = s.owner_person_id
		LEFT JOIN people pc ON pc.id = s.created_by_person_id
		ORDER BY top.ts + top.ars DESC, bands DESC, s.id`, viewerID, title, artist, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []LibraryMatch{}
	for rows.Next() {
		var m LibraryMatch
		if err := rows.Scan(&m.VariantID, &m.Title, &m.Artist, &m.Key, &m.Visibility, &m.MadeBy, &m.Bands, &m.Songbooks, &m.Exact); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}
