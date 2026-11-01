package store

import (
	"context"
	"encoding/json"
	"regexp"
	"strings"
)

// SongGroup is one "# SET: Name" grouping — a project's own canonical play
// order, not alphabetical. The name predates this package's move to
// Postgres (it mirrors the order.txt format v2's own Typst book pipeline
// also reads), kept because the HTTP layer already speaks it.
type SongGroup struct {
	Label string
	Stems []string
}

// HasOrderFile reports whether book has its own song_order set (the "v1"
// canonical ordering) rather than falling back to alphabetical.
func (st *Store) HasOrderFile(book string) bool {
	var raw []byte
	err := st.db.QueryRow(context.Background(),
		`SELECT song_order FROM projects WHERE slug = $1`, book).Scan(&raw)
	if err != nil {
		return false
	}
	var groups []SongGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return false
	}
	return len(groups) > 0
}

// ListSongsOrdered returns book's songs grouped and ordered exactly as its
// song_order lists them, falling back to a single alphabetical group when
// none is set.
func (st *Store) ListSongsOrdered(book string) ([]SongGroup, error) {
	var raw []byte
	err := st.db.QueryRow(context.Background(),
		`SELECT song_order FROM projects WHERE slug = $1`, book).Scan(&raw)
	if err != nil {
		return nil, err
	}
	var groups []SongGroup
	if err := json.Unmarshal(raw, &groups); err != nil {
		return nil, err
	}
	if len(groups) > 0 {
		return groups, nil
	}
	stems, err := st.ListSongs(book)
	if err != nil {
		return nil, err
	}
	return []SongGroup{{Label: "Songs", Stems: stems}}, nil
}

var setMarkerRE = regexp.MustCompile(`^#\s*SET:\s*(.+)$`)

// parseOrderText parses the legacy order.txt format (also read directly by
// v2's own Typst book pipeline) — "# SET: Name" markers followed by one
// stem per line — into the same []SongGroup shape SaveOrder stores. Used
// only by the one-time import tool.
func parseOrderText(data string) []SongGroup {
	var groups []SongGroup
	var cur *SongGroup
	for _, raw := range strings.Split(data, "\n") {
		line := strings.TrimSpace(raw)
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "#") {
			if m := setMarkerRE.FindStringSubmatch(line); m != nil {
				if cur != nil {
					groups = append(groups, *cur)
				}
				cur = &SongGroup{Label: m[1]}
			}
			continue
		}
		if cur == nil {
			cur = &SongGroup{Label: "Songs"}
		}
		cur.Stems = append(cur.Stems, line)
	}
	if cur != nil {
		groups = append(groups, *cur)
	}
	return groups
}

// SaveOrder writes book's song_order.
func (st *Store) SaveOrder(book string, groups []SongGroup) error {
	raw, err := json.Marshal(groups)
	if err != nil {
		return err
	}
	_, err = st.db.Exec(context.Background(),
		`UPDATE projects SET song_order = $1 WHERE slug = $2`, raw, book)
	return err
}
