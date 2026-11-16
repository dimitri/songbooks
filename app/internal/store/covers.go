package store

import (
	"context"
	"encoding/json"
	"sort"
	"strconv"
	"strings"
	"time"

	"app/internal/brand"
	"app/internal/covers"
)

// SongCover is a song of a songbook and its cover, when one was found.
type SongCover struct {
	VariantID, WorkID int64
	Title, Artist     string
	YouTube           string // the reference video's id, "" without one
	Cover             *covers.Cover
	Looked            bool // looked for, found or not
}

// SongbookCovers is a songbook's songs, in its order, with their covers.
func (st *Store) SongbookCovers(ctx context.Context, songbookID int64) ([]SongCover, error) {
	groups, err := st.ListSongsOrdered(ctx, songbookID)
	if err != nil {
		return nil, err
	}
	var ids []int64
	for _, g := range groups {
		ids = append(ids, g.VariantIDs...)
	}
	rows, err := st.db.Query(ctx, `
		SELECT s.id, s.work_id, s.title, s.artist, s.reference_url, c.found, c.cover
		  FROM songs s LEFT JOIN work_covers c ON c.work_id = s.work_id
		 WHERE s.id = ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byID := map[int64]SongCover{}
	for rows.Next() {
		var sc SongCover
		var ref string
		var found *bool
		var raw []byte
		if err := rows.Scan(&sc.VariantID, &sc.WorkID, &sc.Title, &sc.Artist, &ref, &found, &raw); err != nil {
			return nil, err
		}
		sc.YouTube = YouTubeID(ref)
		if found != nil {
			sc.Looked = true
			if *found {
				var c covers.Cover
				if json.Unmarshal(raw, &c) == nil {
					sc.Cover = &c
				}
			}
		}
		byID[sc.VariantID] = sc
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]SongCover, 0, len(ids))
	for _, id := range ids {
		if sc, ok := byID[id]; ok {
			out = append(out, sc)
		}
	}
	return out, nil
}

// SetWorkCover keeps what was found for a work (c nil: nothing was).
func (st *Store) SetWorkCover(ctx context.Context, workID int64, c *covers.Cover) error {
	raw := []byte(`{}`)
	if c != nil {
		var err error
		if raw, err = json.Marshal(c); err != nil {
			return err
		}
	}
	_, err := st.db.Exec(ctx, `
		INSERT INTO work_covers (work_id, found, cover) VALUES ($1, $2, $3)
		ON CONFLICT (work_id) DO UPDATE SET found = $2, cover = $3, looked_at = now()`, workID, c != nil, raw)
	return err
}

// FillCovers looks for the covers of a songbook's songs not looked for yet
// (again: also those with no album cover, none found or only a video's
// still, as when a source was rate limiting), one every pace (iTunes allows about 20 searches a
// minute), telling each to done when not nil. It returns how many were
// found and how many weren't, and the last error of a source: a song a
// source failed on is skipped, to be looked for next time.
func (st *Store) FillCovers(ctx context.Context, f *covers.Finder, songbookID int64, again bool, pace time.Duration, done func(SongCover)) (found, missing int, err error) {
	songs, err := st.SongbookCovers(ctx, songbookID)
	if err != nil {
		return 0, 0, err
	}
	seen := map[int64]bool{}
	first := true
	for _, sc := range songs {
		albumCover := sc.Cover != nil && sc.Cover.Source != "youtube"
		if seen[sc.WorkID] || sc.Looked && (!again || albumCover) {
			continue
		}
		seen[sc.WorkID] = true
		if !first {
			select {
			case <-time.After(pace):
			case <-ctx.Done():
				return found, missing, ctx.Err()
			}
		}
		first = false
		c, ok, ferr := f.Find(ctx, sc.Title, sc.Artist, sc.YouTube)
		if ferr != nil && !ok {
			// A source failing isn't the song having no cover: it stays
			// to be looked for next time.
			err = ferr
			continue
		}
		sc.Cover = nil
		if ok {
			// A cover whose image won't open is kept, without colors.
			c.Colors, _ = f.Colors(ctx, c)
			c.Found = time.Now()
			sc.Cover = &c
			found++
		} else {
			missing++
		}
		if err := st.SetWorkCover(ctx, sc.WorkID, sc.Cover); err != nil {
			return found, missing, err
		}
		sc.Looked = true
		if done != nil {
			done(sc)
		}
	}
	return found, missing, err
}

// CoverLook is the look a songbook's covers make (brand.Derive).
func CoverLook(songs []SongCover) brand.Derived {
	var in []brand.Song
	for _, sc := range songs {
		if sc.Cover != nil {
			in = append(in, brand.Song{Colors: brandColors(sc.Cover), Genre: sc.Cover.Genre})
		}
	}
	return brand.Derive(in)
}

func brandColors(c *covers.Cover) []brand.Color {
	var out []brand.Color
	for _, s := range c.Colors {
		out = append(out, brand.Color{Hex: s.Hex, Share: s.Share})
	}
	return out
}

// BookLook is what a songbook's banner shows: the look of its covers, the
// covers that go best with it (a mosaic of up to four, one per artist,
// album covers before video stills), its most present artists.
type BookLook struct {
	brand.Derived
	Mosaic  []covers.Cover
	Artists []string
	Found   int // songs with a cover
	Songs   int
}

// SongbookLook makes a songbook's BookLook from its songs' covers.
func SongbookLook(songs []SongCover) BookLook {
	bl := BookLook{Derived: CoverLook(songs), Songs: len(songs)}
	count := map[string]int{}
	var with []SongCover
	for _, sc := range songs {
		if a := strings.TrimSpace(sc.Artist); a != "" {
			count[a]++
		}
		if sc.Cover != nil {
			bl.Found++
			with = append(with, sc)
		}
	}
	for a := range count {
		bl.Artists = append(bl.Artists, a)
	}
	sort.Slice(bl.Artists, func(i, j int) bool {
		if count[bl.Artists[i]] != count[bl.Artists[j]] {
			return count[bl.Artists[i]] > count[bl.Artists[j]]
		}
		return bl.Artists[i] < bl.Artists[j]
	})
	fit := func(sc SongCover) float64 {
		f := bl.Fit(brandColors(sc.Cover))
		if sc.Cover.Source == "youtube" {
			f -= 1
		}
		return f
	}
	sort.SliceStable(with, func(i, j int) bool { return fit(with[i]) > fit(with[j]) })
	artists, albums := map[string]bool{}, map[string]bool{}
	for _, sc := range with {
		a, al := strings.ToLower(sc.Artist), strings.ToLower(sc.Cover.Image)
		if artists[a] || albums[al] {
			continue
		}
		artists[a], albums[al] = true, true
		bl.Mosaic = append(bl.Mosaic, *sc.Cover)
		if len(bl.Mosaic) == 4 {
			break
		}
	}
	return bl
}

// ArtistLine is the most present artists, "A, B, C and 12 more".
func (bl BookLook) ArtistLine() string {
	if len(bl.Artists) <= 5 {
		return strings.Join(bl.Artists, ", ")
	}
	return strings.Join(bl.Artists[:5], ", ") + " and " + strconv.Itoa(len(bl.Artists)-5) + " more"
}

// GenreLine is the three most frequent genres, "Soul · Blues · R&B".
func (bl BookLook) GenreLine() string {
	return strings.Join(bl.Genres[:min(3, len(bl.Genres))], " · ")
}

// OwnerCovers is the songs of every songbook of a band or community
// ("band" or "community"), each work once, with their covers.
func (st *Store) OwnerCovers(ctx context.Context, kind string, id int64) ([]SongCover, error) {
	rows, err := st.db.Query(ctx, `
		SELECT id FROM songbooks
		 WHERE kind = 'songbook' AND owner_kind = $1
		   AND (owner_band_id = $2 OR owner_community_id = $2)
		 ORDER BY is_default DESC, id`, kind, id)
	if err != nil {
		return nil, err
	}
	var books []int64
	for rows.Next() {
		var b int64
		if err := rows.Scan(&b); err != nil {
			rows.Close()
			return nil, err
		}
		books = append(books, b)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	var out []SongCover
	seen := map[int64]bool{}
	for _, b := range books {
		songs, err := st.SongbookCovers(ctx, b)
		if err != nil {
			return nil, err
		}
		for _, sc := range songs {
			if !seen[sc.WorkID] {
				seen[sc.WorkID] = true
				out = append(out, sc)
			}
		}
	}
	return out, nil
}
