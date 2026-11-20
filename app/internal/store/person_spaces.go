package store

import (
	"context"
	"fmt"
	"time"
)

// A Space is a band or a community one belongs to, as the space switcher
// lists it (menu PR 7, migration 0102): pinned ones first (in the order
// they were pinned), then by when one last used it, then by name.
type Space struct {
	Kind string // "band" or "community"
	ID   int64
	Name string
	// CommunityKind is a community's kind ("community", "coaching"); ""
	// for a band.
	CommunityKind string
	UsedAt        *time.Time
	Pinned        bool
}

// spaceColumn is the person_spaces column of a kind of space.
func spaceColumn(kind string) (string, error) {
	switch kind {
	case "band":
		return "band_id", nil
	case "community":
		return "community_id", nil
	}
	return "", fmt.Errorf("not a kind of space: %q", kind)
}

// ListSpaces lists the bands and communities personID currently belongs
// to, in the switcher's order (see Space).
func (st *Store) ListSpaces(ctx context.Context, personID int64) ([]Space, error) {
	rows, err := st.db.Query(ctx, `
		SELECT kind, id, name, community_kind, used_at, pinned_at FROM (
			SELECT DISTINCT 'band' AS kind, b.id, b.name, '' AS community_kind, ps.used_at, ps.pinned_at
			FROM bands b
			JOIN band_members m ON m.band_id = b.id AND m.person_id = $1 AND upper_inf(m.validity)
			LEFT JOIN person_spaces ps ON ps.person_id = $1 AND ps.band_id = b.id
			UNION ALL
			SELECT DISTINCT 'community', c.id, c.name, c.kind, ps.used_at, ps.pinned_at
			FROM communities c
			JOIN community_members m ON m.community_id = c.id AND m.person_id = $1 AND upper_inf(m.validity)
			LEFT JOIN person_spaces ps ON ps.person_id = $1 AND ps.community_id = c.id
		) s
		ORDER BY pinned_at IS NULL, pinned_at, used_at DESC NULLS LAST, lower(name), kind, id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Space
	for rows.Next() {
		var sp Space
		var pinnedAt *time.Time
		if err := rows.Scan(&sp.Kind, &sp.ID, &sp.Name, &sp.CommunityKind, &sp.UsedAt, &pinnedAt); err != nil {
			return nil, err
		}
		sp.Pinned = pinnedAt != nil
		out = append(out, sp)
	}
	return out, rows.Err()
}

// TouchSpace records that personID is using a band's or a community's
// pages right now.
func (st *Store) TouchSpace(ctx context.Context, personID int64, kind string, id int64) error {
	col, err := spaceColumn(kind)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO person_spaces (person_id, %[1]s, used_at) VALUES ($1, $2, now())
		ON CONFLICT (person_id, %[1]s) WHERE %[1]s IS NOT NULL DO UPDATE SET used_at = now()`, col),
		personID, id)
	return err
}

// PinSpace pins (or unpins) a band or a community at the top of
// personID's switcher.
func (st *Store) PinSpace(ctx context.Context, personID int64, kind string, id int64, pinned bool) error {
	col, err := spaceColumn(kind)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO person_spaces (person_id, %[1]s, pinned_at) VALUES ($1, $2, CASE WHEN $3 THEN now() END)
		ON CONFLICT (person_id, %[1]s) WHERE %[1]s IS NOT NULL DO UPDATE
		SET pinned_at = CASE WHEN $3 THEN coalesce(person_spaces.pinned_at, now()) END`, col),
		personID, id, pinned)
	return err
}
