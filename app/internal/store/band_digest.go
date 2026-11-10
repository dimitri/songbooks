package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// BandChange is one entry in a band's "since your last visit" digest —
// deliberately as simple as ListRevisions' own "~N lines changed"
// convention: a timestamp, who (if known), and a plain summary string,
// no structured diff.
type BandChange struct {
	CreatedAt time.Time
	ActorName string // "" if unknown
	Summary   string
}

// ListBandChanges builds bandID's "changes since your last visit"
// digest: repertoire adds/removes, variant edits, new members, and
// proposal/vote activity, all created_at > since (nil since = "everything
// ever," used the first time someone ever views the band page, before
// any band_member_last_seen row exists for them). Reverse-chronological,
// capped at 100 entries — this is a digest, not a full audit log (see
// /variants/{id}/history and ListRevisions for the latter).
func (st *Store) ListBandChanges(ctx context.Context, bandID int64, since *time.Time) ([]BandChange, error) {
	var out []BandChange

	variantRows, err := st.db.Query(ctx, `
		SELECT r.created_at, coalesce(pr.display_name, ''), s.title
		FROM revisions r
		JOIN songs s ON s.id = CAST(r.entity_id AS bigint)
		LEFT JOIN people pr ON pr.id = r.actor_person_id
		WHERE r.entity_type = 'variant' AND s.owner_band_id = $1
		  AND ($2::timestamptz IS NULL OR r.created_at > $2)
		ORDER BY r.created_at DESC`, bandID, since)
	if err != nil {
		return nil, err
	}
	for variantRows.Next() {
		var createdAt time.Time
		var actorName, title string
		if err := variantRows.Scan(&createdAt, &actorName, &title); err != nil {
			variantRows.Close()
			return nil, err
		}
		out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("edited %q", title)})
	}
	variantRows.Close()
	if err := variantRows.Err(); err != nil {
		return nil, err
	}

	// Songbook-entry adds/removes (what band_catalog_entry revisions used
	// to cover under the old project/repertoire model) aren't tracked in
	// the digest any more — songbook_entries has no revision history of
	// its own yet; a focused follow-up if the "added/removed from the
	// songbook" digest line is missed.

	memberRows, err := st.db.Query(ctx, `
		SELECT r.created_at, coalesce(pr.display_name, ''), p.display_name, (r.snapshot->>'exists')::boolean, bm.status::text,
		       CASE WHEN upper_inf(bm.validity) THEN NULL ELSE upper(bm.validity) END
		FROM revisions r
		JOIN band_members bm ON bm.id = CAST(r.entity_id AS bigint)
		JOIN people p ON p.id = bm.person_id
		LEFT JOIN people pr ON pr.id = r.actor_person_id
		WHERE r.entity_type = 'band_member' AND bm.band_id = $1
		  AND ($2::timestamptz IS NULL OR r.created_at > $2)
		ORDER BY r.created_at DESC`, bandID, since)
	if err != nil {
		return nil, err
	}
	for memberRows.Next() {
		var createdAt time.Time
		var actorName, memberName, status string
		var preExisted bool
		var leftAt *time.Time
		if err := memberRows.Scan(&createdAt, &actorName, &memberName, &preExisted, &status, &leftAt); err != nil {
			memberRows.Close()
			return nil, err
		}
		switch {
		case !preExisted:
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("%s joined as %s", memberName, status)})
		case leftAt != nil:
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("%s left the band", memberName)})
		default:
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("%s's status changed to %s", memberName, status)})
		}
	}
	memberRows.Close()
	if err := memberRows.Err(); err != nil {
		return nil, err
	}

	proposalRows, err := st.db.Query(ctx, `
		SELECT r.created_at, coalesce(pr.display_name, ''), bp.kind::text, coalesce(s.title, ''), (r.snapshot->>'exists')::boolean, bp.status::text
		FROM revisions r
		JOIN band_proposals bp ON bp.id = CAST(r.entity_id AS bigint)
		LEFT JOIN songs s ON s.id = bp.target_variant_id
		LEFT JOIN people pr ON pr.id = r.actor_person_id
		WHERE r.entity_type = 'band_proposal' AND bp.band_id = $1
		  AND ($2::timestamptz IS NULL OR r.created_at > $2)
		ORDER BY r.created_at DESC`, bandID, since)
	if err != nil {
		return nil, err
	}
	for proposalRows.Next() {
		var createdAt time.Time
		var actorName, kind, title, status string
		var preExisted bool
		if err := proposalRows.Scan(&createdAt, &actorName, &kind, &title, &preExisted, &status); err != nil {
			proposalRows.Close()
			return nil, err
		}
		if title == "" {
			title = "a song"
		}
		kindLabel := "a new song"
		if kind == "new_reference" {
			kindLabel = "a new reference URL"
		}
		switch {
		case !preExisted:
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("proposed %s for %q", kindLabel, title)})
		case status == "accepted":
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("proposal for %q (%s) was accepted", title, kindLabel)})
		case status == "rejected":
			out = append(out, BandChange{createdAt, actorName, fmt.Sprintf("proposal for %q (%s) was rejected", title, kindLabel)})
		}
	}
	proposalRows.Close()
	if err := proposalRows.Err(); err != nil {
		return nil, err
	}

	sort.Slice(out, func(i, j int) bool { return out[i].CreatedAt.After(out[j].CreatedAt) })
	if len(out) > 100 {
		out = out[:100]
	}
	return out, nil
}
