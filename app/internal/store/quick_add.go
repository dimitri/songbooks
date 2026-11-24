package store

import (
	"context"
	"errors"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// Quick add (docs/coaching.md, "Adding a song for a student"): a pasted
// video link, or "Title — Artist", finds the song the songbook should
// link — its owner's own version of it first, else the public song of
// that video or of that title and artist — before anything new is made.

// SongMatch is a song found for a quick add.
type SongMatch struct {
	ID          int64  `json:"id"`
	Title       string `json:"title"`
	Artist      string `json:"artist"`
	VersionName string `json:"versionName,omitempty"`
	Public      bool   `json:"public"`
	Own         bool   `json:"own"` // owner's own song (its version), not a link
	ByVideo     bool   `json:"byVideo"`
}

// FindSongMatch is the song owner's songbook should hold for title and
// artist, or the video videoID (a YouTube id, "" for none), as personID
// sees them: owner's own unnamed song of that video or title and artist
// (its version of a public song), else the public one — the same video
// first, then the same title and artist (§7's identity: case, accents,
// "The", "&"); an empty artist matches a title only when one song has
// it. Never a closed songbook's song, nor anyone else's private one. Nil
// when there's none.
func (st *Store) FindSongMatch(ctx context.Context, personID int64, owner Owner, title, artist, videoID string) (*SongMatch, error) {
	want := normIdentityPart(title)
	if want == "" && videoID == "" {
		return nil, nil
	}
	// Candidates: the title's longest word anywhere in a title, or the
	// video in the reference; then compared exactly in Go.
	word := ""
	for _, w := range strings.Fields(strings.ToLower(title)) {
		w = strings.TrimFunc(w, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
		if utf8.RuneCountInString(w) > utf8.RuneCountInString(word) {
			word = w
		}
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.title, s.artist, s.version_name, s.public, s.reference_url,
		       (s.owner_kind = $3 AND coalesce(s.owner_person_id, s.owner_band_id, s.owner_community_id) = $4) AS own
		FROM songs s
		WHERE song_visible(s.id, $1) AND NOT song_closed(s.id)
		  AND (s.public OR (s.owner_kind = $3 AND coalesce(s.owner_person_id, s.owner_band_id, s.owner_community_id) = $4))
		  AND (($2 <> '' AND lower(s.title) LIKE '%' || $2 || '%')
		       OR ($5 <> '' AND s.reference_url LIKE '%' || $5 || '%'))
		ORDER BY s.id
		LIMIT 500`, personID, word, owner.Kind, owner.ID, videoID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	type cand struct {
		SongMatch
		ref string
	}
	var cands []cand
	for rows.Next() {
		var c cand
		if err := rows.Scan(&c.ID, &c.Title, &c.Artist, &c.VersionName, &c.Public, &c.ref, &c.Own); err != nil {
			return nil, err
		}
		cands = append(cands, c)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	wantArtist := normIdentityPart(artist)
	rank := func(c cand) int {
		r := 0
		switch {
		case videoID != "" && YouTubeID(c.ref) == videoID:
			r = 30
		case normIdentityPart(c.Title) == want && wantArtist != "" && normIdentityPart(c.Artist) == wantArtist:
			r = 20
		case normIdentityPart(c.Title) == want && wantArtist == "":
			r = 10
		default:
			return 0
		}
		if c.Own {
			r += 5
		}
		if c.VersionName != "" {
			r -= 3
		}
		return r
	}
	var best *cand
	bestRank, titleOnly := 0, 0
	for i := range cands {
		r := rank(cands[i])
		if r >= 10 && r < 20 {
			titleOnly++
		}
		if r > bestRank {
			bestRank, best = r, &cands[i]
		}
	}
	if best == nil || (bestRank < 20 && titleOnly > 1) {
		return nil, nil
	}
	m := best.SongMatch
	m.ByVideo = videoID != "" && YouTubeID(best.ref) == videoID
	if !m.Own {
		// The owner's own version of it, when it has one.
		var id int64
		var vt, va string
		err := st.db.QueryRow(ctx, `
			SELECT id, title, artist FROM songs
			WHERE forked_from_id = $1 AND owner_kind = $2
			  AND coalesce(owner_person_id, owner_band_id, owner_community_id) = $3 AND version_name = ''
			ORDER BY id LIMIT 1`, m.ID, owner.Kind, owner.ID).Scan(&id, &vt, &va)
		switch {
		case err == nil:
			m.ID, m.Title, m.Artist, m.Own, m.Public = id, vt, va, true, false
		case !errors.Is(err, pgx.ErrNoRows):
			return nil, err
		}
	}
	return &m, nil
}
