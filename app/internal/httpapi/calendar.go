package httpapi

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"time"

	"app/internal/store"
)

// calendarDay is one cell of a month grid.
type calendarDay struct {
	Date    time.Time
	InMonth bool
	IsToday bool
	IsPast  bool
	Items   []store.CalendarItem
}

// calendarMonth is one month of a person's (or a band's) calendar: the
// grid, Monday first, and the same items again as a list for the agenda
// under it, which is what a phone mostly reads.
type calendarMonth struct {
	Base      string // the page the month links go back to
	Title     string // "November 2026"
	Month     string // "2026-11", for links
	Prev      string
	Next      string
	IsCurrent bool
	Weeks     [][]calendarDay
	Items     []agendaItem
	Today     time.Time
}

// agendaItem is one row of the list under the grid; Anchor is set on the
// first one of its day, which that day's grid cell links to.
type agendaItem struct {
	store.CalendarItem
	Anchor string
}

// parseMonth reads a "2026-11" month parameter; anything else is the
// current month.
func parseMonth(param string, now time.Time) time.Time {
	if t, err := time.ParseInLocation("2006-01", param, now.Location()); err == nil {
		return t
	}
	return time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
}

// monthRange is the span a month grid shows: from the Monday on or
// before the 1st to the Sunday on or after the last day.
func monthRange(first time.Time) (from, to time.Time) {
	from = first.AddDate(0, 0, -((int(first.Weekday()) + 6) % 7))
	last := first.AddDate(0, 1, -1)
	to = last.AddDate(0, 0, 7-((int(last.Weekday())+6)%7))
	return from, to
}

// buildCalendarMonth lays items (which must cover monthRange(first)) out
// on first's month grid.
func buildCalendarMonth(base string, first time.Time, items []store.CalendarItem, now time.Time) calendarMonth {
	from, to := monthRange(first)
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	byDay := map[string][]store.CalendarItem{}
	var inMonth []agendaItem
	for _, it := range items {
		d := it.Starts.In(now.Location())
		key := d.Format("2006-01-02")
		if d.Month() == first.Month() && d.Year() == first.Year() {
			a := agendaItem{CalendarItem: it}
			if len(byDay[key]) == 0 {
				a.Anchor = "day-" + key
			}
			inMonth = append(inMonth, a)
		}
		byDay[key] = append(byDay[key], it)
	}
	cm := calendarMonth{
		Base:      base,
		Title:     first.Format("January 2006"),
		Month:     first.Format("2006-01"),
		Prev:      first.AddDate(0, -1, 0).Format("2006-01"),
		Next:      first.AddDate(0, 1, 0).Format("2006-01"),
		IsCurrent: first.Year() == now.Year() && first.Month() == now.Month(),
		Items:     inMonth,
		Today:     today,
	}
	var week []calendarDay
	for d := from; d.Before(to); d = d.AddDate(0, 0, 1) {
		week = append(week, calendarDay{
			Date: d, InMonth: d.Month() == first.Month(),
			IsToday: d.Equal(today), IsPast: d.Before(today),
			Items: byDay[d.Format("2006-01-02")],
		})
		if len(week) == 7 {
			cm.Weeks = append(cm.Weeks, week)
			week = nil
		}
	}
	return cm
}

// practiceLink is where a calendar item's set list is practised, "" when
// it has none.
func practiceLink(it store.CalendarItem) string {
	if it.SetlistID == nil {
		return ""
	}
	return fmt.Sprintf("/setlists/%d/practice", *it.SetlistID)
}

// songContextQuery keeps the parts of a song page's query string that say
// which list it was opened from, so reopening it from "Continue" lands in
// the same list.
func songContextQuery(q url.Values) string {
	out := url.Values{}
	for _, k := range []string{"setlist", "band", "songbook", "community"} {
		if v := q.Get(k); v != "" {
			if _, err := strconv.ParseInt(v, 10, 64); err == nil || k == "songbook" {
				out.Set(k, v)
			}
		}
	}
	if q.Get("gig") == "1" && out.Get("setlist") != "" {
		out.Set("gig", "1")
	}
	return out.Encode()
}

// nextUpView is the home page's "Next up" card.
type nextUpView struct {
	store.CalendarItem
	Practice string // the set list's practice link, "" without one
	When     string // "Today", "Tomorrow", "Saturday", "Sat 6 Dec"
}

// humanDay says when t is, the way you'd say it: today, tomorrow, a
// weekday within the week, a date after that.
func humanDay(t, now time.Time) string {
	t = t.In(now.Location())
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, now.Location())
	switch n := int(day.Sub(today).Hours() / 24); {
	case n == 0:
		return "Today"
	case n == 1:
		return "Tomorrow"
	case n > 1 && n < 7:
		return t.Format("Monday 2 Jan")
	default:
		return t.Format("Mon 2 Jan")
	}
}

// bandCalendar is personID's calendar for the range, kept to bandID's
// own items: its rehearsals, events and set list gigs.
func (s *Server) bandCalendar(ctx context.Context, personID, bandID int64, from, to time.Time) ([]store.CalendarItem, error) {
	items, err := s.store.ListCalendarForPerson(ctx, personID, from, to)
	if err != nil {
		return nil, err
	}
	var out []store.CalendarItem
	for _, it := range items {
		if it.BandID != nil && *it.BandID == bandID {
			out = append(out, it)
		}
	}
	return out, nil
}

// bandNextUp is the band's next rehearsal, gig or event, nil when nothing
// is planned in the next three months.
func (s *Server) bandNextUp(ctx context.Context, personID, bandID int64) (*nextUpView, error) {
	now := time.Now()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	items, err := s.bandCalendar(ctx, personID, bandID, today, today.AddDate(0, 3, 0))
	if err != nil {
		return nil, err
	}
	return firstUpcoming(items, now), nil
}

// firstUpcoming is the first of items (soonest first) that hasn't
// happened yet; one that started under two hours ago is still on.
func firstUpcoming(items []store.CalendarItem, now time.Time) *nextUpView {
	for _, it := range items {
		if it.AllDay || it.Starts.After(now.Add(-2*time.Hour)) {
			return &nextUpView{CalendarItem: it, Practice: practiceLink(it), When: humanDay(it.Starts, now)}
		}
	}
	return nil
}
