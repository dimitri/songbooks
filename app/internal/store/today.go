package store

import (
	"context"
	"time"
)

// QuietStudents counts a coaching community's students the coach hasn't
// heard from since `since` (Accueil's "N students not heard from in 2
// weeks"): current members who aren't staff, members since before then,
// with no practice logged, no take checked and no lesson attended from
// since until now.
func (st *Store) QuietStudents(ctx context.Context, communityID int64, since, now time.Time) (int, error) {
	var n int
	err := st.db.QueryRow(ctx, `
		SELECT count(*)
		FROM community_members m
		JOIN community_roles cr ON cr.id = m.role_id
		WHERE m.community_id = $1 AND upper_inf(m.validity) AND cr.name <> 'staff'
		  AND lower(m.validity) < $2
		  AND NOT EXISTS (SELECT 1 FROM practice_logs l
		                  WHERE l.community_id = $1 AND l.person_id = m.person_id AND l.practiced_on >= $2::date)
		  AND NOT EXISTS (SELECT 1 FROM practice_takes t
		                  WHERE t.community_id = $1 AND t.person_id = m.person_id AND t.created_at >= $2)
		  AND NOT EXISTS (SELECT 1 FROM event_registrations r JOIN events e ON e.id = r.event_id
		                  WHERE e.owner_community_id = $1 AND e.event_type = 'lesson' AND r.person_id = m.person_id
		                    AND e.starts_at >= $2 AND e.starts_at < $3)`,
		communityID, since, now).Scan(&n)
	return n, err
}
