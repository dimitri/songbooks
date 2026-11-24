package httpapi

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"time"

	"app/internal/brand"
	"app/internal/i18n"
	"app/internal/instruments"
	"app/internal/store"
)

// Accueil's "Aujourd'hui" cards (menu proposal §3.4): the first thing on
// the home page answers "what do I do now?" for each role the person
// has, with one tap to the right place:
//
//   - a band member: tonight's (or tomorrow's) set list;
//   - a jam participant: the next community event they haven't
//     registered for, registered on the spot;
//   - a coach: the next lesson, opened on what to prepare (the student's
//     songs in their key), and the students not heard from lately;
//   - a student: this week's practice and the next lesson.
//
// Several roles give several cards, the most time-critical first; a role
// with nothing coming gives no card.

// todayCard is one of them. Its text is already in the page's language.
type todayCard struct {
	Kind    string    // "band", "jam", "coach" or "student"
	At      time.Time // when what it's about happens: the cards' order
	Eyebrow string    // "Tonight · The Paper Lanterns"
	Title   string
	Meta    string
	Link    string // where the card's action goes
	Action  string // the action's label
	// A practice's days this week, out of how many.
	Done, Of int
	// A second, smaller link (the coach's quiet students).
	More, MoreLink string
	// The jam's: the action registers for this event on the spot (a POST
	// form, as on the event's page), saying what they play when it's an
	// event people play at.
	RegisterEventID int64
	Plays           []string
	AsksPlays       bool
	// The band's or community's own colours, and the text colour that
	// reads on its header.
	Header, HeaderFg, Accent string
}

// Percent is the practice's progress bar, in %.
func (c todayCard) Percent() int {
	if c.Of <= 0 {
		return 0
	}
	return min(100, c.Done*100/c.Of)
}

// How far ahead each kind of card looks, and how long "now" lasts.
const (
	todayBandAhead  = 2 // days: tonight or tomorrow
	todayWeekAhead  = 7 // days: a lesson, a jam
	todayQuietSince = 14
	todayStartedFor = 2 * time.Hour // still "now" this long after it started
)

// todayKindRank breaks ties between cards at the same time: the gig
// before the jam, the lesson to teach before the one to take.
var todayKindRank = map[string]int{"band": 0, "jam": 1, "coach": 2, "student": 3}

// sortTodayCards puts the most time-critical card first.
func sortTodayCards(cards []todayCard) {
	sort.SliceStable(cards, func(i, j int) bool {
		if !cards[i].At.Equal(cards[j].At) {
			return cards[i].At.Before(cards[j].At)
		}
		return todayKindRank[cards[i].Kind] < todayKindRank[cards[j].Kind]
	})
}

// daysFrom is how many calendar days t is after now's day (0 today).
func daysFrom(t, now time.Time) int {
	t = t.In(now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	return int((day.Sub(today).Hours() + 12) / 24) // +12: a DST day is 23 or 25 hours
}

// todayDay says which day t is, from now: "tonight" (today from 17:00),
// "today", "tomorrow", a weekday within the week, else a date.
func todayDay(lang string, t, now time.Time, allDay bool) string {
	t = t.In(now.Location())
	switch n := daysFrom(t, now); {
	case n == 0 && !allDay && t.Hour() >= 17:
		return i18n.T(lang, "today|tonight")
	case n == 0:
		return i18n.T(lang, "today|today")
	case n == 1:
		return i18n.T(lang, "today|tomorrow")
	case n > 1 && n < 7:
		return i18n.Date(lang, t, "Monday")
	default:
		return i18n.Date(lang, t, "Mon 2 Jan")
	}
}

// todayWhen is todayDay with the time: "tomorrow 10:00".
func todayWhen(lang string, t, now time.Time) string {
	return todayDay(lang, t, now, false) + " " + t.In(now.Location()).Format("15:04")
}

// endOfWeek is the last moment of now's week (Monday to Sunday).
func endOfWeek(now time.Time) time.Time {
	d := (7 - int(now.Weekday())) % 7 // days to Sunday
	sunday := time.Date(now.Year(), now.Month(), now.Day()+d, 23, 59, 0, 0, now.Location())
	return sunday
}

func joinDot(parts ...string) string {
	var out []string
	for _, p := range parts {
		if p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// firstName is a display name's first word: "George" for a lesson's
// student, as the lessons' own names say it.
func firstName(name string) string {
	if f := strings.Fields(name); len(f) > 0 {
		return f[0]
	}
	return name
}

// todayCards builds the person's cards. cal is their calendar from today
// on (the home page's own, soonest first).
func (s *Server) todayCards(ctx context.Context, lang string, person *store.Person, cal []store.CalendarItem, now time.Time) ([]todayCard, error) {
	var cards []todayCard
	if c, err := s.todayBandCard(ctx, lang, cal, now); err != nil {
		return nil, err
	} else if c != nil {
		cards = append(cards, *c)
	}
	if c, err := s.todayJamCard(ctx, lang, person, cal, now); err != nil {
		return nil, err
	} else if c != nil {
		cards = append(cards, *c)
	}
	communities, err := s.store.ListCommunityCardsForPerson(ctx, person.ID)
	if err != nil {
		return nil, err
	}
	for _, community := range communities {
		if !community.IsCoaching() {
			continue
		}
		coach, err := s.store.HasAnyCommunityManagePrivilege(ctx, community.ID, person.ID)
		if err != nil {
			return nil, err
		}
		var c *todayCard
		if coach {
			c, err = s.todayCoachCard(ctx, lang, community.ID, community.Name, now)
		} else {
			c, err = s.todayStudentCard(ctx, lang, person.ID, community.ID, community.Name, now)
		}
		if err != nil {
			return nil, err
		}
		if c != nil {
			s.todayLook(ctx, c, "community", community.ID)
			cards = append(cards, *c)
		}
	}
	sortTodayCards(cards)
	return cards, nil
}

// todayLook colours a card in its band's or community's look.
func (s *Server) todayLook(ctx context.Context, c *todayCard, kind string, id int64) {
	b, name, err := s.store.GetBrand(ctx, kind, id)
	if err != nil {
		return
	}
	// The preset's colours under the brand's own (a brand that only
	// names a preset has its header there), and the text in the colour
	// that reads on that header, light or dark.
	look := brand.Resolve(b, name)
	if look == nil {
		return
	}
	c.Header, c.HeaderFg = look.Header, look.HeaderFg()
	c.Accent = look.Accent2
	if c.Accent == "" {
		c.Accent = look.Accent
	}
	if c.Header != "" && c.HeaderFg != "#ffffff" {
		// A light header: the eyebrow in the text's own colour, as an
		// accent made for a dark card wouldn't read on it.
		c.Accent = c.HeaderFg
	}
}

// todayBandCard is the band's set list to play tonight or tomorrow: the
// soonest of the person's bands' dates that has one.
func (s *Server) todayBandCard(ctx context.Context, lang string, cal []store.CalendarItem, now time.Time) (*todayCard, error) {
	for _, it := range cal {
		if it.BandID == nil || it.SetlistID == nil {
			continue
		}
		if days := daysFrom(it.Starts, now); days < 0 || days >= todayBandAhead {
			continue
		}
		if !it.AllDay && it.Starts.Before(now.Add(-todayStartedFor)) {
			continue
		}
		list, meta, _, err := s.store.LoadSetlistByID(ctx, *it.SetlistID)
		if err != nil {
			return nil, err
		}
		songs := 0
		for _, set := range list.Sets {
			songs += len(set.Entries)
		}
		title := meta.Title
		if title == "" {
			title = it.Title
		}
		place := it.Place
		if place == "" && it.Title != title {
			place = it.Title // the event's name, "Concert · La Péniche Rouge"
		}
		if strings.Contains(title, place) {
			place = "" // said already
		}
		if !it.AllDay {
			title = joinDot(title, it.Starts.In(now.Location()).Format("15:04"))
		}
		c := &todayCard{
			Kind: "band", At: it.Starts,
			Eyebrow: joinDot(todayDay(lang, it.Starts, now, it.AllDay), it.GroupName),
			Title:   title,
			Meta:    joinDot(i18n.N(lang, songs, "%d song", "%d songs"), place),
			Link:    setlistLink(it),
			Action:  i18n.T(lang, "Open the set list"),
		}
		s.todayLook(ctx, c, "band", *it.BandID)
		return c, nil
	}
	return nil, nil
}

// todayJamCard is the next event of one of the person's communities (a
// jam, an open mic, a master class: anything but a lesson) in the week
// that they haven't registered for, and didn't set up themselves.
func (s *Server) todayJamCard(ctx context.Context, lang string, person *store.Person, cal []store.CalendarItem, now time.Time) (*todayCard, error) {
	for _, it := range cal {
		if it.CommunityID == nil || it.EventID == 0 || it.Kind == "lesson" {
			continue
		}
		if it.Starts.Before(now.Add(-todayStartedFor)) || daysFrom(it.Starts, now) >= todayWeekAhead {
			continue
		}
		ev, err := s.store.GetEvent(ctx, it.EventID)
		if err != nil {
			return nil, err
		}
		if ev.CreatedByPersonID != nil && *ev.CreatedByPersonID == person.ID {
			continue
		}
		regs, err := s.store.ListEventRegistrations(ctx, ev.ID)
		if err != nil {
			return nil, err
		}
		registered := false
		for _, r := range regs {
			registered = registered || r.PersonID == person.ID
		}
		if registered {
			continue
		}
		c := &todayCard{
			Kind: "jam", At: ev.StartsAt,
			Eyebrow:         joinDot(todayDay(lang, ev.StartsAt, now, false), ev.OwnerName),
			Title:           ev.Name,
			Meta:            joinDot(ev.StartsAt.In(now.Location()).Format("15:04"), ev.Location), // the day is in the eyebrow
			Link:            fmt.Sprintf("/events/%d", ev.ID),
			Action:          i18n.T(lang, "Register"),
			RegisterEventID: ev.ID,
			AsksPlays:       playingEventTypes[ev.EventType],
		}
		if c.AsksPlays {
			mine, err := s.store.ListPersonInstruments(ctx, person.ID)
			if err != nil {
				return nil, err
			}
			for _, pi := range mine {
				if pi.Instrument != instruments.Other {
					c.Plays = append(c.Plays, pi.Instrument)
				}
			}
		}
		s.todayLook(ctx, c, "community", ev.OwnerCommunityID)
		return c, nil
	}
	return nil, nil
}

// todayCoachCard is a coach's next lesson in the week, opened on what to
// prepare: the student's page (their voice, their songs in their key) or
// the group's; and how many students they haven't heard from lately.
func (s *Server) todayCoachCard(ctx context.Context, lang string, communityID int64, communityName string, now time.Time) (*todayCard, error) {
	c := &todayCard{Kind: "coach", At: endOfWeek(now), Eyebrow: joinDot(i18n.T(lang, "Next lesson"), communityName)}
	lessons, err := s.store.ListCoachingLessons(ctx, communityID, now, 0)
	if err != nil {
		return nil, err
	}
	for _, l := range lessons {
		ev := l.Event
		if ev.IsPast(now) || daysFrom(ev.StartsAt, now) >= todayWeekAhead {
			continue
		}
		who := ev.Name
		c.Link = fmt.Sprintf("/communities/%d/lessons", communityID)
		var meta []string
		switch {
		case ev.CoachingGroupID != nil:
			g, err := s.store.GetCoachingGroup(ctx, *ev.CoachingGroupID)
			if err != nil {
				return nil, err
			}
			who = g.Name
			c.Link = fmt.Sprintf("/communities/%d/groups/%d", communityID, g.ID)
			meta = append(meta, i18n.N(lang, len(l.Students), "%d student", "%d students"))
		case len(l.Students) == 1:
			st := l.Students[0]
			who = firstName(st.DisplayName)
			c.Link = fmt.Sprintf("/communities/%d/students/%d", communityID, st.PersonID)
			sb, err := s.store.FindStudentSongbook(ctx, communityID, st.PersonID)
			if err != nil {
				return nil, err
			}
			if sb != 0 {
				songs, err := s.store.ListStudentSongs(ctx, sb)
				if err != nil {
					return nil, err
				}
				meta = append(meta, i18n.N(lang, len(songs), "%d song in their key", "%d songs in their key"))
			}
		case len(l.Students) > 1:
			var names []string
			for _, st := range l.Students {
				names = append(names, firstName(st.DisplayName))
			}
			who = strings.Join(names, ", ")
		}
		if ev.LessonMode == "online" {
			meta = append(meta, i18n.T(lang, "online"))
		}
		meta = append(meta, ev.Location)
		c.At = ev.StartsAt
		c.Title = who + ", " + todayWhen(lang, ev.StartsAt, now)
		c.Meta = joinDot(meta...)
		c.Action = i18n.T(lang, "Prepare")
		break
	}
	quiet, err := s.store.QuietStudents(ctx, communityID, now.AddDate(0, 0, -todayQuietSince), now)
	if err != nil {
		return nil, err
	}
	if quiet > 0 {
		c.More = i18n.N(lang, quiet, "%d student not heard from in 2 weeks", "%d students not heard from in 2 weeks")
		c.MoreLink = fmt.Sprintf("/communities/%d/students", communityID)
	}
	if c.Title == "" && c.More == "" {
		return nil, nil
	}
	if c.Title == "" {
		c.Title = i18n.T(lang, "No lesson this week")
	}
	return c, nil
}

// todayStudentCard is a student's week with their coach: the practice
// still to do this week (the first one short of its days), and their next
// lesson in the week.
func (s *Server) todayStudentCard(ctx context.Context, lang string, personID, communityID int64, communityName string, now time.Time) (*todayCard, error) {
	assignments, err := s.store.ListStudentAssignments(ctx, communityID, personID, false)
	if err != nil {
		return nil, err
	}
	var next *store.Event
	lessons, err := s.store.ListCoachingLessons(ctx, communityID, now, personID)
	if err != nil {
		return nil, err
	}
	for _, l := range lessons {
		if !l.Event.IsPast(now) && daysFrom(l.Event.StartsAt, now) < todayWeekAhead {
			ev := l.Event
			next = &ev
			break
		}
	}
	if len(assignments) == 0 && next == nil {
		return nil, nil
	}
	c := &todayCard{Kind: "student", At: endOfWeek(now), Eyebrow: joinDot(i18n.T(lang, "today|This week"), communityName)}
	var lesson string
	if next != nil {
		c.At = next.StartsAt
		lesson = i18n.T(lang, "lesson %s", todayWhen(lang, next.StartsAt, now))
	}
	if len(assignments) == 0 {
		c.Title = i18n.T(lang, "Lesson %s", todayWhen(lang, next.StartsAt, now))
		c.Meta = joinDot(next.Name, next.Location)
		c.Link = fmt.Sprintf("/communities/%d/lessons", communityID)
		c.Action = i18n.T(lang, "See my lessons")
		return c, nil
	}
	a := assignments[0]
	for _, x := range assignments {
		if x.PerWeek > 0 && x.DaysThisWeek < x.PerWeek {
			a = x
			break
		}
	}
	c.Title = a.Label()
	var week string
	if a.PerWeek > 0 {
		c.Done, c.Of = min(a.DaysThisWeek, a.PerWeek), a.PerWeek
		week = i18n.T(lang, "%d/%d this week", a.DaysThisWeek, a.PerWeek)
	}
	more := ""
	if n := len(assignments) - 1; n > 0 {
		more = i18n.N(lang, n, "+%d other", "+%d others")
	}
	c.Meta = joinDot(week, lesson, more)
	switch {
	case a.ExerciseID != nil:
		c.Link = fmt.Sprintf("/communities/%d/warmups/%d", communityID, *a.ExerciseID)
	case a.VariantID != nil && a.SongbookID != 0:
		c.Link = fmt.Sprintf("/song/%d?songbook=%d", *a.VariantID, a.SongbookID)
	case a.VariantID != nil:
		c.Link = fmt.Sprintf("/song/%d?community=%d", *a.VariantID, communityID)
	default:
		c.Link = fmt.Sprintf("/communities/%d/students/%d#practice", communityID, personID)
	}
	c.Action = i18n.T(lang, "today|Start")
	return c, nil
}
