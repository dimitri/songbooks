package store

import "context"

// KnownPerson is someone a person already plays or learns with: a
// current member of one of their current bands, or of one of their
// communities (not The Big Songbook, which everyone is in). With no
// usernames (migration 0135), it's how an "Add someone" form offers
// people without anyone typing an email: by name, with where they
// share a group, which also tells two people of the same name apart.
type KnownPerson struct {
	ID          int64
	DisplayName string
	Where       string // the bands and communities shared, by name, comma separated
}

// ListPeopleKnownTo lists the people personID shares a current band or
// community with: not themself, not a placeholder (shell) account. By
// name.
func (st *Store) ListPeopleKnownTo(ctx context.Context, personID int64) ([]KnownPerson, error) {
	rows, err := st.db.Query(ctx, `
		WITH shared AS (
			SELECT them.person_id, b.name
			FROM band_members me
			JOIN band_members them ON them.band_id = me.band_id AND upper_inf(them.validity)
			JOIN bands b ON b.id = me.band_id
			WHERE me.person_id = $1 AND upper_inf(me.validity)
			UNION
			SELECT them.person_id, c.name
			FROM community_members me
			JOIN communities c ON c.id = me.community_id AND c.kind <> 'global'
			JOIN community_members them ON them.community_id = me.community_id AND upper_inf(them.validity)
			WHERE me.person_id = $1 AND upper_inf(me.validity)
		)
		SELECT p.id, p.display_name, string_agg(DISTINCT s.name, ', ' ORDER BY s.name)
		FROM shared s JOIN people p ON p.id = s.person_id
		WHERE p.id <> $1 AND NOT p.is_shell
		GROUP BY p.id, p.display_name
		ORDER BY lower(p.display_name), p.id`, personID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []KnownPerson
	for rows.Next() {
		var k KnownPerson
		if err := rows.Scan(&k.ID, &k.DisplayName, &k.Where); err != nil {
			return nil, err
		}
		out = append(out, k)
	}
	return out, rows.Err()
}

// KnowsPerson reports whether otherID is one of ListPeopleKnownTo
// (personID): who an "Add someone" form may add by picking a name.
func (st *Store) KnowsPerson(ctx context.Context, personID, otherID int64) (bool, error) {
	known, err := st.ListPeopleKnownTo(ctx, personID)
	if err != nil {
		return false, err
	}
	for _, k := range known {
		if k.ID == otherID {
			return true, nil
		}
	}
	return false, nil
}
