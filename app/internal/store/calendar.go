package store

import (
	"context"
	"strings"
	"time"
)

// CalendarItem is one dated thing on a person's calendar: a gig attached
// to a set list (setlist_gigs), or an event of one of their bands or
// communities (events), rehearsals included.
type CalendarItem struct {
	Kind      string // "gig", "rehearsal" or "event"
	Title     string
	Starts    time.Time
	AllDay    bool // a set list gig has a date and no time
	Place     string
	GroupName string // the band or community, "" for a personal set list
	BandID    *int64
	Link      string
	SetlistID *int64 // the set list to play or practise, when there is one
}

// ListCalendarForPerson lists everything on personID's calendar from
// `from` (inclusive) to `to` (exclusive), soonest first: gigs of their
// own and their bands' set lists, and the events of the bands and
// communities they currently belong to.
func (st *Store) ListCalendarForPerson(ctx context.Context, personID int64, from, to time.Time) ([]CalendarItem, error) {
	rows, err := st.db.Query(ctx, `
		WITH my_bands AS (
		  SELECT band_id FROM band_members WHERE person_id = $1 AND upper_inf(validity)
		), my_communities AS (
		  SELECT community_id FROM community_members WHERE person_id = $1 AND upper_inf(validity)
		)
		SELECT 'gig', sl.title, g.date::timestamptz, true, g.venue, coalesce(b.name, ''), b.id,
		       '/setlists/' || sl.id, sl.id
		FROM setlist_gigs g
		JOIN setlists sl ON sl.id = g.setlist_id
		LEFT JOIN bands b ON b.id = sl.owner_band_id
		WHERE g.date >= $2::date AND g.date < $3::date
		  AND (sl.owner_person_id = $1 OR sl.owner_band_id IN (SELECT band_id FROM my_bands))
		UNION ALL
		SELECT CASE e.event_type WHEN 'rehearsal' THEN 'rehearsal' WHEN 'concert' THEN 'gig' ELSE 'event' END,
		       e.name, e.starts_at, false, e.location, coalesce(b.name, c.name, ''), b.id,
		       '/events/' || e.id, e.setlist_id
		FROM events e
		LEFT JOIN bands b ON b.id = e.owner_band_id
		LEFT JOIN communities c ON c.id = e.owner_community_id
		WHERE e.starts_at >= $2 AND e.starts_at < $3
		  AND (e.owner_band_id IN (SELECT band_id FROM my_bands)
		       OR e.owner_community_id IN (SELECT community_id FROM my_communities))
		ORDER BY 3, 2`, personID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []CalendarItem
	for rows.Next() {
		var it CalendarItem
		if err := rows.Scan(&it.Kind, &it.Title, &it.Starts, &it.AllDay, &it.Place, &it.GroupName, &it.BandID, &it.Link, &it.SetlistID); err != nil {
			return nil, err
		}
		out = append(out, it)
	}
	return out, rows.Err()
}

// KindLabel says what kind of date it is: "Gig", "Rehearsal" or "Event".
func (it CalendarItem) KindLabel() string {
	switch it.Kind {
	case "gig":
		return "Gig"
	case "rehearsal":
		return "Rehearsal"
	}
	return "Event"
}

// Details is the line under the item's title: its kind (unless the title
// already says it), the band or community when withGroup, and the place.
func (it CalendarItem) Details(withGroup bool) string {
	var parts []string
	if k := it.KindLabel(); k != it.Title {
		parts = append(parts, k)
	}
	if withGroup && it.GroupName != "" {
		parts = append(parts, it.GroupName)
	}
	if it.Place != "" {
		parts = append(parts, it.Place)
	}
	return strings.Join(parts, " · ")
}
