package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/jackc/pgx/v5"

	"app/internal/covers"
)

// A band's or community's card image: what fills the picture of its card
// on the home page (and the settings' preview). It is kept beside its
// brand, in the same jsonb column under the "card" key: it is chosen in
// the settings, not in the look's form, so saving the look (SetBrand)
// keeps it, and brand.Parse never sees it.
//
// Modes:
//   - CardAuto (""): the logo when there is one, else a mosaic of the
//     songs' covers, else the initials (or the brand's mark);
//   - CardLogo: the logo, even when there are covers (the initials while
//     there is no logo);
//   - CardCoverMosaic: the mosaic of the songs' covers, even with a logo;
//   - CardPicked: the covers the band picked, in their order.
const (
	CardAuto        = ""
	CardLogo        = "logo"
	CardCoverMosaic = "mosaic"
	CardPicked      = "picked"
)

// CardMaxPicked is how many covers can be picked: a 3×3 mosaic.
const CardMaxPicked = 9

// CardImage is the choice, as kept.
type CardImage struct {
	Mode string `json:"mode,omitempty"`
	// Works are the picked covers' works (CardPicked), in order.
	Works []int64 `json:"works,omitempty"`
}

// Clean keeps a valid choice: a known mode, at most CardMaxPicked works,
// each once, and works only when they are picked.
func (c CardImage) Clean() CardImage {
	switch c.Mode {
	case CardAuto, CardLogo, CardCoverMosaic, CardPicked:
	default:
		c.Mode = CardAuto
	}
	if c.Mode != CardPicked {
		c.Works = nil
		return c
	}
	seen := map[int64]bool{}
	var works []int64
	for _, w := range c.Works {
		if w > 0 && !seen[w] && len(works) < CardMaxPicked {
			seen[w] = true
			works = append(works, w)
		}
	}
	c.Works = works
	return c
}

// GetCardImage loads a band's or community's card image choice.
func (st *Store) GetCardImage(ctx context.Context, kind string, id int64) (CardImage, error) {
	table, ok := brandTable[kind]
	if !ok {
		return CardImage{}, fmt.Errorf("no card image for %q", kind)
	}
	var raw []byte
	err := st.db.QueryRow(ctx, `SELECT brand->'card' FROM `+table+` WHERE id = $1`, id).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return CardImage{}, ErrNotFound
	}
	if err != nil {
		return CardImage{}, err
	}
	var c CardImage
	if len(raw) > 0 {
		_ = json.Unmarshal(raw, &c)
	}
	return c.Clean(), nil
}

// SetCardImage saves a band's or community's card image choice (cleaned);
// the brand beside it stays as it is. ErrNotFound when there's no such
// band or community.
func (st *Store) SetCardImage(ctx context.Context, kind string, id int64, c CardImage) error {
	table, ok := brandTable[kind]
	if !ok {
		return fmt.Errorf("no card image for %q", kind)
	}
	raw, err := json.Marshal(c.Clean())
	if err != nil {
		return err
	}
	tag, err := st.db.Exec(ctx, `UPDATE `+table+` SET brand = jsonb_set(brand, '{card}', $2::jsonb) WHERE id = $1`, id, raw)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// MosaicCover is one cover of a mosaic, with the song it is the cover of.
type MosaicCover struct {
	WorkID int64
	Title  string
	Artist string
	covers.Cover
}

// CoverChoices is the covers a band's or community's songs have, each
// image once, for a mosaic and for picking from: album covers before
// video stills, then by title, artist and work (case and accents
// aside), so the choice is the same on every load and only moves when
// the songs or their covers do.
func CoverChoices(songs []SongCover) []MosaicCover {
	var all []MosaicCover
	for _, sc := range songs {
		if sc.Cover != nil && sc.Cover.Image != "" {
			all = append(all, MosaicCover{WorkID: sc.WorkID, Title: sc.Title, Artist: sc.Artist, Cover: *sc.Cover})
		}
	}
	key := func(s string) string { return strings.ToLower(strings.TrimSpace(s)) }
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i], all[j]
		if (a.Source == "youtube") != (b.Source == "youtube") {
			return b.Source == "youtube"
		}
		if key(a.Title) != key(b.Title) {
			return key(a.Title) < key(b.Title)
		}
		if key(a.Artist) != key(b.Artist) {
			return key(a.Artist) < key(b.Artist)
		}
		return a.WorkID < b.WorkID
	})
	// Sorted first, so which song an album's cover shows for doesn't
	// depend on the songbook's order either.
	var out []MosaicCover
	seen := map[string]bool{}
	for _, m := range all {
		if !seen[m.Image] {
			seen[m.Image] = true
			out = append(out, m)
		}
	}
	return out
}

// MosaicSize is how many covers a square mosaic of up to max covers
// shows when have are there: 9 (3×3), 4 (2×2), 1, or none.
func MosaicSize(have, max int) int {
	for _, n := range []int{9, 4, 1} {
		if n <= max && have >= n {
			return n
		}
	}
	return 0
}

// CardMosaic is the covers a card's mosaic shows: the first of
// CoverChoices, up to max (9, 4 or 1); or, when the choice is CardPicked,
// the picked works' covers in their order (those still with a cover), up
// to 3×3. The count is always a full square (MosaicSize).
func CardMosaic(choices []MosaicCover, c CardImage, max int) []MosaicCover {
	list := choices
	if c.Mode == CardPicked && len(c.Works) > 0 {
		byWork := map[int64]MosaicCover{}
		for _, m := range choices {
			if _, ok := byWork[m.WorkID]; !ok {
				byWork[m.WorkID] = m
			}
		}
		list = nil
		for _, w := range c.Works {
			if m, ok := byWork[w]; ok {
				list = append(list, m)
			}
		}
		// What was picked shows, up to 3×3, whatever the card's own size.
		max = CardMaxPicked
	}
	return list[:MosaicSize(len(list), max)]
}
