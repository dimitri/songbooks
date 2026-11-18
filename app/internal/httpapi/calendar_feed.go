package httpapi

import (
	"net/http"
	"net/url"
	"strings"
	"time"

	"app/internal/store"
)

// A person's calendar as an iCal feed (RFC 5545), at a secret address a
// calendar app subscribes to: their bands' and communities' events and
// gigs, and their lessons (a coaching community's, docs/coaching.md),
// from three months back to a year ahead. The address is the secret: no
// login; a new one can be made, and the old one stops.

// feedBase is the site's address, as the request reached it.
func feedBase(r *http.Request) string {
	scheme := "https"
	if r.TLS == nil && r.Header.Get("X-Forwarded-Proto") != "https" && strings.HasPrefix(r.Host, "127.0.0.1") {
		scheme = "http"
	}
	return scheme + "://" + r.Host
}

// handleCalendarFeed is GET /calendar/feed/{token} (".ics" or not).
func (s *Server) handleCalendarFeed(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimSuffix(r.PathValue("token"), ".ics")
	personID, err := s.store.PersonByCalendarToken(r.Context(), token)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if personID == 0 {
		http.NotFound(w, r)
		return
	}
	now := time.Now()
	items, err := s.store.ListCalendarForPerson(r.Context(), personID, now.AddDate(0, -3, 0), now.AddDate(1, 0, 0))
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	base := feedBase(r)
	w.Header().Set("Content-Type", "text/calendar; charset=utf-8")
	w.Write([]byte(buildFeed(items, base, now)))
}

func buildFeed(items []store.CalendarItem, base string, now time.Time) string {
	stamp := func(t time.Time) string { return t.UTC().Format("20060102T150405Z") }
	var b strings.Builder
	line := func(s string) { b.WriteString(foldICS(s) + "\r\n") }
	line("BEGIN:VCALENDAR")
	line("VERSION:2.0")
	line("PRODID:-//Songbooks//Calendar//EN")
	line("CALSCALE:GREGORIAN")
	line("X-WR-CALNAME:Songbooks")
	line("REFRESH-INTERVAL;VALUE=DURATION:PT1H")
	line("X-PUBLISHED-TTL:PT1H")
	for _, it := range items {
		line("BEGIN:VEVENT")
		line("UID:" + it.UID + "@songbooks")
		line("DTSTAMP:" + stamp(now))
		if it.AllDay {
			d := it.Starts.Format("20060102")
			line("DTSTART;VALUE=DATE:" + d)
			line("DTEND;VALUE=DATE:" + it.Starts.AddDate(0, 0, 1).Format("20060102"))
		} else {
			end := it.Starts.Add(2 * time.Hour)
			if it.Ends != nil {
				end = *it.Ends
			}
			line("DTSTART:" + stamp(it.Starts))
			line("DTEND:" + stamp(end))
		}
		title := it.Title
		if it.GroupName != "" && it.GroupName != it.Title {
			title += " — " + it.GroupName
		}
		line("SUMMARY:" + icsEscape(title))
		place := it.Place
		var desc []string
		switch it.LessonMode {
		case "online":
			desc = append(desc, "Online lesson")
			if place == "" {
				place = "Online"
			}
		case "in_person":
			desc = append(desc, "Lesson in the room")
		}
		if it.Description != "" {
			desc = append(desc, it.Description)
		}
		url := base + it.Link
		desc = append(desc, url)
		if place != "" {
			line("LOCATION:" + icsEscape(place))
		}
		line("DESCRIPTION:" + icsEscape(strings.Join(desc, "\n")))
		line("URL:" + url)
		line("END:VEVENT")
	}
	line("END:VCALENDAR")
	return b.String()
}

// foldICS folds a content line at 75 octets, as RFC 5545 asks.
func foldICS(s string) string {
	if len(s) <= 75 {
		return s
	}
	var b strings.Builder
	n := 0
	for _, r := range s {
		size := len(string(r))
		if n+size > 75 {
			b.WriteString("\r\n ")
			n = 1
		}
		b.WriteRune(r)
		n += size
	}
	return b.String()
}

// handleCalendarSubscribePage is GET /calendar/subscribe: the address of
// the person's feed, to add to a calendar app.
func (s *Server) handleCalendarSubscribePage(w http.ResponseWriter, r *http.Request) {
	person := personFromContext(r)
	token, err := s.store.CalendarFeedToken(r.Context(), person.ID)
	if err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	feed := feedBase(r) + "/calendar/feed/" + token + ".ics"
	webcal := "webcal://" + strings.SplitN(feed, "://", 2)[1]
	s.render(w, "calendar_subscribe", map[string]any{
		"Person":  person,
		"FeedURL": feed,
		"Webcal":  webcal,
		"Google":  "https://calendar.google.com/calendar/r?cid=" + url.QueryEscape(webcal),
	})
}

// handleResetCalendarFeed is POST /api/calendar/feed/reset: a new address,
// the old one stops working.
func (s *Server) handleResetCalendarFeed(w http.ResponseWriter, r *http.Request) {
	if _, err := s.store.ResetCalendarFeed(r.Context(), personFromContext(r).ID); err != nil {
		httpError(w, http.StatusInternalServerError, err.Error())
		return
	}
	w.WriteHeader(http.StatusNoContent)
}
