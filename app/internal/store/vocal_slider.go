package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A community's player for singers (migration 0093): its songs' practice
// player has the voice's volume beside play, a mic and a slider, and the
// key out in the open with whose it is (play.html, audio-player.js
// "Sing"). Its staff turn it on or off on the community's admin page.

// SetCommunityPlayerVocalSlider turns a community's player for singers on
// or off.
func (st *Store) SetCommunityPlayerVocalSlider(ctx context.Context, communityID int64, on bool) error {
	tag, err := st.db.Exec(ctx, `UPDATE communities SET player_vocal_slider = $1 WHERE id = $2`, on, communityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("no such community %d: %w", communityID, ErrNotFound)
	}
	return nil
}

// CommunityPlayerVocalSlider reports whether a community has its player
// for singers on (false for no such community).
func (st *Store) CommunityPlayerVocalSlider(ctx context.Context, communityID int64) (bool, error) {
	var on bool
	err := st.db.QueryRow(ctx, `SELECT player_vocal_slider FROM communities WHERE id = $1`, communityID).Scan(&on)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	return on, err
}

// MemberVocalSliderCommunity is the community with its player for
// singers on that a person is a current member of and that has the song
// in one of its songbooks (a student's, a group's, the repertoire): a
// song a student or their coach opens from anywhere. 0 for none.
func (st *Store) MemberVocalSliderCommunity(ctx context.Context, personID, variantID int64) (int64, error) {
	if personID == 0 {
		return 0, nil
	}
	var id int64
	err := st.db.QueryRow(ctx, `
		SELECT c.id FROM communities c
		WHERE c.player_vocal_slider
		  AND EXISTS (SELECT 1 FROM community_members m
		              WHERE m.community_id = c.id AND m.person_id = $1 AND upper_inf(m.validity))
		  AND EXISTS (SELECT 1 FROM songbooks sb JOIN songbook_entries e ON e.songbook_id = sb.id
		              WHERE sb.owner_community_id = c.id AND e.variant_id = $2)
		ORDER BY c.id LIMIT 1`, personID, variantID).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, nil
	}
	return id, err
}

// UpcomingSessions is when each student and each group of a coaching
// community next has a lesson between from and to (a lesson under way at
// from counts): the soonest one's start, by person and by group.
func (st *Store) UpcomingSessions(ctx context.Context, communityID int64, from, to time.Time) (people, groups map[int64]time.Time, err error) {
	people, groups = map[int64]time.Time{}, map[int64]time.Time{}
	rows, err := st.db.Query(ctx, `
		SELECT r.person_id, min(e.starts_at) FROM events e
		JOIN event_registrations r ON r.event_id = e.id
		WHERE e.owner_community_id = $1 AND e.event_type = 'lesson'
		  AND coalesce(e.ends_at, e.starts_at) >= $2 AND e.starts_at < $3
		GROUP BY r.person_id`, communityID, from, to)
	if err != nil {
		return nil, nil, err
	}
	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			rows.Close()
			return nil, nil, err
		}
		people[id] = at
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, nil, err
	}
	rows, err = st.db.Query(ctx, `
		SELECT e.coaching_group_id, min(e.starts_at) FROM events e
		WHERE e.owner_community_id = $1 AND e.event_type = 'lesson' AND e.coaching_group_id IS NOT NULL
		  AND coalesce(e.ends_at, e.starts_at) >= $2 AND e.starts_at < $3
		GROUP BY e.coaching_group_id`, communityID, from, to)
	if err != nil {
		return nil, nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var at time.Time
		if err := rows.Scan(&id, &at); err != nil {
			return nil, nil, err
		}
		groups[id] = at
	}
	return people, groups, rows.Err()
}
