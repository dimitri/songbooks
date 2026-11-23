package store

import (
	"context"
	"encoding/json"
	"sort"
	"strings"
	"time"

	"app/internal/songdiff"
)

// The changes in a person's songs, on their home page ("Changes in your
// songs", docs/ownership-model.md §5): no notification is sent for a
// change upstream; the home page lists it as news instead, until the
// person marks the list seen (people.song_news_seen_at, migration 0133).
//
// A person's songs: those in their songbooks (their own, their student
// songbooks), in their current bands' songbooks (not an archived
// band's), and the originals of the arrangements they may take changes
// into (theirs, their bands', their communities' repertoire's: the
// versionEditors). A change is an accepted one: a revision of the song —
// a save, a catalogue edit applied or approved (a pending edit makes
// none), a revert, a reference or its label changed — by someone else,
// since the person had the song, since they last marked the list seen
// and at most SongNewsWindow ago. Only songs they may still open.

// SongNewsWindow is how far back the list goes.
const SongNewsWindow = 30 * 24 * time.Hour

// SongNews is the latest change to one of a person's songs.
type SongNews struct {
	SongID                     int64
	Title, Artist, VersionName string
	Catalogue                  bool
	RevisionID                 int64 // the latest change's revision
	At                         time.Time
	ActorID                    int64 // 0: no one (an import)
	ActorName                  string
	Summary                    string // its one-line summary, "" for none
	// Before and After are the song before and after the latest change,
	// to say what it changed when it has no summary.
	Before, After VariantSnapshot
	Changes       int // changes since, the latest counted (not those of the layout or comments alone)
	// Why the person has it: "arrangement" (the original of theirs),
	// "band" (BandName's songbook), "songbook" (theirs).
	Via      string
	BandName string
	// ArrangementID is the person's (or their band's, community's)
	// arrangement of the song, when it has changes of it to take; 0
	// otherwise.
	ArrangementID int64
}

// SongNewsSeenAt is when personID last marked their changes seen (zero:
// never).
func (st *Store) SongNewsSeenAt(ctx context.Context, personID int64) (time.Time, error) {
	var at *time.Time
	if err := st.db.QueryRow(ctx, `SELECT song_news_seen_at FROM people WHERE id = $1`, personID).Scan(&at); err != nil {
		return time.Time{}, err
	}
	if at == nil {
		return time.Time{}, nil
	}
	return *at, nil
}

// SeeSongNews marks personID's changes seen up to upTo (the newest one
// the page showed, so one arriving meanwhile stays); never back.
func (st *Store) SeeSongNews(ctx context.Context, personID int64, upTo time.Time) error {
	if upTo.IsZero() || upTo.After(time.Now()) {
		upTo = time.Now()
	}
	_, err := st.db.Exec(ctx, `
		UPDATE people SET song_news_seen_at = greatest(coalesce(song_news_seen_at, '-infinity'), $2) WHERE id = $1`,
		personID, upTo)
	return err
}

// SongNewsFor is the latest change to each of personID's songs, newest
// first, at most limit songs (0: all).
func (st *Store) SongNewsFor(ctx context.Context, personID int64, limit int) ([]SongNews, error) {
	seen, err := st.SongNewsSeenAt(ctx, personID)
	if err != nil {
		return nil, err
	}
	since := time.Now().Add(-SongNewsWindow)
	if seen.After(since) {
		since = seen
	}
	rows, err := st.db.Query(ctx, `
		WITH held AS (
		  -- In their own songbooks, their student songbooks.
		  SELECT se.variant_id AS song_id, se.added_at AS since, 2 AS rank, ''::text AS band, 0::bigint AS arrangement
		  FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		  WHERE sb.owner_person_id = $1 OR (sb.kind = 'student' AND sb.student_person_id = $1)
		  UNION ALL
		  -- In their current bands' songbooks.
		  SELECT se.variant_id, greatest(se.added_at, lower(bm.validity)), 1, b.name, 0
		  FROM songbook_entries se JOIN songbooks sb ON sb.id = se.songbook_id
		  JOIN bands b ON b.id = sb.owner_band_id AND b.archived_at IS NULL
		  JOIN band_members bm ON bm.band_id = b.id AND bm.person_id = $1 AND upper_inf(bm.validity)
		  UNION ALL
		  -- The originals of the arrangements they take changes into.
		  SELECT v.forked_from_id, v.created_at, 0, '', v.id
		  FROM songs v
		  WHERE v.forked_from_id IS NOT NULL AND v.base_body IS NOT NULL AND v.frozen_at IS NULL
		    AND (v.owner_person_id = $1
		         OR (v.owner_band_id IS NOT NULL AND has_band_privilege(v.owner_band_id, $1, $3))
		         OR EXISTS (SELECT 1 FROM community_members cm JOIN community_role_privileges crp ON crp.role_id = cm.role_id
		                    WHERE cm.community_id = v.owner_community_id AND cm.person_id = $1
		                      AND upper_inf(cm.validity) AND crp.privilege = $4))
		),
		had AS (SELECT song_id, min(since) AS since FROM held GROUP BY song_id),
		via AS (SELECT DISTINCT ON (song_id) song_id, rank, band, arrangement FROM held ORDER BY song_id, rank, arrangement DESC),
		revs AS (
		  SELECT r.id, h.song_id, r.actor_person_id, r.summary, r.snapshot, r.created_at,
		         lead(r.snapshot) OVER (PARTITION BY h.song_id ORDER BY r.created_at, r.id) AS next_snapshot
		  FROM had h JOIN revisions r ON r.entity_type = 'variant' AND r.entity_id = h.song_id::text
		  WHERE r.created_at > greatest(h.since, $2)
		),
		theirs AS (SELECT * FROM revs WHERE actor_person_id IS DISTINCT FROM $1)
		SELECT t.song_id, t.id, coalesce(t.actor_person_id, 0), coalesce(p.display_name, ''), t.summary,
		       t.snapshot, t.next_snapshot, t.created_at,
		       s.title, s.artist, s.version_name, s.reference_url, s.body, coalesce(`+catalogueSQL("s")+`, false),
		       v.rank, v.band, v.arrangement,
		       coalesce((SELECT a.base_body FROM songs a WHERE a.id = v.arrangement), '')
		FROM theirs t
		JOIN songs s ON s.id = t.song_id
		JOIN via v ON v.song_id = t.song_id
		LEFT JOIN people p ON p.id = t.actor_person_id
		WHERE song_visible(t.song_id, $1)
		ORDER BY t.song_id, t.created_at DESC, t.id DESC`,
		personID, since, BandPrivEditSongbook, PrivEditRepertoire)
	if err != nil {
		return nil, err
	}
	// Each song's changes, newest first: the latest that changed
	// something (not only the layout or comments) is its news.
	bySong := map[int64]*SongNews{}
	var order []int64
	for rows.Next() {
		var n SongNews
		var snap, next []byte
		var rank int
		var ref, body, base string
		if err := rows.Scan(&n.SongID, &n.RevisionID, &n.ActorID, &n.ActorName, &n.Summary,
			&snap, &next, &n.At,
			&n.Title, &n.Artist, &n.VersionName, &ref, &body, &n.Catalogue,
			&rank, &n.BandName, &n.ArrangementID, &base); err != nil {
			rows.Close()
			return nil, err
		}
		_ = json.Unmarshal(snap, &n.Before)
		label := n.VersionName
		if next != nil {
			_ = json.Unmarshal(next, &n.After)
		} else {
			n.After = VariantSnapshot{Title: n.Title, Artist: n.Artist, ReferenceURL: ref, Body: body}
		}
		if n.After.VersionName == nil {
			n.After.VersionName = &label
		}
		if !changedSomething(n.Before, n.After) {
			continue
		}
		if seen := bySong[n.SongID]; seen != nil {
			seen.Changes++
			continue
		}
		n.Changes = 1
		switch rank {
		case 0:
			n.Via = "arrangement"
		case 1:
			n.Via = "band"
		default:
			n.Via = "songbook"
		}
		// Changes to take: the original's chart now is another than the
		// one the arrangement last took or kept.
		if n.ArrangementID != 0 && !songdiff.ChartChanged(base, body) {
			n.ArrangementID = 0
		}
		bySong[n.SongID] = &n
		order = append(order, n.SongID)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	found := make([]SongNews, 0, len(order))
	for _, id := range order {
		found = append(found, *bySong[id])
	}
	sort.SliceStable(found, func(i, j int) bool { return found[i].At.After(found[j].At) })
	var out []SongNews
	for _, n := range found {
		// The access rule's last word (a coaching community's songs, a
		// closed songbook): only what they may open.
		if r, err := st.SongRightsFor(ctx, personID, n.SongID); err != nil || !r.See {
			continue
		}
		out = append(out, n)
		if limit > 0 && len(out) == limit {
			break
		}
	}
	return out, nil
}

// changedSomething reports whether a change from before to after is
// worth telling: its chart (not only the layout or comments:
// songdiff.ChartChanged), its title, artist, reference or label.
func changedSomething(before, after VariantSnapshot) bool {
	if before.VersionName != nil && after.VersionName != nil && *before.VersionName != *after.VersionName {
		return true
	}
	return before.Title != after.Title || before.Artist != after.Artist ||
		strings.TrimSpace(before.ReferenceURL) != strings.TrimSpace(after.ReferenceURL) ||
		songdiff.ChartChanged(before.Body, after.Body)
}
