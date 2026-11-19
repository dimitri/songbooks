package store

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// A coaching community's song player has two options of its own, each
// its staff's to turn on or off on the community's admin page, and on
// only for a coaching community:
//
//   - player_vocal_slider (migration 0093): the voice's volume, a mic and
//     a slider, in the player of the community's songs (play.html, the
//     "Voice & key" panel; audio-player.js "Sing");
//   - coach_student_keys (migration 0097): in the songs opened from its
//     songbooks, the coach picks a student in that panel and saves their
//     key, the coming week's lessons first (httpapi/coach_keys.go).

// ErrNotCoaching: a player option turned on for a community that isn't a
// coaching one.
var ErrNotCoaching = errors.New("not a coaching community")

// setPlayerOption turns one of a coaching community's player options on
// or off; on, for a coaching community only.
func (st *Store) setPlayerOption(ctx context.Context, column string, communityID int64, on bool) error {
	tag, err := st.db.Exec(ctx, `UPDATE communities SET `+column+` = $1 WHERE id = $2 AND (kind = 'coaching' OR NOT $1)`, on, communityID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		if _, err := st.GetCommunity(ctx, communityID); err != nil {
			return err
		}
		return fmt.Errorf("community %d: %w", communityID, ErrNotCoaching)
	}
	return nil
}

// SetCommunityPlayerVocalSlider turns a coaching community's voice
// slider on or off.
func (st *Store) SetCommunityPlayerVocalSlider(ctx context.Context, communityID int64, on bool) error {
	return st.setPlayerOption(ctx, "player_vocal_slider", communityID, on)
}

// SetCommunityCoachStudentKeys turns a coaching community's key per
// student on or off.
func (st *Store) SetCommunityCoachStudentKeys(ctx context.Context, communityID int64, on bool) error {
	return st.setPlayerOption(ctx, "coach_student_keys", communityID, on)
}

// PlayerOptions are a community's song player options: both off for a
// community that isn't a coaching one, or no such community.
type PlayerOptions struct {
	VocalSlider      bool
	CoachStudentKeys bool
}

// CommunityPlayerOptions reads a community's player options.
func (st *Store) CommunityPlayerOptions(ctx context.Context, communityID int64) (PlayerOptions, error) {
	var o PlayerOptions
	err := st.db.QueryRow(ctx, `
		SELECT player_vocal_slider, coach_student_keys FROM communities
		WHERE id = $1 AND kind = 'coaching'`, communityID).Scan(&o.VocalSlider, &o.CoachStudentKeys)
	if errors.Is(err, pgx.ErrNoRows) {
		return PlayerOptions{}, nil
	}
	return o, err
}

// CommunityPlayerVocalSlider reports whether a community has its voice
// slider on (false for no such community, or not a coaching one).
func (st *Store) CommunityPlayerVocalSlider(ctx context.Context, communityID int64) (bool, error) {
	o, err := st.CommunityPlayerOptions(ctx, communityID)
	return o.VocalSlider, err
}

// StudentKeyIn is StudentKeyOf within one community: the key the coach
// gave the song in the student's songbook there. A student's key is for
// the songs they practise in that community, not in a band or elsewhere.
func (st *Store) StudentKeyIn(ctx context.Context, personID, communityID, variantID int64) (songbookID int64, semitones int, ok bool, err error) {
	err = st.db.QueryRow(ctx, `
		SELECT sb.id, e.semitones FROM songbooks sb
		JOIN songbook_entries e ON e.songbook_id = sb.id
		WHERE sb.kind = 'student' AND sb.student_person_id = $1 AND sb.owner_community_id = $2 AND e.variant_id = $3
		ORDER BY sb.id LIMIT 1`, personID, communityID, variantID).Scan(&songbookID, &semitones)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, false, nil
	}
	return songbookID, semitones, err == nil, err
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
		WHERE c.player_vocal_slider AND c.kind = 'coaching'
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
