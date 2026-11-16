// Package covers finds a song's cover art and the colors in it.
//
// A song is looked up by its title and artist the way music players do
// it without an account: the iTunes Search API first (no key, artwork up
// to 3000 px, the album's genre), then Deezer's search (no key), and the
// song's own YouTube reference as the last resort (its thumbnail is the
// very version the band plays). The artist's own albums come before
// compilations ("Various Artists") and the earliest release wins, so a
// song gets its original album, not a best-of.
//
// Nothing is stored but the image's address, a link to the album's page
// (the cover is shown as a link to it, the way players credit artwork)
// and its palette: the image stays where it is.
package covers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"

	"golang.org/x/text/unicode/norm"
)

// Cover is what was found for a song.
type Cover struct {
	Source string    `json:"source"` // itunes, deezer or youtube
	Image  string    `json:"image"`  // the artwork, 600 px when the source has it
	Link   string    `json:"link"`   // the album's (or video's) page
	Album  string    `json:"album"`
	Year   int       `json:"year,omitempty"`
	Genre  string    `json:"genre,omitempty"`
	Colors []Swatch  `json:"colors,omitempty"`
	Found  time.Time `json:"-"`
}

// Finder looks covers up; its zero value uses http.DefaultClient and the
// public endpoints, and doesn't wait out a rate limit (NewFinder does).
type Finder struct {
	Client *http.Client
	ITunes string // base URLs, for tests
	Deezer string
	// Wait is how long to wait before asking again, once, when a source
	// says "too many requests"; zero doesn't ask again.
	Wait time.Duration
}

// NewFinder is a Finder on the public endpoints, waiting a minute out
// when rate limited.
func NewFinder() *Finder { return &Finder{Wait: time.Minute} }

func (f *Finder) client() *http.Client {
	if f.Client != nil {
		return f.Client
	}
	return http.DefaultClient
}

// Find looks for a song's cover: iTunes, then Deezer, then the YouTube
// reference (youtubeID, "" when the song has none). ok is false when
// nothing matched; err tells why when a source failed.
func (f *Finder) Find(ctx context.Context, title, artist, youtubeID string) (c Cover, ok bool, err error) {
	// A source that fails (down, rate limiting) passes to the next; the
	// error is told only when no source found anything.
	if strings.TrimSpace(title) != "" && strings.TrimSpace(artist) != "" {
		for _, source := range []func(context.Context, string, string) (Cover, bool, error){f.itunes, f.deezer} {
			c, found, e := source(ctx, title, artist)
			if found {
				return c, true, nil
			}
			if e != nil {
				err = e
			}
		}
	}
	if youtubeID != "" {
		return Cover{
			Source: "youtube",
			Image:  "https://i.ytimg.com/vi/" + youtubeID + "/hqdefault.jpg",
			Link:   "https://www.youtube.com/watch?v=" + youtubeID,
		}, true, nil
	}
	return Cover{}, false, err
}

// candidate is one search result, before picking.
type candidate struct {
	Cover
	artist, title string
	compilation   bool
}

func (f *Finder) itunes(ctx context.Context, title, artist string) (Cover, bool, error) {
	base := f.ITunes
	if base == "" {
		base = "https://itunes.apple.com"
	}
	q := url.Values{"term": {artist + " " + title}, "entity": {"song"}, "limit": {"25"}}
	var res struct {
		Results []struct {
			ArtistName, TrackName, CollectionName, CollectionArtistName string
			CollectionViewURL                                           string `json:"collectionViewUrl"`
			ArtworkURL100                                               string `json:"artworkUrl100"`
			ReleaseDate, PrimaryGenreName                               string
		}
	}
	if err := f.get(ctx, base+"/search?"+q.Encode(), &res); err != nil {
		return Cover{}, false, err
	}
	var cs []candidate
	for _, r := range res.Results {
		year := 0
		fmt.Sscanf(r.ReleaseDate, "%d", &year)
		cs = append(cs, candidate{
			Cover: Cover{
				Source: "itunes",
				Image:  strings.Replace(r.ArtworkURL100, "100x100bb", "600x600bb", 1),
				Link:   stripQuery(r.CollectionViewURL),
				Album:  r.CollectionName, Year: year, Genre: r.PrimaryGenreName,
			},
			artist: r.ArtistName, title: r.TrackName,
			compilation: r.CollectionArtistName != "" && !sameName(r.CollectionArtistName, r.ArtistName),
		})
	}
	c, ok := pick(cs, title, artist)
	return c, ok, nil
}

func (f *Finder) deezer(ctx context.Context, title, artist string) (Cover, bool, error) {
	base := f.Deezer
	if base == "" {
		base = "https://api.deezer.com"
	}
	q := url.Values{"q": {artist + " " + title}, "limit": {"25"}}
	var res struct {
		Data []struct {
			Title  string
			Artist struct{ Name string }
			Album  struct {
				ID       int64
				Title    string
				CoverXL  string `json:"cover_xl"`
				CoverBig string `json:"cover_big"`
			}
		}
	}
	if err := f.get(ctx, base+"/search?"+q.Encode(), &res); err != nil {
		return Cover{}, false, err
	}
	var cs []candidate
	ids := map[string]int64{}
	for _, r := range res.Data {
		img := r.Album.CoverBig
		if img == "" {
			img = r.Album.CoverXL
		}
		cs = append(cs, candidate{
			Cover: Cover{
				Source: "deezer", Image: img, Album: r.Album.Title,
				Link: fmt.Sprintf("https://www.deezer.com/album/%d", r.Album.ID),
			},
			artist: r.Artist.Name, title: r.Title,
		})
		ids[img] = r.Album.ID
	}
	// A search result doesn't say whose album it is: ask, and skip the
	// compilations.
	for i, c := range rank(cs, title, artist) {
		if i == 3 {
			break
		}
		var album struct {
			Artist      struct{ Name string }
			ReleaseDate string `json:"release_date"`
			Genres      struct{ Data []struct{ Name string } }
		}
		if err := f.get(ctx, fmt.Sprintf("%s/album/%d", base, ids[c.Image]), &album); err != nil {
			return Cover{}, false, err
		}
		if !sameName(album.Artist.Name, artist) {
			continue
		}
		fmt.Sscanf(album.ReleaseDate, "%d", &c.Year)
		if len(album.Genres.Data) > 0 {
			c.Genre = album.Genres.Data[0].Name
		}
		return c.Cover, true, nil
	}
	return Cover{}, false, nil
}

func (f *Finder) get(ctx context.Context, u string, out any) error {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second+f.Wait)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", "songbooks (https://songbooks.tapoueh.org)")
	for try := 0; ; try++ {
		resp, err := f.client().Do(req)
		if err != nil {
			return err
		}
		// iTunes allows about 20 searches a minute and answers 403 past
		// that: wait for the minute to pass, once.
		if (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) && try == 0 && f.Wait > 0 {
			resp.Body.Close()
			select {
			case <-time.After(f.Wait):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("%s: %s", u, resp.Status)
		}
		return json.NewDecoder(resp.Body).Decode(out)
	}
}

// later marks an album that isn't the song's first home: a live album, a
// best-of, a remaster, a deluxe reissue.
var later = regexp.MustCompile(`(?i)\b(live|sessions?|greatest|best of|hits|collection|anthology|essential|remaster|deluxe|edition|karaoke|tribute|cover|instrumental)\b`)

// pick is the result that is the song: the same artist and title, its
// first release (the artist's own album before a compilation, an album
// that isn't a live or best-of before one that is, then the earliest
// year). A compilation is a last resort: some songs are only on those.
func pick(cs []candidate, title, artist string) (Cover, bool) {
	r := rank(cs, title, artist)
	if len(r) == 0 {
		return Cover{}, false
	}
	return r[0].Cover, true
}

// rank is the results that are the song, the likeliest first (pick).
func rank(cs []candidate, title, artist string) []candidate {
	var ok []candidate
	for _, c := range cs {
		if c.Image == "" || !sameName(c.artist, artist) || !sameTitle(c.title, title) {
			continue
		}
		ok = append(ok, c)
	}
	sort.SliceStable(ok, func(i, j int) bool {
		if ok[i].compilation != ok[j].compilation {
			return !ok[i].compilation
		}
		li, lj := later.MatchString(ok[i].Album), later.MatchString(ok[j].Album)
		if li != lj {
			return !li
		}
		if (ok[i].Year == 0) != (ok[j].Year == 0) {
			return ok[i].Year != 0
		}
		return ok[i].Year < ok[j].Year
	})
	return ok
}

// fold is a name to compare: lower case, no accents, letters and digits
// only, "&" read as "and", a leading "the" dropped.
func fold(s string) string {
	s = strings.ReplaceAll(strings.ToLower(s), "&", " and ")
	var b strings.Builder
	for _, r := range norm.NFD.String(s) {
		switch {
		case unicode.Is(unicode.Mn, r):
		case unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), " "):
			b.WriteByte(' ')
		}
	}
	return strings.TrimPrefix(strings.TrimSpace(b.String()), "the ")
}

// sameName: one artist name holds the other ("Jeff Beck" and "Jeff Beck
// Group"; "Stevie Ray Vaughan" and "Stevie Ray Vaughan & Double
// Trouble"), spaces aside.
func sameName(a, b string) bool {
	a, b = strings.ReplaceAll(fold(a), " ", ""), strings.ReplaceAll(fold(b), " ", "")
	return a != "" && b != "" && (strings.Contains(a, b) || strings.Contains(b, a))
}

// sameTitle: the same title once what's in brackets ("(Remastered
// 2011)", "[Live]") and after a dash ("- Single Version") is set aside.
var titleExtra = regexp.MustCompile(`\s*(\(.*?\)|\[.*?\]|\s-\s.*$)`)

func sameTitle(a, b string) bool {
	a = strings.ReplaceAll(fold(titleExtra.ReplaceAllString(a, "")), " ", "")
	b = strings.ReplaceAll(fold(titleExtra.ReplaceAllString(b, "")), " ", "")
	return a != "" && b != "" && (a == b || strings.HasPrefix(a, b) || strings.HasPrefix(b, a))
}

func stripQuery(u string) string {
	if i := strings.IndexByte(u, '?'); i >= 0 {
		return u[:i]
	}
	return u
}
