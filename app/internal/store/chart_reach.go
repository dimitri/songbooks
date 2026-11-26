package store

import (
	"context"
)

// Who plays a chart (the edit model, docs/catalogue-edit-review.md step
// 7): the editor's note on The Big Songbook's song says who a fix
// reaches — how many songbooks and set lists link the song, how many
// arrangements are offered its changes — and names the viewer's own
// places among them, each with its key. Others' places are counted, never
// named.

// ChartPlace is one of the viewer's places holding the song: a songbook
// (their own, a band's, a community's) and its key there.
type ChartPlace struct {
	SongbookID int64
	Name       string
	Owner      string // the band's or community's name, "" for one's own
	Semitones  int
}

// ChartReach is who plays a chart.
type ChartReach struct {
	Songbooks, Setlists, Arrangements int
	Mine                              []ChartPlace
}

// ChartReachOf is songID's reach, personID's places named.
func (st *Store) ChartReachOf(ctx context.Context, songID, personID int64) (*ChartReach, error) {
	var r ChartReach
	if err := st.db.QueryRow(ctx, `
		SELECT (SELECT count(DISTINCT songbook_id) FROM songbook_entries WHERE variant_id = $1),
		       (SELECT count(DISTINCT setlist_id) FROM setlist_items WHERE variant_id = $1),
		       (SELECT count(*) FROM songs WHERE forked_from_id = $1 AND frozen_at IS NULL AND base_body IS NOT NULL)`, songID).
		Scan(&r.Songbooks, &r.Setlists, &r.Arrangements); err != nil {
		return nil, err
	}
	rows, err := st.db.Query(ctx, `
		SELECT sb.id, sb.name, coalesce(b.name, c.name, ''), se.semitones
		FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		LEFT JOIN bands b ON b.id = sb.owner_band_id
		LEFT JOIN communities c ON c.id = sb.owner_community_id
		WHERE se.variant_id = $1 AND (
		  sb.owner_person_id = $2
		  OR (sb.kind = 'student' AND sb.student_person_id = $2)
		  OR EXISTS (SELECT 1 FROM band_members bm WHERE bm.band_id = sb.owner_band_id AND bm.person_id = $2 AND upper_inf(bm.validity))
		  OR (sb.kind NOT IN ('student', 'group') AND EXISTS (SELECT 1 FROM community_members cm WHERE cm.community_id = sb.owner_community_id AND cm.person_id = $2 AND upper_inf(cm.validity))))
		ORDER BY sb.owner_person_id IS NULL, coalesce(b.name, c.name, ''), sb.name, sb.id`, songID, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var p ChartPlace
		if err := rows.Scan(&p.SongbookID, &p.Name, &p.Owner, &p.Semitones); err != nil {
			return nil, err
		}
		r.Mine = append(r.Mine, p)
	}
	return &r, rows.Err()
}
