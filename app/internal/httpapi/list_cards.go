package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode"

	"app/internal/brand"
	"app/internal/i18n"
	"app/internal/store"
)

// The cards of My bands and My communities (cards.html "band-list-card"
// and "community-list-card"): each one wears its band's or community's
// look, scoped to the card (its brand's design tokens set inline on it),
// and says more than the home page's short card: who's in it, how big
// its songbook is, what's next on the calendar, when it last moved.

// listCardFaces is how many members a band card shows by name.
const listCardFaces = 6

// listCardPerson is one member's face on a card: initials in a disc.
type listCardPerson struct {
	Name     string
	Initials string
	Guest    bool
}

// cardPerson is a member's face: their display name.
func cardPerson(name string, guest bool) listCardPerson {
	return listCardPerson{Name: name, Initials: initials(name), Guest: guest}
}

// listCardLook is what a card wears: the brand (nil for the app's own
// look) and its mark, the logo, the brand's mark or the name's initials.
type listCardLook struct {
	Brand    *brand.Theme
	Logo     string
	Mark     string
	Initials bool // Mark is the name's initials, not the brand's own
}

// bandListCard is a band's card on My bands.
type bandListCard struct {
	bandCardView
	listCardLook
	People       []listCardPerson
	MorePeople   int
	SongCount    int
	LastActivity string // "3h ago", "12 Oct": "" when nothing happened
	Next         *store.CalendarItem
}

// communityListCard is a community's card on My communities.
type communityListCard struct {
	communityCardView
	listCardLook
	// Staff is who runs it (its staff, a coaching community's coach),
	// the first few.
	Staff     []listCardPerson
	MoreStaff int
	// A coaching community's student: their open practice assignments
	// (the first few labels and how many), and their own page.
	Practice      []string
	PracticeCount int
	MorePractice  int
	StudentPage   string
}

// listCardPractice is how many practice assignments a card names.
const listCardPractice = 3

// bandListCards dresses buildBandCards' cards for My bands, and lists
// the Google Fonts stylesheets their looks need.
func (s *Server) bandListCards(ctx context.Context, lang string, personID int64, cards []bandCardView) ([]bandListCard, []string, error) {
	ids := make([]int64, len(cards))
	for i, c := range cards {
		ids[i] = c.ID
	}
	stats, err := s.store.ListBandCardStats(ctx, ids)
	if err != nil {
		return nil, nil, err
	}
	now := time.Now()
	items, err := s.calendarAhead(ctx, personID, now)
	if err != nil {
		return nil, nil, err
	}
	fonts := &fontSet{}
	out := make([]bandListCard, len(cards))
	for i, c := range cards {
		card := bandListCard{bandCardView: c}
		card.listCardLook = s.listLook(ctx, "band", c.ID, c.Name, fonts)
		st := stats[c.ID]
		card.SongCount = st.SongCount
		if st.LastActivity != nil {
			card.LastActivity = activityLabel(lang, *st.LastActivity, now)
		}
		members := append([]store.BandMember(nil), c.MemberList...)
		sort.SliceStable(members, func(a, b int) bool {
			if (members[a].Status == "core") != (members[b].Status == "core") {
				return members[a].Status == "core"
			}
			return members[a].JoinedAt.Before(members[b].JoinedAt)
		})
		for j, m := range members {
			if j == listCardFaces {
				card.MorePeople = len(members) - j
				break
			}
			card.People = append(card.People, cardPerson(m.DisplayName, m.Status == "guest"))
		}
		for j := range items {
			if it := &items[j]; it.BandID != nil && *it.BandID == c.ID {
				card.Next = it
				break
			}
		}
		out[i] = card
	}
	return out, fonts.list, nil
}

// communityListCards dresses buildCommunityCards' cards for My
// communities, and lists the Google Fonts stylesheets their looks need.
func (s *Server) communityListCards(ctx context.Context, personID int64, cards []communityCardView) ([]communityListCard, []string, error) {
	fonts := &fontSet{}
	out := make([]communityListCard, len(cards))
	for i, c := range cards {
		card := communityListCard{communityCardView: c}
		card.listCardLook = s.listLook(ctx, "community", c.ID, c.Name, fonts)
		members, err := s.store.ListCommunityMembers(ctx, c.ID, false)
		if err != nil {
			return nil, nil, err
		}
		for i := len(members) - 1; i >= 0; i-- { // the longest-standing first
			m := members[i]
			if !m.IsStaff() {
				continue
			}
			if len(card.Staff) == listCardFaces {
				card.MoreStaff++
				continue
			}
			card.Staff = append(card.Staff, cardPerson(m.DisplayName, false))
		}
		if c.IsCoaching() && c.MyRole != "staff" {
			list, err := s.store.ListStudentAssignments(ctx, c.ID, personID, false)
			if err != nil {
				return nil, nil, err
			}
			card.PracticeCount = len(list)
			for j, a := range list {
				if j == listCardPractice {
					break
				}
				card.Practice = append(card.Practice, a.Label())
			}
			card.MorePractice = card.PracticeCount - len(card.Practice)
			card.StudentPage = fmt.Sprintf("/communities/%d/students/%d", c.ID, personID)
		}
		out[i] = card
	}
	return out, fonts.list, nil
}

// listLook is a band's or community's look for its card, and adds the
// fonts it needs to fonts.
func (s *Server) listLook(ctx context.Context, kind string, id int64, name string, fonts *fontSet) listCardLook {
	look := listCardLook{Mark: initials(name), Initials: true}
	b, _, err := s.store.GetBrand(ctx, kind, id)
	if err != nil {
		return look
	}
	t := brand.Resolve(b, name)
	if t == nil {
		return look
	}
	look.Brand = t
	look.Logo = t.LogoURL
	if t.Mark != "" {
		look.Mark, look.Initials = t.Mark, false
	}
	fonts.add(t.FontsURL)
	return look
}

// calendarAhead is personID's calendar from now on for a year, without
// what's already over: an all-day gig stays until its day ends, an event
// until it ends (or starts, when it has no end).
func (s *Server) calendarAhead(ctx context.Context, personID int64, now time.Time) ([]store.CalendarItem, error) {
	items, err := s.store.ListCalendarForPerson(ctx, personID, now.Add(-24*time.Hour), now.AddDate(1, 0, 0))
	if err != nil {
		return nil, err
	}
	out := items[:0]
	for _, it := range items {
		end := it.Starts
		switch {
		case it.AllDay:
			end = it.Starts.AddDate(0, 0, 1)
		case it.Ends != nil:
			end = *it.Ends
		}
		if end.After(now) {
			out = append(out, it)
		}
	}
	return out, nil
}

// fontSet is the Google Fonts stylesheets a page's cards need, each once.
type fontSet struct {
	list []string
	seen map[string]bool
}

func (f *fontSet) add(url string) {
	if url == "" || f.seen[url] {
		return
	}
	if f.seen == nil {
		f.seen = map[string]bool{}
	}
	f.seen[url] = true
	f.list = append(f.list, url)
}

// initials is a name's first letters, up to two: "The Paper Lanterns"
// is "PL" ("The" skipped when more follows), "Velvet Hour" "VH", "Léa" "L".
func initials(name string) string {
	words := strings.FieldsFunc(name, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	if len(words) > 2 {
		switch strings.ToLower(words[0]) {
		case "the", "le", "la", "les", "l":
			words = words[1:]
		}
	}
	// "Jam du Canal" is "JC": the capitalised words, when there are two.
	var capital []string
	for _, w := range words {
		if unicode.IsUpper([]rune(w)[0]) {
			capital = append(capital, w)
		}
	}
	if len(capital) >= 2 {
		words = capital
	}
	var out []rune
	for _, w := range words {
		out = append(out, unicode.ToUpper([]rune(w)[0]))
		if len(out) == 2 {
			break
		}
	}
	return string(out)
}

// activityLabel is when something last happened: "just now", "3h ago",
// "5d ago" within a week, the date after that.
func activityLabel(lang string, t, now time.Time) string {
	if now.Sub(t) < 7*24*time.Hour {
		return timeAgo(lang, t)
	}
	if t.Year() == now.Year() {
		return i18n.Date(lang, t, "2 Jan")
	}
	return i18n.Date(lang, t, "2 Jan 2006")
}
