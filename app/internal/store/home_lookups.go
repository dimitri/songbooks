package store

import (
	"context"
	"time"
)

// What the home page (httpapi/home.go) looks up beyond its cards: a
// student's songbooks in their coaching communities, and what's new for
// a person in each of their communities.

// StudentBook is the songbook a coaching community keeps for one of its
// students (kind 'student'), with the community's name.
type StudentBook struct {
	ID            int64
	Name          string
	CommunityID   int64
	CommunityName string
}

// ListStudentBooksForPerson is personID's own student songbooks, one per
// coaching community they are a current member of, by community name.
// A coach's students' songbooks aren't theirs: only the songbooks kept
// for personID as the student are.
func (st *Store) ListStudentBooksForPerson(ctx context.Context, personID int64) ([]StudentBook, error) {
	rows, err := st.db.Query(ctx, `
		SELECT sb.id, sb.name, c.id, c.name
		  FROM songbooks sb JOIN communities c ON c.id = sb.owner_community_id
		 WHERE sb.kind = 'student' AND sb.student_person_id = $1
		   AND EXISTS (SELECT 1 FROM community_members m
		                WHERE m.community_id = c.id AND m.person_id = $1 AND upper_inf(m.validity))
		 ORDER BY lower(c.name), c.id, sb.id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []StudentBook
	for rows.Next() {
		var b StudentBook
		if err := rows.Scan(&b.ID, &b.Name, &b.CommunityID, &b.CommunityName); err != nil {
			return nil, err
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// CountNewCommunitySongs is, for each of communityIDs, how many songs
// were added since `since` to the community's songbooks personID can
// open: its open songbooks and courses, the gated ones personID was
// granted, and their own student songbook (not other students', not a
// coaching group's).
func (st *Store) CountNewCommunitySongs(ctx context.Context, personID int64, communityIDs []int64, since time.Time) (map[int64]int, error) {
	out := map[int64]int{}
	if len(communityIDs) == 0 {
		return out, nil
	}
	rows, err := st.db.Query(ctx, `
		SELECT sb.owner_community_id, count(DISTINCT e.variant_id)
		  FROM songbook_entries e JOIN songbooks sb ON sb.id = e.songbook_id
		 WHERE sb.owner_community_id = ANY($2) AND e.added_at >= $3
		   AND (sb.kind = 'student' AND sb.student_person_id = $1
		        OR sb.kind NOT IN ('student', 'group')
		           AND (NOT sb.requires_grant
		                OR EXISTS (SELECT 1 FROM songbook_access_grants g WHERE g.songbook_id = sb.id AND g.person_id = $1)))
		 GROUP BY sb.owner_community_id`, personID, communityIDs, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var n int
		if err := rows.Scan(&id, &n); err != nil {
			return nil, err
		}
		out[id] = n
	}
	return out, rows.Err()
}
