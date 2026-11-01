package store

import "context"

// BookMeta is small, optional project-level metadata that doesn't belong
// to any single song — currently just a reference playlist link.
type BookMeta struct {
	Playlist string `json:"playlist"`
}

// LoadBookMeta returns a zero-value BookMeta (not an error) when book has
// no playlist_url set — this is optional metadata, not a required part of
// a project.
func (st *Store) LoadBookMeta(book string) BookMeta {
	var playlist string
	if err := st.db.QueryRow(context.Background(),
		`SELECT playlist_url FROM projects WHERE slug = $1`, book).Scan(&playlist); err != nil {
		return BookMeta{}
	}
	return BookMeta{Playlist: playlist}
}
