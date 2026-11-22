package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"app/internal/brand"
	"app/internal/i18n"
	"app/internal/store"
)

// The home page (Aujourd'hui, index.html), laid out as a dashboard: a
// top row with the person's own songbook (wide) beside "This week" (the
// next dates, a weekly series once), then every band and community
// they're in as one grid of equal cards, each with its picture (logo,
// cover mosaic or initials).

// --- This week ---

// homeWeekRows is how many rows "This week" shows at most (a phone
// shows two of them, index.html's CSS), and homeLaterRows how many of
// the next dates it shows when nothing is on this week.
const (
	homeWeekRows  = 5
	homeLaterRows = 3
	homeWeekDays  = 7
)

// weekRow is one row of "This week": a date, or the next date of a
// series repeating at the same weekday and time.
type weekRow struct {
	nextUpView
	// Weekly: the same date comes back every week (at least twice in
	// the window), so it shows once, as "weekly".
	Weekly bool
	// More is how many other dates of the series are in the window.
	More int
}

// comingUp is "This week".
type comingUp struct {
	Rows []weekRow
	// Later: nothing is on this week; Rows are the next dates after it.
	Later bool
	// Total is how many dates are coming up in the whole window, for
	// "See all (n)".
	Total int
}

// seriesKey is what makes dates one series: the same kind, title and
// band or community, on the same weekday at the same time.
func seriesKey(it store.CalendarItem, loc *time.Location) string {
	t := it.Starts.In(loc)
	var band, community int64
	if it.BandID != nil {
		band = *it.BandID
	}
	if it.CommunityID != nil {
		community = *it.CommunityID
	}
	at := t.Format("15:04")
	if it.AllDay {
		at = "all-day"
	}
	return fmt.Sprintf("%s|%s|%d|%d|%d|%s", it.Kind, strings.ToLower(strings.TrimSpace(it.Title)), band, community, t.Weekday(), at)
}

// comingUpWeek makes "This week" from the calendar (soonest first): what
// isn't over yet, a series of dates repeating each week at the same time
// (a weekly rehearsal) once, at its next date. The rows are this week's
// (the next homeWeekDays days), at most homeWeekRows; when there are
// none, the next homeLaterRows after it.
func comingUpWeek(items []store.CalendarItem, now time.Time) comingUp {
	loc := now.Location()
	var out comingUp
	var rows []*weekRow
	bySeries := map[string]*weekRow{}
	last := map[string]time.Time{}
	steady := map[string]bool{}
	for _, it := range items {
		if !it.AllDay && !it.Starts.After(now.Add(-todayStartedFor)) {
			continue
		}
		if it.AllDay && daysFrom(it.Starts, now) < 0 {
			continue
		}
		out.Total++
		key := seriesKey(it, loc)
		if r, ok := bySeries[key]; ok {
			r.More++
			if daysFrom(it.Starts, last[key].In(loc)) != 7 {
				steady[key] = false
			}
			last[key] = it.Starts
			continue
		}
		r := &weekRow{nextUpView: nextUpView{CalendarItem: it, SetlistLink: setlistLink(it), When: humanDay(it.Starts, now)}}
		bySeries[key], last[key], steady[key] = r, it.Starts, true
		rows = append(rows, r)
	}
	for _, r := range rows {
		r.Weekly = r.More > 0 && steady[seriesKey(r.CalendarItem, loc)]
	}
	for _, r := range rows {
		if daysFrom(r.Starts, now) >= homeWeekDays || len(out.Rows) == homeWeekRows {
			break
		}
		out.Rows = append(out.Rows, *r)
	}
	if len(out.Rows) == 0 {
		out.Later = len(rows) > 0
		for _, r := range rows[:min(homeLaterRows, len(rows))] {
			out.Rows = append(out.Rows, *r)
		}
	}
	return out
}

// --- Your songbook ---

// homeSongbookMosaic is how many covers the songbook card's mosaic has
// at most (3×3), and homeRecentSongs how many songs it names.
const (
	homeSongbookMosaic = 9
	homeRecentSongs    = 3
)

// mySongbookCard is "Your songbook": the person's own songbook (their
// default one, "My songbook" unless renamed), the covers of its songs,
// the songs they opened last, and their other songbooks.
type mySongbookCard struct {
	ID     int64
	Name   string
	Songs  int
	Mosaic []store.MosaicCover
	Recent []store.RecentSong
	Others []bookStats
}

// MosaicClass is the mosaic's grid: "m1", "m4" or "m9".
func (c mySongbookCard) MosaicClass() string { return fmt.Sprintf("m%d", len(c.Mosaic)) }

func (s *Server) mySongbook(ctx context.Context, lang string, personID int64) (*mySongbookCard, error) {
	id, err := s.store.GetDefaultSongbook(ctx, "person", personID)
	if err != nil {
		return nil, err
	}
	books, err := s.buildBookStats(ctx, personID)
	if err != nil {
		return nil, err
	}
	card := &mySongbookCard{ID: id}
	for _, b := range books {
		if b.ID == id {
			card.Name, card.Songs = b.Name, b.SongCount
		} else {
			card.Others = append(card.Others, b)
		}
	}
	if card.Name == "" || card.Name == "My songbook" {
		card.Name = i18n.T(lang, "My songbook")
	}
	songs, err := s.store.SongbookCovers(ctx, id)
	if err != nil {
		return nil, err
	}
	card.Mosaic = store.CardMosaic(store.CoverChoices(songs), store.CardImage{}, homeSongbookMosaic)
	if len(card.Mosaic) < homeSongbookMosaic {
		// The covers not looked for yet are, in the background: the
		// next visit shows them.
		s.fillCovers(id)
	}
	card.Recent, err = s.store.ListRecentSongs(ctx, personID, homeRecentSongs)
	if err != nil {
		return nil, err
	}
	return card, nil
}

// --- Bands and communities ---

// homeCardMosaic is how many covers a band's or community's card shows
// at most when they're chosen for it (2×2); picked ones can be 3×3.
const homeCardMosaic = 4

// groupCard is a band's or community's card: the same for both.
type groupCard struct {
	Kind      string // "band" or "community"
	ID        int64
	Name      string
	Link      string
	KindLabel string // "Band", "Community", "Coaching"
	Tagline   string
	cardPicture
	Songs, Members int
	Next           *store.CalendarItem
}

// cardPicture is what fills a card's picture: the logo, else the
// mosaic, else the mark (the brand's own, else the name's initials) on
// the tile's colours, which are the brand's header (or accent) and the
// text color that reads on it; empty for the app's own look.
type cardPicture struct {
	Logo           string
	Mosaic         []store.MosaicCover
	Mark           string
	TileBg, TileFg string
}

// MosaicClass is the mosaic's grid: "m1", "m4" or "m9".
func (p cardPicture) MosaicClass() string { return fmt.Sprintf("m%d", len(p.Mosaic)) }

// choosePicture decides a card's picture from the group's choice
// (store.CardImage), its logo and its songs' covers (loaded only when
// needed: covers is called at most once).
func choosePicture(choice store.CardImage, logo string, covers func() []store.MosaicCover) (string, []store.MosaicCover) {
	switch choice.Mode {
	case store.CardLogo:
		return logo, nil
	case store.CardCoverMosaic, store.CardPicked:
		if m := store.CardMosaic(covers(), choice, homeCardMosaic); len(m) > 0 {
			return "", m
		}
		return logo, nil
	}
	if logo != "" {
		return logo, nil
	}
	return "", store.CardMosaic(covers(), choice, homeCardMosaic)
}

// groupPicture is a band's or community's card picture, from its brand,
// its card image choice and its songs' covers.
// Its brand's tagline comes with it.
func (s *Server) groupPicture(ctx context.Context, kind string, id int64, name string, choice store.CardImage) (cardPicture, string, error) {
	p := cardPicture{Mark: initials(name)}
	b, _, err := s.store.GetBrand(ctx, kind, id)
	if err != nil {
		return p, "", err
	}
	logo, tagline := "", ""
	if t := brand.Resolve(b, name); t != nil {
		logo, tagline = t.LogoURL, t.Tagline
		if t.Mark != "" {
			p.Mark = t.Mark
		}
		p.TileBg = t.Header
		if p.TileBg == "" {
			p.TileBg = t.Accent
		}
		p.TileFg = brand.ContrastOn(p.TileBg)
	}
	var coverErr error
	p.Logo, p.Mosaic = choosePicture(choice, logo, func() []store.MosaicCover {
		songs, err := s.store.OwnerCovers(ctx, kind, id)
		if err != nil {
			coverErr = err
			return nil
		}
		return store.CoverChoices(songs)
	})
	return p, tagline, coverErr
}

// homeGroupCards makes the cards of every band and community the person
// is in. Those with a date coming up come first, soonest first (the
// order of "This week"), then the others by name: the order a musician
// needs them in this week, the same on every load until a date passes.
func (s *Server) homeGroupCards(ctx context.Context, bands []bandCardView, communities []communityCardView, calendar []store.CalendarItem, now time.Time) ([]groupCard, error) {
	next := func(kind string, id int64) *store.CalendarItem {
		for i, it := range calendar {
			if !it.AllDay && !it.Starts.After(now.Add(-todayStartedFor)) || it.AllDay && daysFrom(it.Starts, now) < 0 {
				continue
			}
			if kind == "band" && it.BandID != nil && *it.BandID == id || kind == "community" && it.CommunityID != nil && *it.CommunityID == id {
				return &calendar[i]
			}
		}
		return nil
	}
	var ids []int64
	for _, b := range bands {
		ids = append(ids, b.ID)
	}
	stats, err := s.store.ListBandCardStats(ctx, ids)
	if err != nil {
		return nil, err
	}
	var cards []groupCard
	add := func(kind string, id int64, name, link, label string, songs, members int) error {
		choice, err := s.store.GetCardImage(ctx, kind, id)
		if err != nil {
			return err
		}
		pic, tagline, err := s.groupPicture(ctx, kind, id, name, choice)
		if err != nil {
			return err
		}
		cards = append(cards, groupCard{Kind: kind, ID: id, Name: name, Link: link, KindLabel: label, Tagline: tagline,
			cardPicture: pic, Songs: songs, Members: members, Next: next(kind, id)})
		return nil
	}
	for _, b := range bands {
		if err := add("band", b.ID, b.Name, fmt.Sprintf("/bands/%d", b.ID), "Band", stats[b.ID].SongCount, b.Members); err != nil {
			return nil, err
		}
	}
	for _, c := range communities {
		label := "Community"
		if c.IsCoaching() {
			label = "Coaching"
		}
		if err := add("community", c.ID, c.Name, fmt.Sprintf("/communities/%d", c.ID), label, c.SongCount, c.MemberCount); err != nil {
			return nil, err
		}
	}
	sortGroupCards(cards)
	return cards, nil
}

// sortGroupCards: a date coming up first, soonest first; then by name.
func sortGroupCards(cards []groupCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		a, b := cards[i], cards[j]
		if (a.Next != nil) != (b.Next != nil) {
			return a.Next != nil
		}
		if a.Next != nil && !a.Next.Starts.Equal(b.Next.Starts) {
			return a.Next.Starts.Before(b.Next.Starts)
		}
		if x, y := strings.ToLower(a.Name), strings.ToLower(b.Name); x != y {
			return x < y
		}
		return a.Kind < b.Kind
	})
}
